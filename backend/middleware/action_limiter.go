package middleware

import (
	"context"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	auth "stargate-backend/storage/auth"
)

// Action is a claim/submit/review mutation limited on REST and MCP.
type Action string

const (
	ActionClaim  Action = "claim"
	ActionSubmit Action = "submit"
	ActionReview Action = "review"
)

// ActionLimit is a token-bucket quota for one action.
type ActionLimit struct {
	RPS   float64
	Burst int
}

// DefaultActionLimits matches the unified-plan recommendation:
// 10 rps claim (burst 100), 5 rps submit/review (burst 50).
func DefaultActionLimits() map[Action]ActionLimit {
	return map[Action]ActionLimit{
		ActionClaim:  {RPS: 10, Burst: 100},
		ActionSubmit: {RPS: 5, Burst: 50},
		ActionReview: {RPS: 5, Burst: 50},
	}
}

// AllowResult is the outcome of one Allow check.
type AllowResult struct {
	Allowed    bool
	Limit      int
	Remaining  int
	ResetUnix  int64
	RetryAfter time.Duration
	Action     Action
	Key        string
}

type actionBucket struct {
	tokens     float64
	lastRefill time.Time
	limit      ActionLimit
}

// ActionLimiter is a shared claim/submit/review limiter for REST and MCP.
// Count once at the HTTP edge. In-process store calls must not call Allow.
type ActionLimiter struct {
	mu      sync.Mutex
	limits  map[Action]ActionLimit
	buckets map[string]*actionBucket
	now     func() time.Time
	checks  uint64
}

// NewActionLimiter builds a limiter. Empty limits fall back to DefaultActionLimits.
func NewActionLimiter(limits map[Action]ActionLimit) *ActionLimiter {
	if len(limits) == 0 {
		limits = DefaultActionLimits()
	}
	copied := make(map[Action]ActionLimit, len(limits))
	for k, v := range limits {
		if v.RPS <= 0 {
			v.RPS = 1
		}
		if v.Burst <= 0 {
			v.Burst = 1
		}
		copied[k] = v
	}
	return &ActionLimiter{
		limits:  copied,
		buckets: make(map[string]*actionBucket),
		now:     time.Now,
	}
}

// NewActionLimiterFromEnv returns the default limiter, or nil when disabled
// via STARGATE_ACTION_RATE_LIMIT=off|0|false|disabled.
func NewActionLimiterFromEnv() *ActionLimiter {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("STARGATE_ACTION_RATE_LIMIT"))) {
	case "off", "0", "false", "disabled":
		return nil
	default:
		return NewActionLimiter(DefaultActionLimits())
	}
}

// LimitOf returns the configured quota for an action.
func (l *ActionLimiter) LimitOf(action Action) (ActionLimit, bool) {
	if l == nil {
		return ActionLimit{}, false
	}
	lim, ok := l.limits[action]
	return lim, ok
}

// Allow consumes one token for key+action. A nil limiter allows everything.
func (l *ActionLimiter) Allow(key string, action Action) AllowResult {
	if l == nil {
		return AllowResult{Allowed: true, Action: action, Key: key, Remaining: math.MaxInt32}
	}
	lim, ok := l.limits[action]
	if !ok {
		return AllowResult{Allowed: true, Action: action, Key: key, Remaining: math.MaxInt32}
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.checks++
	if l.checks%256 == 0 {
		l.sweepLocked(now)
	}

	bk := bucketKey(key, action)
	b := l.buckets[bk]
	if b == nil {
		b = &actionBucket{tokens: float64(lim.Burst), lastRefill: now, limit: lim}
		l.buckets[bk] = b
	}

	elapsed := now.Sub(b.lastRefill).Seconds()
	if elapsed > 0 {
		b.tokens += elapsed * b.limit.RPS
		if b.tokens > float64(b.limit.Burst) {
			b.tokens = float64(b.limit.Burst)
		}
		b.lastRefill = now
	}

	resetUnix := now.Add(time.Second).Unix()
	if b.tokens < float64(b.limit.Burst) && b.limit.RPS > 0 {
		secs := (float64(b.limit.Burst) - b.tokens) / b.limit.RPS
		resetUnix = now.Add(time.Duration(secs * float64(time.Second))).Unix()
	}

	if b.tokens >= 1 {
		b.tokens--
		return AllowResult{
			Allowed:   true,
			Limit:     b.limit.Burst,
			Remaining: int(math.Floor(b.tokens)),
			ResetUnix: resetUnix,
			Action:    action,
			Key:       key,
		}
	}

	retry := time.Second
	if b.limit.RPS > 0 {
		retry = time.Duration(math.Ceil((1-b.tokens)/b.limit.RPS) * float64(time.Second))
		if retry < time.Second {
			retry = time.Second
		}
	}
	return AllowResult{
		Allowed:    false,
		Limit:      b.limit.Burst,
		Remaining:  0,
		ResetUnix:  now.Add(retry).Unix(),
		RetryAfter: retry,
		Action:     action,
		Key:        key,
	}
}

func (l *ActionLimiter) sweepLocked(now time.Time) {
	for k, b := range l.buckets {
		if now.Sub(b.lastRefill) > 10*time.Minute && b.tokens >= float64(b.limit.Burst) {
			delete(l.buckets, k)
		}
	}
}

func bucketKey(key string, action Action) string {
	if key == "" {
		key = "anon"
	}
	return key + ":" + string(action)
}

// ClientKey picks one shared identity: API key, then wallet, then tool.
func ClientKey(apiKey, wallet, tool string) string {
	if v := strings.TrimSpace(apiKey); v != "" {
		return "apikey:" + v
	}
	if v := strings.TrimSpace(wallet); v != "" {
		return "wallet:" + v
	}
	if v := strings.TrimSpace(tool); v != "" {
		return "tool:" + v
	}
	return "anon"
}

// APIKeyFromRequest reads X-API-Key, Bearer, or the X-API-Key cookie.
func APIKeyFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	if k := strings.TrimSpace(r.Header.Get("X-API-Key")); k != "" {
		return k
	}
	if authz := r.Header.Get("Authorization"); strings.HasPrefix(authz, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(authz, "Bearer "))
	}
	if c, err := r.Cookie("X-API-Key"); err == nil {
		return strings.TrimSpace(c.Value)
	}
	return ""
}

