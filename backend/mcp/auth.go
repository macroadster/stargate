package mcp

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"time"
)

// checkRateLimit checks if the API key has exceeded rate limit (100 requests per minute)
func (h *HTTPMCPServer) checkRateLimit(key string) bool {
	h.rateLimiterMu.Lock()
	defer h.rateLimiterMu.Unlock()

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
