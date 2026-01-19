package middleware

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	auth "stargate-backend/storage/auth"
)

// CORS middleware
func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key")

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
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

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
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		next.ServeHTTP(w, r)
	})
}

// Timeout middleware
func Timeout(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()

			r = r.WithContext(ctx)

			// Wrap response writer to track if data was sent
			tracked := &timeoutTrackingWriter{ResponseWriter: w}

			done := make(chan struct{})
			go func() {
				defer close(done)
				next.ServeHTTP(tracked, r)
			}()

			select {
			case <-done:
				// Request completed normally
			case <-ctx.Done():
				// Request timed out
				// Only write error if response hasn't been committed yet
				if !tracked.committed {
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

type timeoutTrackingWriter struct {
	http.ResponseWriter
	committed bool
}

func (tw *timeoutTrackingWriter) WriteHeader(statusCode int) {
	tw.committed = true
	tw.ResponseWriter.WriteHeader(statusCode)
}

func (tw *timeoutTrackingWriter) Write(b []byte) (int, error) {
	if !tw.committed {
		tw.ResponseWriter.WriteHeader(http.StatusOK)
		tw.committed = true
	}
	return tw.ResponseWriter.Write(b)
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
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	if rw.statusCode != 0 {
		// Headers already written, ignore superfluous calls
		return
	}
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// APIAuth validates API keys against the validator
func APIAuth(validator auth.APIKeyValidator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			apiKey := r.Header.Get("X-API-Key")
			if apiKey == "" {
				auth := r.Header.Get("Authorization")
				if strings.HasPrefix(auth, "Bearer ") {
					apiKey = strings.TrimPrefix(auth, "Bearer ")
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