// IdentityKey is the shared REST/MCP bucket identity for a request.
func IdentityKey(r *http.Request, keys auth.APIKeyValidator, tool string) string {
	apiKey := APIKeyFromRequest(r)
	wallet := ""
	if keys != nil && apiKey != "" {
		if rec, ok := keys.Get(apiKey); ok {
			wallet = rec.Wallet
		}
	}
	return ClientKey(apiKey, wallet, tool)
}

// ActionForTool maps MCP tool names onto claim/submit/review.
func ActionForTool(tool string) (Action, bool) {
	switch strings.TrimSpace(tool) {
	case "claim_task":
		return ActionClaim, true
	case "submit_work":
		return ActionSubmit, true
	case "review_submission", "approve_submission", "reject_submission":
		return ActionReview, true
	default:
		return "", false
	}
}

// ClassifySmartContractAction maps REST claim/submit/review routes to an action.
func ClassifySmartContractAction(method, path string) (Action, string, bool) {
	if !strings.EqualFold(method, http.MethodPost) {
		return "", "", false
	}
	path = strings.TrimSuffix(path, "/")
	switch {
	case strings.HasPrefix(path, "/api/smart_contract/tasks/") && strings.HasSuffix(path, "/claim"):
		return ActionClaim, "claim_task", true
	case strings.HasPrefix(path, "/api/smart_contract/claims/") && strings.HasSuffix(path, "/submit"):
		return ActionSubmit, "submit_work", true
	case strings.HasPrefix(path, "/api/smart_contract/submissions/") && strings.HasSuffix(path, "/review"):
		return ActionReview, "review_submission", true
	default:
		return "", "", false
	}
}

// WriteRateLimitHeaders sets X-RateLimit-* and Retry-After when denied.
func WriteRateLimitHeaders(w http.ResponseWriter, res AllowResult) {
	if w == nil {
		return
	}
	if res.Limit > 0 {
		w.Header().Set("X-RateLimit-Limit", strconv.Itoa(res.Limit))
		w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(res.Remaining))
	}
	if res.ResetUnix > 0 {
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(res.ResetUnix, 10))
	}
	if !res.Allowed && res.RetryAfter > 0 {
		secs := int(math.Ceil(res.RetryAfter.Seconds()))
		if secs < 1 {
			secs = 1
		}
		w.Header().Set("Retry-After", strconv.Itoa(secs))
	}
}

// Discover describes the limiter for /discover payloads.
func (l *ActionLimiter) Discover() map[string]interface{} {
	if l == nil {
		return map[string]interface{}{
			"enabled": false,
			"notes":   "rate limiting disabled",
		}
	}
	out := map[string]interface{}{
		"enabled":  true,
		"shared":   true,
		"key":      "api_key | wallet | tool",
		"surfaces": []string{"/api/smart_contract/*", "/mcp/call"},
		"notes":    "One count per HTTP request. In-process MCP store calls are not counted again.",
	}
	for _, action := range []Action{ActionClaim, ActionSubmit, ActionReview} {
		if lim, ok := l.limits[action]; ok {
			out[string(action)] = map[string]interface{}{
				"rps":   lim.RPS,
				"burst": lim.Burst,
			}
		}
	}
	return out
}

type actionLimitCtxKey struct{}

// MarkActionLimited records that this request already consumed a token.
// REST handlers skip a second Allow if MCP later ServeHTTP's the same request.
func MarkActionLimited(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, actionLimitCtxKey{}, true)
}

// ActionLimitApplied reports whether Allow already ran for this request.
func ActionLimitApplied(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, _ := ctx.Value(actionLimitCtxKey{}).(bool)
	return v
}
