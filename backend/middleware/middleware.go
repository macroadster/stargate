package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	auth "stargate-backend/storage/auth"
)

// CORS middleware
func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			origin = "*"
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, PATCH")
		w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, Authorization, X-API-Key, X-Requested-With")
		w.Header().Set("Access-Control-Allow-Credentials", "true")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// Logging middleware
func Logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Create a response writer wrapper to capture status code
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK, headersWritten: false}

		next.ServeHTTP(wrapped, r)

		duration := time.Since(start)
		entry := map[string]interface{}{
			"ts":       start.UTC().Format(time.RFC3339Nano),
			"method":   r.Method,
			"path":     r.URL.Path,
			"status":   wrapped.statusCode,
			"duration": duration.String(),
		}
		if err := json.NewEncoder(log.Writer()).Encode(entry); err != nil {
			log.Printf("%s %s %d %v", r.Method, r.URL.Path, wrapped.statusCode, duration)
		}
	})
}

// Recovery middleware
func Recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("Panic recovered: %v", err)

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)

				errorResp := map[string]interface{}{
					"success": false,
					"error": map[string]interface{}{
						"error":   "internal_server_error",
						"message": "Internal server error occurred",
						"code":    http.StatusInternalServerError,
					},
				}

				json.NewEncoder(w).Encode(errorResp)
			}
		}()

		next.ServeHTTP(w, r)
	})
}

// SecurityHeaders middleware
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains; preload")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")

		// Sandbox paths serve user-generated HTML apps. They must not be able
		// to phone home or exfiltrate same-origin data (the sql.js auth blob,
		// API keys, …) to a remote host, so lock connect-src down to local
		// schemes only. blob:/data: are self-generated, not remote egress.
		// Local media (file-picker plays, embedded base64 audio) keeps working
		// via media-src/connect-src blob:/data:; inline scripts/styles and
		// base64 images are required by the existing contract artifacts.
		// 'wasm-unsafe-eval' permits WebAssembly compilation (sql.js/SQLite)
		// from already-allowed scripts; it grants no new source origin.
		// Same-origin framing (modal preview, Starlight Shell) still works;
		// cross-origin framing is denied.
		if strings.HasPrefix(r.URL.Path, "/sandbox/") {
			w.Header().Set("X-Frame-Options", "SAMEORIGIN")
			w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
			csp := sandboxCSPDefault
			// Narrow carve-outs for known first-party system contracts whose
			// artifacts hardcode specific third-party origins. These are
			// pinned-hash apps built for this network; every other contract
			// keeps the strict default.
			if hash := sandboxPathHash(r.URL.Path); hash != "" {
				if override, ok := sandboxCSPOverrides[hash]; ok {
					csp = override
				}
			}
			w.Header().Set("Content-Security-Policy", csp)
			next.ServeHTTP(w, r)
			return
		}

		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")

		// The Starlight Shell (/desktop/) embeds arbitrary sites in the Browser
		// app's sandboxed iframe (no allow-top-navigation), so those pages must
		// be allowed to frame http(s) origins. Every other path keeps
		// frame-src 'self' blob:. Sites that set X-Frame-Options or CSP
		// frame-ancestors still block themselves — the shell shows a blocked
		// page with an "open in system browser" escape hatch for those.
		frameSrc := "frame-src 'self' blob:; "
		if strings.HasPrefix(r.URL.Path, "/desktop/") {
			frameSrc = "frame-src 'self' blob: http: https:; "
		}

		// Content Security Policy
		// - connect-src 'self' prevents sending data to external domains
		// - script-src 'self' 'unsafe-inline' 'unsafe-eval' (unsafe-eval is needed for wasm/sql.js)
		// - worker-src 'self' blob: (needed for sql.js if used in a worker)
		origin := r.Header.Get("Origin")
		connectSrc := "connect-src 'self';"
		if origin != "" && origin != "null" {
			connectSrc = fmt.Sprintf("connect-src 'self' %s;", origin)
		}

		csp := fmt.Sprintf("default-src 'self'; "+
			"script-src 'self' 'unsafe-inline' 'unsafe-eval'; "+
			"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; "+
			"font-src 'self' https://fonts.gstatic.com; "+
			"img-src 'self' data: blob: https:; "+
			"%s "+
			"worker-src 'self' blob:; "+
			"%s"+
			"object-src 'none';", connectSrc, frameSrc)
		w.Header().Set("Content-Security-Policy", csp)

		next.ServeHTTP(w, r)
	})
}

