package mcp

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"stargate-backend/middleware"
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

// applyActionLimit counts claim/submit/review once at the MCP HTTP edge.
// callToolDirect / store calls do not call Allow, so they cannot double-count.
// Returns false when the request is denied (headers already written; caller writes the body).
func (h *HTTPMCPServer) applyActionLimit(w http.ResponseWriter, r *http.Request, tool string) (*http.Request, bool) {
	action, ok := middleware.ActionForTool(tool)
	if !ok || h.actionLimiter == nil || r == nil || middleware.ActionLimitApplied(r.Context()) {
		return r, true
	}
	key := middleware.IdentityKey(r, h.apiKeyStore, tool)
	res := h.actionLimiter.Allow(key, action)
	middleware.WriteRateLimitHeaders(w, res)
	if !res.Allowed {
		return r, false
	}
	return r.WithContext(middleware.MarkActionLimited(r.Context())), true
}

func actionLimitError(tool string) *ToolError {
	return &ToolError{
		Code:       ErrCodeRateLimited,
		Message:    "Rate limit exceeded. Retry after a short delay.",
		Tool:       tool,
		HttpStatus: http.StatusTooManyRequests,
		Hint:       "Shared claim/submit/review limit across /api/smart_contract and /mcp/call.",
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
