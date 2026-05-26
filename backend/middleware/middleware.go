package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
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

		// Sandbox paths serve user-generated HTML apps that need full web
		// capabilities (external scripts, remote APIs). Skip the restrictive
		// CSP and allow framing so the modal iframe preview works too.
		if strings.HasPrefix(r.URL.Path, "/sandbox/") {
			w.Header().Set("X-Frame-Options", "ALLOWALL")
			next.ServeHTTP(w, r)
			return
		}

		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")

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
			"frame-src 'self' blob:; "+
			"object-src 'none';", connectSrc)
		w.Header().Set("Content-Security-Policy", csp)

		next.ServeHTTP(w, r)
	})
}

// Timeout middleware. Uses a mutex-protected response writer so that when the
// timeout fires the handler goroutine can no longer write to the real
// connection. This prevents the "wrote more than the declared Content-Length"
// race that previously caused goroutine leaks and crash loops.
func Timeout(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip timeout for streaming endpoints and health probes.
			if r.Header.Get("Accept") == "text/event-stream" ||
				strings.Contains(r.URL.Path, "/chat/stream") ||
				strings.Contains(r.URL.Path, "/mcp/events") ||
				strings.Contains(r.URL.Path, "/smart_contract/events") ||
				r.URL.Path == "/api/health" ||
				r.URL.Path == "/bitcoin/v1/health" {
				next.ServeHTTP(w, r)
				return
			}

			ctx, cancel := context.WithTimeout(r.Context(), timeout)
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
func ContentType(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" || r.Method == "PUT" {
			contentType := r.Header.Get("Content-Type")
			if contentType == "" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)

				errorResp := map[string]interface{}{
					"success": false,
					"error": map[string]interface{}{
						"error":   "missing_content_type",
						"message": "Content-Type header is required",
						"code":    http.StatusBadRequest,
					},
				}

				json.NewEncoder(w).Encode(errorResp)
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

// Rate limiting middleware (simple implementation)
func RateLimit(requests int, window time.Duration) func(http.Handler) http.Handler {
	type client struct {
		requests int
		window   time.Time
	}

	clients := make(map[string]*client)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			clientIP := r.RemoteAddr
			now := time.Now()

			if c, exists := clients[clientIP]; exists {
				if now.Sub(c.window) > window {
					// Reset window
					c.requests = 1
					c.window = now
				} else {
					c.requests++
					if c.requests > requests {
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusTooManyRequests)

						errorResp := map[string]interface{}{
							"success": false,
							"error": map[string]interface{}{
								"error":   "rate_limit_exceeded",
								"message": "Too many requests",
								"code":    http.StatusTooManyRequests,
							},
						}

						json.NewEncoder(w).Encode(errorResp)
						return
					}
				}
			} else {
				clients[clientIP] = &client{
					requests: 1,
					window:   now,
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

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

// APIAuth validates API keys against the validator
func APIAuth(validator auth.APIKeyValidator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			apiKey := r.Header.Get("X-API-Key")
			if apiKey == "" {
				authHeader := r.Header.Get("Authorization")
				if strings.HasPrefix(authHeader, "Bearer ") {
					apiKey = strings.TrimPrefix(authHeader, "Bearer ")
				}
			}

			// Check cookie if header is missing
			if apiKey == "" {
				if cookie, err := r.Cookie("X-API-Key"); err == nil {
					apiKey = cookie.Value
				}
			}

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

			next.ServeHTTP(w, r)
		})
	}
}