// sandboxCSPDefault is the locked-down policy for user-generated sandbox
// apps. Nothing remote may load; blob:/data: are self-generated, not remote
// egress. 'wasm-unsafe-eval' permits WebAssembly compilation (sql.js /
// SQLite) from already-allowed scripts; it grants no new origin.
const sandboxCSPDefault = "default-src 'self'; " +
	"script-src 'self' 'unsafe-inline' 'wasm-unsafe-eval'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: blob:; " +
	"font-src 'self' data:; " +
	"connect-src 'self' blob: data:; " +
	"media-src 'self' blob: data:; " +
	"worker-src 'self' blob:; " +
	"object-src 'none'; " +
	"base-uri 'none'; " +
	"form-action 'self'; " +
	"frame-ancestors 'self'"

// sandboxCSPOverrides holds narrow CSP exceptions for known, first-party
// system contracts pinned by content hash. Their artifacts hardcode
// specific third-party origins (CDN asset hosts, the chat relay, and
// cross-node Starlight video URLs). Every other sandbox path keeps
// sandboxCSPDefault; adding an override is an explicit, auditable decision.
var sandboxCSPOverrides = map[string]string{
	// Starlight Chat (p2p): sql.js loaded from cdnjs (JS + WASM binary),
	// chat stream/fetch against the starlight-ai.freemyip.com MCP relay.
	"503751efad310be50b8c2e63563dc1a2c1ddef4f52c9d3b91a6394c75b3985f3": "default-src 'self'; " +
		"script-src 'self' 'unsafe-inline' 'wasm-unsafe-eval' https://cdnjs.cloudflare.com; " +
		"style-src 'self' 'unsafe-inline'; " +
		"img-src 'self' data: blob:; " +
		"font-src 'self' data:; " +
		"connect-src 'self' blob: data: https://cdnjs.cloudflare.com https://starlight-ai.freemyip.com; " +
		"media-src 'self' blob: data:; " +
		"worker-src 'self' blob:; " +
		"object-src 'none'; " +
		"base-uri 'none'; " +
		"form-action 'self'; " +
		"frame-ancestors 'self'",
	// Loops (local video library): quantumcss + starlight.js from unpkg,
	// Video library backed by sql.js (wasm-unsafe-eval covers it). It
	// streams "Starlight videos" cross-node — media/img/connect must reach
	// arbitrary https sandboxes by design once a user pastes a URL; until
	// then media/img posts load from this node. Local files via FSA API
	// and blob: URLs are unaffected by CSP.
	"2d89e6ebfd09604938416481cb69cee2a63ceadab93541b3fbf4a26293274bf7": "default-src 'self'; " +
		"script-src 'self' 'unsafe-inline' 'wasm-unsafe-eval' https://unpkg.com; " +
		"style-src 'self' 'unsafe-inline' https://unpkg.com; " +
		"img-src 'self' data: blob: https:; " +
		"font-src 'self' data: https://unpkg.com; " +
		"connect-src 'self' blob: data: https:; " +
		"media-src 'self' blob: data: https:; " +
		"worker-src 'self' blob:; " +
		"object-src 'none'; " +
		"base-uri 'none'; " +
		"form-action 'self'; " +
		"frame-ancestors 'self'",
}

// sandboxPathHash extracts the first path segment after /sandbox/ for
// override lookup. Returns "" when there is none.
func sandboxPathHash(path string) string {
	rel := strings.TrimPrefix(path, "/sandbox/")
	seg, _, _ := strings.Cut(rel, "/")
	return strings.Trim(seg, "/")
}

