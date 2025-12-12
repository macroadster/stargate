package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"stargate-backend/auth"
)

// APIKeyHandler issues API keys via registration.
type APIKeyHandler struct {
	*BaseHandler
	issuer    auth.APIKeyIssuer
	validator auth.APIKeyValidator
}

// NewAPIKeyHandler builds an APIKeyHandler with separate issuer/validator implementations.
func NewAPIKeyHandler(issuer auth.APIKeyIssuer, validator auth.APIKeyValidator) *APIKeyHandler {
	return &APIKeyHandler{BaseHandler: NewBaseHandler(), issuer: issuer, validator: validator}
}

// HandleRegister issues a new API key for the provided email (optional).
// Request: {"email":"user@example.com"}
// Response: {"api_key":"...","email":"user@example.com"}
func (h *APIKeyHandler) HandleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var body struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.sendError(w, http.StatusBadRequest, "invalid json")
		return
	}

	email := strings.TrimSpace(body.Email)
	rec, err := h.issuer.Issue(email, "registration")
	if err != nil {
		h.sendError(w, http.StatusInternalServerError, "failed to issue api key")
		return
	}

	h.sendSuccess(w, map[string]interface{}{
		"api_key":    rec.Key,
		"email":      rec.Email,
		"created_at": rec.CreatedAt,
	})
}
