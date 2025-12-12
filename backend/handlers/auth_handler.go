package handlers

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/btcsuite/btcd/btcec"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcutil"
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
		// Legacy Bitcoin signmessage verification
		ok, err := verifyBTCMessage(ch.Wallet, sig, ch.Nonce)
		if err != nil {
			return false
		}
		return ok
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

// verifyBTCMessage verifies a legacy Bitcoin signmessage signature (base64) against a wallet address.
func verifyBTCMessage(address, signatureB64, message string) (bool, error) {
	params := chooseParams(address)
	if params == nil {
		return false, fmt.Errorf("unsupported address network")
	}
	// Decode address to ensure network validity
	if _, err := btcutil.DecodeAddress(address, params); err != nil {
		return false, err
	}

	sigBytes, err := base64.StdEncoding.DecodeString(signatureB64)
	if err != nil {
		return false, err
	}
	if len(sigBytes) != 65 {
		return false, fmt.Errorf("invalid signature length")
	}

	msgHash := hashBitcoinMessage(message)

	pubKey, wasCompressed, err := btcec.RecoverCompact(btcec.S256(), sigBytes, msgHash)
	if err != nil {
		return false, err
	}

	var derivedAddr string
	if wasCompressed {
		addr, err := btcutil.NewAddressPubKey(pubKey.SerializeCompressed(), params)
		if err != nil {
			return false, err
		}
		derivedAddr = addr.AddressPubKeyHash().EncodeAddress()
	} else {
		addr, err := btcutil.NewAddressPubKey(pubKey.SerializeUncompressed(), params)
		if err != nil {
			return false, err
		}
		derivedAddr = addr.AddressPubKeyHash().EncodeAddress()
	}

	return strings.EqualFold(derivedAddr, address), nil
}

func hashBitcoinMessage(message string) []byte {
	var buf bytes.Buffer
	_ = wire.WriteVarString(&buf, 0, "Bitcoin Signed Message:\n")
	_ = wire.WriteVarString(&buf, 0, message)
	h1 := sha256.Sum256(buf.Bytes())
	h2 := sha256.Sum256(h1[:])
	return h2[:]
}

// chooseParams picks mainnet or testnet based on address prefix.
func chooseParams(address string) *chaincfg.Params {
	addr := strings.ToLower(strings.TrimSpace(address))
	if strings.HasPrefix(addr, "bc1") || strings.HasPrefix(addr, "1") || strings.HasPrefix(addr, "3") {
		return &chaincfg.MainNetParams
	}
	if strings.HasPrefix(addr, "tb1") || strings.HasPrefix(addr, "m") || strings.HasPrefix(addr, "n") || strings.HasPrefix(addr, "2") {
		return &chaincfg.TestNet3Params
	}
	return nil
}