// Timeout middleware. Uses a mutex-protected response writer so that when the
// timeout fires the handler goroutine can no longer write to the real
// connection. This prevents the "wrote more than the declared Content-Length"
// race that previously caused goroutine leaks and crash loops.
func Timeout(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip timeout for streaming endpoints, health probes, and all MCP endpoints.
			// MCP tool calls (especially scans, PSBTs, execution) can legitimately take >30s;
			// timing them out causes clients to retry and produces "infinite" POST loops.
			if r.Header.Get("Accept") == "text/event-stream" ||
				strings.Contains(r.URL.Path, "/chat/stream") ||
				strings.Contains(r.URL.Path, "/mcp/events") ||
				strings.Contains(r.URL.Path, "/smart_contract/events") ||
				strings.HasPrefix(r.URL.Path, "/mcp") ||
				r.URL.Path == "/api/health" ||
				r.URL.Path == "/bitcoin/v1/health" {
				next.ServeHTTP(w, r)
				return
			}

			limit := timeout
			// Qwen2 scan prefill in Trin can take longer than the default 30s.
			if (r.URL.Path == "/bitcoin/v1/scan/image" || r.URL.Path == "/bitcoin/v1/extract") &&
				strings.TrimSpace(os.Getenv("STARLIGHT_LLM_GGUF")) != "" && limit < 3*time.Minute {
				limit = 3 * time.Minute
			}
			ctx, cancel := context.WithTimeout(r.Context(), limit)
			defer cancel()

			r = r.WithContext(ctx)

			tracked := &timeoutTrackingWriter{w: w}

			done := make(chan struct{})
			go func() {
				defer close(done)
				next.ServeHTTP(tracked, r)
			}()

			select {
			case <-done:
				// Request completed normally
			case <-ctx.Done():
				// Set timedOut under the lock so the handler goroutine
				// can no longer write to the real ResponseWriter.
				tracked.mu.Lock()
				alreadyCommitted := tracked.committed
				tracked.timedOut = true
				tracked.mu.Unlock()

				if !alreadyCommitted {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusRequestTimeout)

					errorResp := map[string]interface{}{
						"success": false,
						"error": map[string]interface{}{
							"error":   "request_timeout",
							"message": "Request timed out",
							"code":    http.StatusRequestTimeout,
						},
					}

					json.NewEncoder(w).Encode(errorResp)
				}
			}
		})
	}
}

// timeoutTrackingWriter wraps an http.ResponseWriter with a mutex so that
// after a timeout, the handler goroutine's writes are silently discarded
// instead of racing with the timeout response.
type timeoutTrackingWriter struct {
	w          http.ResponseWriter
	mu         sync.Mutex
	committed  bool
	timedOut   bool
	statusCode int
}

func (tw *timeoutTrackingWriter) Header() http.Header {
	return tw.w.Header()
}

func (tw *timeoutTrackingWriter) WriteHeader(statusCode int) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if tw.timedOut || tw.committed {
		return
	}
	tw.committed = true
	tw.statusCode = statusCode
	tw.w.WriteHeader(statusCode)
}

func (tw *timeoutTrackingWriter) Write(b []byte) (int, error) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if tw.timedOut {
		return 0, http.ErrHandlerTimeout
	}
	if !tw.committed {
		tw.statusCode = http.StatusOK
		tw.w.WriteHeader(http.StatusOK)
		tw.committed = true
	}
	return tw.w.Write(b)
}

func (tw *timeoutTrackingWriter) Flush() {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if tw.timedOut {
		return
	}
	if f, ok := tw.w.(http.Flusher); ok {
		f.Flush()
	}
}

// ContentType middleware

// Rate limiting middleware (simple implementation)

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode     int
	headersWritten bool
	flushed        bool
}

func (rw *responseWriter) WriteHeader(code int) {
	if rw.headersWritten {
		return
	}
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
	rw.headersWritten = true
}

func (rw *responseWriter) Flush() {
	if rw.ResponseWriter == nil {
		return
	}
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		rw.flushed = true
		f.Flush()
	}
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.headersWritten {
		rw.statusCode = http.StatusOK
		rw.ResponseWriter.WriteHeader(http.StatusOK)
		rw.headersWritten = true
	}
	return rw.ResponseWriter.Write(b)
}

// APIAuth validates API keys against the validator.
// Same extractor as REST authWrap and MCP: X-API-Key, Bearer, or cookie.
func APIAuth(validator auth.APIKeyValidator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			apiKey := auth.APIKeyFromRequest(r)
			if apiKey == "" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"success": false,
					"error": map[string]interface{}{
						"error":   "api_key_required",
						"message": "API key required",
						"code":    http.StatusUnauthorized,
					},
				})
				return
			}

			if validator != nil && !validator.Validate(apiKey) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"success": false,
					"error": map[string]interface{}{
						"error":   "api_key_invalid",
						"message": "Invalid API key",
						"code":    http.StatusForbidden,
					},
				})
				return
			}

			next.ServeHTTP(w, r.WithContext(auth.WithAPIKey(r.Context(), apiKey)))
		})
	}
}

// LoopbackOnly rejects requests whose remote address is not a loopback
// address. Used for /metrics and /debug/pprof so a 0.0.0.0 bind does not
// expose heap dumps or node topology to the network.
func LoopbackOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
