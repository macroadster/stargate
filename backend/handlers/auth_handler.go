package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"stargate-backend/bitcoin"
	auth "stargate-backend/storage/auth"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
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

// HandleLogin verifies an existing API key.
// Request: {"api_key":"..."}
// Response: { "valid": true }
func (h *APIKeyHandler) HandleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if h.validator == nil {
		h.sendError(w, http.StatusServiceUnavailable, "api key store unavailable")
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

	apiKey := strings.TrimSpace(body.APIKey)
	if apiKey == "" {
		h.sendError(w, http.StatusBadRequest, "api_key required")
		return
	}
	if !h.validator.Validate(apiKey) {
		h.sendError(w, http.StatusForbidden, "invalid api key")
		return
	}

	wallet := strings.TrimSpace(body.Wallet)
	if wallet != "" {
		rec, found := h.validator.Get(apiKey)
		bound := ""
		if found {
			bound = strings.TrimSpace(rec.Wallet)
		}
		if bound == "" {
			h.sendError(w, http.StatusForbidden, "wallet binding requires POST /api/auth/challenge then POST /api/auth/verify")
			return
		}
		if bound != wallet {
			h.sendError(w, http.StatusForbidden, "wallet already bound; rebind requires verification")
			return
		}
	}

	h.setAPIKeyCookie(w, r, apiKey)

	h.sendSuccess(w, map[string]interface{}{
		"valid":   true,
		"api_key": apiKey,
		"wallet":  wallet,
	})
}

func (h *APIKeyHandler) setAPIKeyCookie(w http.ResponseWriter, r *http.Request, apiKey string) {
	secure := r.TLS != nil || os.Getenv("NODE_ENV") == "production"
	http.SetCookie(w, &http.Cookie{
		Name:     "X-API-Key",
		Value:    apiKey,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   86400 * 30, // 30 days
	})
}

// HandleLogout clears the API key cookie.
func (h *APIKeyHandler) HandleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "X-API-Key",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
	h.sendSuccess(w, map[string]string{"status": "logged out"})
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
		ok, err := VerifyBTCSignature(ch.Wallet, sig, strings.TrimSpace(ch.Nonce))
		if err != nil {
			return false
		}
		return ok
	}
	if !h.challenges.Verify(body.Wallet, body.Signature, verifier) {
		h.sendError(w, http.StatusForbidden, "invalid signature")
		return
	}

	// Invalidate any existing API keys for this wallet before issuing a new one
	if reissuer, ok := h.issuer.(auth.APIKeyWalletReissuer); ok {
		if err := reissuer.InvalidateByWallet(body.Wallet); err != nil {
			h.sendError(w, http.StatusInternalServerError, "failed to invalidate existing keys")
			return
		}
	}

	rec, err := h.issuer.Issue(body.Email, body.Wallet, "wallet-verify")
	if err != nil {
		h.sendError(w, http.StatusInternalServerError, "failed to issue api key")
		return
	}

	h.setAPIKeyCookie(w, r, rec.Key)

	h.sendSuccess(w, map[string]interface{}{
		"api_key":  rec.Key,
		"wallet":   rec.Wallet,
		"email":    rec.Email,
		"verified": true,
	})
}

// VerifyBTCSignature is a transport-layer alias for bitcoin.VerifyBTCSignature.
func VerifyBTCSignature(address, signature, message string) (bool, error) {
	return bitcoin.VerifyBTCSignature(address, signature, message)
}

// VerifyBTCSignatureWithDetails is a transport-layer alias.
func VerifyBTCSignatureWithDetails(address, signature, message string) bitcoin.SignatureVerificationResult {
	return bitcoin.VerifyBTCSignatureWithDetails(address, signature, message)
}

// SignatureVerificationResult is the historical name used by MCP and tests.
type SignatureVerificationResult = bitcoin.SignatureVerificationResult

// VerifyLegacySignMessage is a transport-layer alias.
func VerifyLegacySignMessage(address, signatureB64, message string) (bool, error) {
	return bitcoin.VerifyLegacySignMessage(address, signatureB64, message)
}

func hashBitcoinMessage(message string) []byte {
	return bitcoin.HashBitcoinMessage(message)
}

// VerifyBIP322Simple is a transport-layer alias.
func VerifyBIP322Simple(address, signature, message string) (bool, error) {
	return bitcoin.VerifyBIP322Simple(address, signature, message)
}

// ChooseParams is a transport-layer alias.
func ChooseParams(address string) *chaincfg.Params {
	return bitcoin.ChooseParams(address)
}

// DetectAddressInfo provides detailed information about a Bitcoin address.
func DetectAddressInfo(address string) AddressInfo {
	addr := strings.TrimSpace(address)
	info := AddressInfo{
		Address:     addr,
		IsValid:     false,
		AddressType: "unknown",
		Network:     "unknown",
	}

	if addr == "" {
		info.Error = "Empty address"
		return info
	}

	// Try to decode against all known networks to get detailed info
	for _, params := range []*chaincfg.Params{
		&chaincfg.MainNetParams,
		&chaincfg.TestNet4Params,
		&chaincfg.TestNet3Params,
		&chaincfg.RegressionNetParams,
	} {
		decoded, err := btcutil.DecodeAddress(addr, params)
		if err == nil {
			info.IsValid = true

			// Detect network
			switch params {
			case &chaincfg.MainNetParams:
				info.Network = "mainnet"
			case &chaincfg.TestNet4Params:
				info.Network = "testnet4"
			case &chaincfg.TestNet3Params:
				info.Network = "testnet3"
			case &chaincfg.RegressionNetParams:
				info.Network = "regtest"
			}

			// Detect address type
			switch addr := decoded.(type) {
			case *btcutil.AddressPubKeyHash:
				if len(addr.ScriptAddress()) == 20 {
					addrStr := addr.String()
					if strings.HasPrefix(strings.ToLower(addrStr), "1") || strings.HasPrefix(strings.ToLower(addrStr), "m") || strings.HasPrefix(strings.ToLower(addrStr), "n") {
						info.AddressType = "p2pkh"
					}
				}
			case *btcutil.AddressScriptHash:
				info.AddressType = "p2sh"
			case *btcutil.AddressWitnessPubKeyHash:
				info.AddressType = "p2wpkh"
			case *btcutil.AddressWitnessScriptHash:
				info.AddressType = "p2wsh"
			case *btcutil.AddressTaproot:
				info.AddressType = "p2tr"
			}

			// If this is the correct network, we can return
			if decoded.IsForNet(params) {
				return info
			}
		}
	}

	if info.IsValid {
		info.Error = "Network mismatch"
	} else {
		info.Error = "Invalid address format"
	}

	return info
}

// AddressInfo provides detailed information about a Bitcoin address for AI agents.
type AddressInfo struct {
	Address     string `json:"address"`
	IsValid     bool   `json:"is_valid"`
	AddressType string `json:"address_type"`
	Network     string `json:"network"`
	Error       string `json:"error,omitempty"`
}
