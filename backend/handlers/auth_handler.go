package handlers

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/crypto"
	auth "stargate-backend/storage/auth"
)

// APIKeyHandler issues API keys via registration.
type APIKeyHandler struct {
	*BaseHandler
	issuer     auth.APIKeyIssuer
	validator  auth.APIKeyValidator
	challenges *auth.ChallengeStore
}

// NewAPIKeyHandler builds an APIKeyHandler with separate issuer/validator implementations.
func NewAPIKeyHandler(issuer auth.APIKeyIssuer, validator auth.APIKeyValidator, challenges *auth.ChallengeStore) *APIKeyHandler {
	return &APIKeyHandler{BaseHandler: NewBaseHandler(), issuer: issuer, validator: validator, challenges: challenges}
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
		Email  string `json:"email"`
		Wallet string `json:"wallet_address"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.sendError(w, http.StatusBadRequest, "invalid json")
		return
	}

	email := strings.TrimSpace(body.Email)
	rec, err := h.issuer.Issue(email, body.Wallet, "registration")
	if err != nil {
		h.sendError(w, http.StatusInternalServerError, "failed to issue api key")
		return
	}

	h.sendSuccess(w, map[string]interface{}{
		"api_key":    rec.Key,
		"email":      rec.Email,
		"wallet":     rec.Wallet,
		"created_at": rec.CreatedAt,
	})
}

// HandleLogin verifies an existing API key.
// Request: {"api_key":"..."}
// Response: { "valid": true }
func (h *APIKeyHandler) HandleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var body struct {
		APIKey string `json:"api_key"`
		Wallet string `json:"wallet_address"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.sendError(w, http.StatusBadRequest, "invalid json")
		return
	}

	if !h.validator.Validate(strings.TrimSpace(body.APIKey)) {
		h.sendError(w, http.StatusForbidden, "invalid api key")
		return
	}

	h.sendSuccess(w, map[string]interface{}{
		"valid":   true,
		"api_key": body.APIKey,
		"wallet":  strings.TrimSpace(body.Wallet),
	})
}

// HandleChallenge issues a nonce for wallet verification.
// Request: {"wallet_address":"..."}
// Response: { "nonce": "...", "expires_at": "..."}
func (h *APIKeyHandler) HandleChallenge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if h.challenges == nil {
		h.sendError(w, http.StatusServiceUnavailable, "challenge store unavailable")
		return
	}
	var body struct {
		Wallet string `json:"wallet_address"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.sendError(w, http.StatusBadRequest, "invalid json")
		return
	}
	wallet := strings.TrimSpace(body.Wallet)
	if wallet == "" {
		h.sendError(w, http.StatusBadRequest, "wallet_address required")
		return
	}
	ch, err := h.challenges.Issue(wallet)
	if err != nil {
		h.sendError(w, http.StatusInternalServerError, "failed to issue challenge")
		return
	}
	h.sendSuccess(w, ch)
}

// HandleVerify checks signature against nonce and issues an API key.
// Request: {"wallet_address":"...","signature":"..."}
// Response: { "api_key":"...","wallet":"...","verified":true }
func (h *APIKeyHandler) HandleVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if h.challenges == nil {
		h.sendError(w, http.StatusServiceUnavailable, "challenge store unavailable")
		return
	}
	var body struct {
		Wallet    string `json:"wallet_address"`
		Signature string `json:"signature"`
		Email     string `json:"email,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.sendError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if strings.TrimSpace(body.Wallet) == "" || strings.TrimSpace(body.Signature) == "" {
		h.sendError(w, http.StatusBadRequest, "wallet_address and signature required")
		return
	}
	verifier := func(ch auth.Challenge, sig string) bool {
		// If Ethereum-style address, verify EIP-191 personal_sign over nonce
		if strings.HasPrefix(strings.ToLower(ch.Wallet), "0x") {
			return verifyEthPersonalSign(ch.Wallet, sig, ch.Nonce)
		}
		// Fallback: accept exact nonce match (legacy)
		return sig == ch.Nonce
	}
	if !h.challenges.Verify(body.Wallet, body.Signature, verifier) {
		h.sendError(w, http.StatusForbidden, "invalid signature")
		return
	}
	rec, err := h.issuer.Issue(body.Email, body.Wallet, "wallet-verify")
	if err != nil {
		h.sendError(w, http.StatusInternalServerError, "failed to issue api key")
		return
	}
	h.sendSuccess(w, map[string]interface{}{
		"api_key":  rec.Key,
		"wallet":   rec.Wallet,
		"email":    rec.Email,
		"verified": true,
	})
}

// verifyEthPersonalSign checks a personal_sign signature over nonce against the given 0x wallet address.
func verifyEthPersonalSign(wallet, signatureHex, nonce string) bool {
	w := strings.ToLower(strings.TrimSpace(wallet))
	sig := strings.TrimPrefix(strings.TrimSpace(signatureHex), "0x")
	if len(sig) != 130 { // 65 bytes hex
		return false
	}
	sigBytes, err := hex.DecodeString(sig)
	if err != nil {
		return false
	}
	// Adjust V if needed (ethers often uses 27/28)
	if sigBytes[64] >= 27 {
		sigBytes[64] -= 27
	}
	msg := []byte(nonce)
	prefix := []byte("\x19Ethereum Signed Message:\n" + strconv.Itoa(len(msg)))
	hash := crypto.Keccak256Hash(append(prefix, msg...))
	pubKey, err := crypto.SigToPub(hash.Bytes(), sigBytes)
	if err != nil {
		return false
	}
	recovered := strings.ToLower(crypto.PubkeyToAddress(*pubKey).Hex())
	return recovered == w
}
