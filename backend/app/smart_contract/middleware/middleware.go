package middleware

import (
	"encoding/json"
	"net/http"
)

// ErrorResponse represents a standard error response
type ErrorResponse struct {
	Error string `json:"error"`
}

// Error sends a standardized error response
func Error(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(ErrorResponse{Error: message})
}

// APIKeyValidator interface for API key validation
type APIKeyValidator interface {
	Get(key string) (interface{}, bool)
}

// AuthMiddleware provides authentication wrapping functionality
type AuthMiddleware struct {
	apiKeys APIKeyValidator
}

// NewAuthMiddleware creates a new auth middleware
func NewAuthMiddleware(apiKeys APIKeyValidator) *AuthMiddleware {
	return &AuthMiddleware{apiKeys: apiKeys}
}

// RequireAPIKey wraps a handler to require valid API key
func (a *AuthMiddleware) RequireAPIKey(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		apiKey := r.Header.Get("X-API-Key")
		if apiKey == "" {
			Error(w, http.StatusUnauthorized, "API key required")
			return
		}

		_, ok := a.apiKeys.Get(apiKey)
		if !ok {
			Error(w, http.StatusForbidden, "invalid API key")
			return
		}

		next(w, r)
	}
}

// CORS middleware for handling cross-origin requests
func CORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next(w, r)
	}
}
