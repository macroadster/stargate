package mcp

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// checkRateLimit checks if the API key has exceeded rate limit (100 requests per minute)
func (h *HTTPMCPServer) checkRateLimit(key string) bool {
	now := time.Now()
	window := now.Add(-time.Minute)
	times := h.rateLimiter[key]
	valid := make([]time.Time, 0, len(times))
	for _, t := range times {
		if t.After(window) {
			valid = append(valid, t)
		}
	}
	h.rateLimiter[key] = valid
	if len(valid) >= 100 {
		return false
	}
	h.rateLimiter[key] = append(h.rateLimiter[key], now)
	return true
}

func (h *HTTPMCPServer) authWrap(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Printf("AUDIT: %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
		// Check API key if configured
		if h.apiKeyStore != nil {
			key := r.Header.Get("X-API-Key")
			if key == "" {
				// Check Authorization: Bearer <key>
				auth := r.Header.Get("Authorization")
				if strings.HasPrefix(auth, "Bearer ") {
					key = strings.TrimPrefix(auth, "Bearer ")
				}
			}
			if key == "" && h.allowUnauthMCP(r) {
				next(w, r)
				return
			}
			if key == "" {
				log.Printf("AUDIT: Missing API key for %s %s", r.Method, r.URL.Path)
				h.writeHTTPError(w, http.StatusUnauthorized, "API_KEY_REQUIRED", "API key required", "Send X-API-Key or Authorization: Bearer <key>.")
				return
			}
			if !h.apiKeyStore.Validate(key) {
				log.Printf("AUDIT: Invalid API key for %s %s", r.Method, r.URL.Path)
				h.writeHTTPError(w, http.StatusForbidden, "API_KEY_INVALID", "Invalid API key", "Double-check the X-API-Key header value.")
				return
			}
			// Check rate limit
			if !h.checkRateLimit(key) {
				log.Printf("AUDIT: Rate limit exceeded for key %s on %s %s", key, r.Method, r.URL.Path)
				h.writeHTTPError(w, http.StatusTooManyRequests, "RATE_LIMITED", "Rate limit exceeded", "Retry after a short delay.")
				return
			}
			log.Printf("AUDIT: Authenticated request for key %s on %s %s", key, r.Method, r.URL.Path)
		}
		next(w, r)
	}
}

func (h *HTTPMCPServer) allowUnauthMCP(r *http.Request) bool {
	if r == nil || r.URL == nil {
		return false
	}
	if r.Method != http.MethodPost {
		return false
	}
	if r.URL.Path != "/mcp" && r.URL.Path != "/mcp/" {
		return false
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return false
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	if len(body) == 0 {
		return false
	}
	var req jsonRPCRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return false
	}
	switch req.Method {
	case "initialize", "notifications/initialized":
		return true
	default:
		return false
	}
}
