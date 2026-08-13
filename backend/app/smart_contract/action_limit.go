package smart_contract

import (
	"net/http"

	"stargate-backend/middleware"
)

// limitAction counts claim/submit/review once at the REST edge.
// In-process MCP store calls never enter this wrapper, so they cannot double-count.
// If MCP later ServeHTTP's the same request, the context mark skips a second Allow.
func (s *Server) limitAction(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s == nil || s.actionLimiter == nil || middleware.ActionLimitApplied(r.Context()) {
			next(w, r)
			return
		}
		action, tool, ok := middleware.ClassifySmartContractAction(r.Method, r.URL.Path)
		if !ok {
			next(w, r)
			return
		}
		key := middleware.IdentityKey(r, s.apiKeys, tool)
		res := s.actionLimiter.Allow(key, action)
		middleware.WriteRateLimitHeaders(w, res)
		if !res.Allowed {
			Error(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
		next(w, r.WithContext(middleware.MarkActionLimited(r.Context())))
	}
}
