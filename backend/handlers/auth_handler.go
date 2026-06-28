package handlers

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"strings"

	auth "stargate-backend/storage/auth"

	"github.com/btcsuite/btcd/btcec/v2/ecdsa"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
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

// HandleRegister is DISABLED for security reasons.
// Email-based registration without validation is a security vulnerability.
// Use wallet challenge verification instead.
func (h *APIKeyHandler) HandleRegister(w http.ResponseWriter, r *http.Request) {
	h.sendError(w, http.StatusForbidden, "Email-based registration is disabled for security reasons. Use wallet challenge verification instead: POST /api/auth/challenge followed by POST /api/auth/verify")
	return
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

	apiKey := strings.TrimSpace(body.APIKey)
	if !h.validator.Validate(apiKey) {
		h.sendError(w, http.StatusForbidden, "invalid api key")
		return
	}

	wallet := strings.TrimSpace(body.Wallet)
	if wallet != "" {
		if getter, ok := h.validator.(interface {
			Get(string) (auth.APIKey, bool)
		}); ok {
			if rec, ok := getter.Get(apiKey); ok {
				if strings.TrimSpace(rec.Wallet) != "" && rec.Wallet != wallet {
					h.sendError(w, http.StatusForbidden, "wallet already bound; rebind requires verification")
					return
				}
			}
		}
		if updater, ok := h.validator.(auth.APIKeyWalletUpdater); ok {
			if _, err := updater.UpdateWallet(apiKey, wallet); err != nil {
				h.sendError(w, http.StatusInternalServerError, "failed to bind wallet to api key")
				return
			}
		}
	}

	// Set httpOnly cookie for security
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

	h.sendSuccess(w, map[string]interface{}{
		"valid":   true,
		"api_key": apiKey,
		"wallet":  wallet,
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

	// Set httpOnly cookie for security
	secure := r.TLS != nil || os.Getenv("NODE_ENV") == "production"
	http.SetCookie(w, &http.Cookie{
		Name:     "X-API-Key",
		Value:    rec.Key,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   86400 * 30, // 30 days
	})

	h.sendSuccess(w, map[string]interface{}{
		"api_key":  rec.Key,
		"wallet":   rec.Wallet,
		"email":    rec.Email,
		"verified": true,
	})
}

// VerifyBTCSignature supports legacy signmessage (compact) and BIP-322 simple witness signatures.
// It tries both the provided message and a hex-decoded variant to be lenient with wallets that
// interpret hex-looking nonces differently.
func VerifyBTCSignature(address, signature, message string) (bool, error) {
	result := VerifyBTCSignatureWithDetails(address, signature, message)
	return result.Success, result.Error
}

// VerifyBTCSignatureWithDetails provides detailed verification results for better error handling.
func VerifyBTCSignatureWithDetails(address, signature, message string) SignatureVerificationResult {
	result := SignatureVerificationResult{Success: false}

	msgTrimmed := strings.TrimSpace(message)

	// Try legacy signmessage first
	if ok, err := VerifyLegacySignMessage(address, signature, msgTrimmed); err == nil {
		if ok {
			result.Success = true
			result.Format = "legacy"
			result.Message = msgTrimmed
			return result
		} else {
			result.LegacyErrors = append(result.LegacyErrors, "legacy verification failed")
		}
	} else {
		result.LegacyErrors = append(result.LegacyErrors, err.Error())
	}

	// Try BIP-322 simple
	if ok, err := VerifyBIP322Simple(address, signature, msgTrimmed); err == nil {
		if ok {
			result.Success = true
			result.Format = "bip322"
			result.Message = msgTrimmed
			return result
		} else {
			result.BIP322Errors = append(result.BIP322Errors, "BIP-322 verification failed")
		}
	} else {
		result.BIP322Errors = append(result.BIP322Errors, err.Error())
	}

	// If the message is hex, also try interpreting it as raw bytes
	if hexMsg, err := hex.DecodeString(msgTrimmed); err == nil {
		msgAlt := string(hexMsg)

		if ok, err := VerifyLegacySignMessage(address, signature, msgAlt); err == nil && ok {
			result.Success = true
			result.Format = "legacy-hex-decoded"
			result.Message = msgAlt
			return result
		}

		if ok, err := VerifyBIP322Simple(address, signature, msgAlt); err == nil && ok {
			result.Success = true
			result.Format = "bip322-hex-decoded"
			result.Message = msgAlt
			return result
		}
	}

	result.Error = fmt.Errorf("signature verification failed - tried legacy and BIP-322 formats")
	return result
}

// SignatureVerificationResult provides detailed feedback about signature verification attempts.
type SignatureVerificationResult struct {
	Success      bool     `json:"success"`
	Format       string   `json:"format,omitempty"`
	Message      string   `json:"message,omitempty"`
	LegacyErrors []string `json:"legacy_errors,omitempty"`
	BIP322Errors []string `json:"bip322_errors,omitempty"`
	Error        error    `json:"-"`
}

// VerifyLegacySignMessage verifies a legacy Bitcoin signmessage signature (base64 compact) against a wallet address.
func VerifyLegacySignMessage(address, signatureB64, message string) (bool, error) {
	params := ChooseParams(address)
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

	pubKey, wasCompressed, err := ecdsa.RecoverCompact(sigBytes, msgHash)
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

	if strings.EqualFold(derivedAddr, address) {
		return true, nil
	}

	// Also allow the same key in segwit form (P2WPKH) or nested P2SH-P2WPKH for wallets
	// that emit legacy signmessage over a segwit address.
	pubKeyHash := btcutil.Hash160(pubKey.SerializeCompressed())
	if wpkh, err := btcutil.NewAddressWitnessPubKeyHash(pubKeyHash, params); err == nil {
		if strings.EqualFold(wpkh.EncodeAddress(), address) {
			return true, nil
		}
	}
	if witScript, err := txscript.NewScriptBuilder().AddOp(txscript.OP_0).AddData(pubKeyHash).Script(); err == nil {
		if sh, err := btcutil.NewAddressScriptHash(witScript, params); err == nil {
			if strings.EqualFold(sh.EncodeAddress(), address) {
				return true, nil
			}
		}
	}

	return false, nil
}

func hashBitcoinMessage(message string) []byte {
	var buf bytes.Buffer
	_ = wire.WriteVarString(&buf, 0, "Bitcoin Signed Message:\n")
	_ = wire.WriteVarString(&buf, 0, message)
	h1 := sha256.Sum256(buf.Bytes())
	h2 := sha256.Sum256(h1[:])
	return h2[:]
}

// VerifyBIP322Simple implements the "simple" flow from BIP-322 for P2PKH/P2WPKH/P2SH-P2WPKH.
// It accepts witness encoded as hex (preferred) or base64, as produced by Bitcoin Core `signmessage` with a segwit address.
func VerifyBIP322Simple(address, signature, message string) (bool, error) {
	params := ChooseParams(address)
	if params == nil {
		return false, fmt.Errorf("unsupported address network")
	}
	addr, err := btcutil.DecodeAddress(address, params)
	if err != nil {
		return false, err
	}

	sigBytes, err := decodeMaybeHexOrBase64(strings.TrimSpace(signature))
	if err != nil {
		return false, err
	}

	witness, err := parseWitness(sigBytes)
	if err != nil {
		return false, err
	}

	pkScript, err := txscript.PayToAddrScript(addr)
	if err != nil {
		return false, err
	}

	// Build toSpend (anchor) tx: one output to address with zero value.
	toSpend := wire.NewMsgTx(0)
	toSpend.AddTxIn(&wire.TxIn{
		PreviousOutPoint: wire.OutPoint{
			Hash:  chainhash.Hash{},
			Index: math.MaxUint32,
		},
		Sequence: math.MaxUint32,
	})
	toSpend.AddTxOut(&wire.TxOut{
		Value:    0,
		PkScript: pkScript,
	})

	// Build toSign tx that spends toSpend and commits to message via OP_RETURN(BIP322 prefix + sha256(msg)).
	toSign := wire.NewMsgTx(0)
	toSign.AddTxIn(&wire.TxIn{
		PreviousOutPoint: wire.OutPoint{
			Hash:  toSpend.TxHash(),
			Index: 0,
		},
		Sequence: 0, // BIP-322 simple uses non-final sequence on the spending input
	})
	toSign.TxIn[0].Witness = witness

	commitment := sha256.Sum256([]byte("BIP0322-signed-message:" + message))
	nullData, err := txscript.NewScriptBuilder().AddOp(txscript.OP_RETURN).AddData(commitment[:]).Script()
	if err != nil {
		return false, err
	}
	toSign.AddTxOut(&wire.TxOut{Value: 0, PkScript: nullData})

	flags := txscript.StandardVerifyFlags
	prevFetcher := txscript.NewCannedPrevOutputFetcher(pkScript, toSpend.TxOut[0].Value)
	sigHashes := txscript.NewTxSigHashes(toSign, prevFetcher)
	vm, err := txscript.NewEngine(pkScript, toSign, 0, flags, nil, sigHashes, toSpend.TxOut[0].Value, prevFetcher)
	if err != nil {
		return false, err
	}
	if err := vm.Execute(); err != nil {
		return false, err
	}
	return true, nil
}

// parseWitness decodes a BIP-0141 witness stack from raw bytes.
func parseWitness(b []byte) (wire.TxWitness, error) {
	r := bytes.NewReader(b)
	count, err := wire.ReadVarInt(r, 0)
	if err != nil {
		return nil, err
	}
	if count > 20 {
		return nil, fmt.Errorf("witness item count too large")
	}
	w := make(wire.TxWitness, 0, count)
	for i := uint64(0); i < count; i++ {
		data, err := wire.ReadVarBytes(r, 0, math.MaxInt32, "witness element")
		if err != nil {
			return nil, err
		}
		w = append(w, data)
	}
	if r.Len() != 0 {
		return nil, fmt.Errorf("trailing data in witness")
	}
	return w, nil
}

func decodeMaybeHexOrBase64(s string) ([]byte, error) {
	// Prefer hex (Bitcoin Core signmessagewithaddress for segwit returns hex-encoded witness).
	if dec, err := hex.DecodeString(s); err == nil {
		return dec, nil
	}
	return base64.StdEncoding.DecodeString(s)
}

// ChooseParams picks network params by decoding the address (prefers testnet4 for tb1/m/n/2).
func ChooseParams(address string) *chaincfg.Params {
	addr := strings.TrimSpace(address)
	if addr == "" {
		return nil
	}

	// Try decoding against known networks, preferring testnet4 for tb1/m/n/2 addresses.
	for _, params := range []*chaincfg.Params{
		&chaincfg.TestNet4Params,
		&chaincfg.TestNet3Params,
		&chaincfg.MainNetParams,
	} {
		if decoded, err := btcutil.DecodeAddress(addr, params); err == nil && decoded.IsForNet(params) {
			return params
		}
	}
	return nil
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
