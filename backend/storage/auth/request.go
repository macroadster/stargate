package auth

import (
	"context"
	"net/http"
	"strings"
)

// One bearer path for /api and /mcp (ADR 0005): X-API-Key, Authorization: Bearer, or the X-API-Key cookie.
// Register/login, middleware, MCP session, and STARGATE_API_KEY seed all resolve keys through this extractor
// plus the shared APIKeyValidator (api_keys table — no second key store).

type apiKeyCtxKey struct{}

// APIKeyFromRequest reads the plaintext API key from the request.
// Precedence: X-API-Key header, then Authorization: Bearer, then X-API-Key cookie.
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

// WithAPIKey stores the validated plaintext key on the request context.
func WithAPIKey(ctx context.Context, key string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return ctx
	}
	return context.WithValue(ctx, apiKeyCtxKey{}, key)
}

// ContextAPIKey returns the key previously stored by WithAPIKey.
func ContextAPIKey(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(apiKeyCtxKey{}).(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// RequestAPIKey prefers a context-stashed key (set after validation), then the request headers/cookie.
func RequestAPIKey(r *http.Request) string {
	if r == nil {
		return ""
	}
	if k := ContextAPIKey(r.Context()); k != "" {
		return k
	}
	return APIKeyFromRequest(r)
}

// ValidateKey is a nil-safe check against a validator.
func ValidateKey(validator APIKeyValidator, key string) bool {
	key = strings.TrimSpace(key)
	if key == "" || validator == nil {
		return false
	}
	return validator.Validate(key)
}
