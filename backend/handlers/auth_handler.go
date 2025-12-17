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
	"strings"

	"github.com/btcsuite/btcd/btcec"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
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

// HandleRegister issues a new API key for the provided email and wallet.
// Request: {"email":"user@example.com","wallet_address":"..."}
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
	wallet := strings.TrimSpace(body.Wallet)
	if wallet == "" {
		h.sendError(w, http.StatusBadRequest, "wallet_address required")
		return
	}

	rec, err := h.issuer.Issue(email, wallet, "registration")
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
		ok, err := verifyBTCSignature(ch.Wallet, sig, ch.Nonce)
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

// verifyBTCSignature supports legacy signmessage (compact) and BIP-322 simple witness signatures.
func verifyBTCSignature(address, signature, message string) (bool, error) {
	if ok, err := verifyLegacySignMessage(address, signature, message); err == nil && ok {
		return true, nil
	}
	return verifyBIP322Simple(address, signature, message)
}

// verifyLegacySignMessage verifies a legacy Bitcoin signmessage signature (base64 compact) against a wallet address.
func verifyLegacySignMessage(address, signatureB64, message string) (bool, error) {
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

// verifyBIP322Simple implements the "simple" flow from BIP-322 for P2PKH/P2WPKH/P2SH-P2WPKH.
// It accepts witness encoded as hex (preferred) or base64, as produced by Bitcoin Core `signmessage` with a segwit address.
func verifyBIP322Simple(address, signature, message string) (bool, error) {
	params := chooseParams(address)
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
		Sequence: math.MaxUint32,
	})
	toSign.TxIn[0].Witness = witness

	commitment := sha256.Sum256([]byte("BIP0322-signed-message:" + message))
	nullData, err := txscript.NewScriptBuilder().AddOp(txscript.OP_RETURN).AddData(commitment[:]).Script()
	if err != nil {
		return false, err
	}
	toSign.AddTxOut(&wire.TxOut{Value: 0, PkScript: nullData})

	flags := txscript.StandardVerifyFlags
	sigHashes := txscript.NewTxSigHashes(toSign)
	vm, err := txscript.NewEngine(pkScript, toSign, 0, flags, nil, sigHashes, 0)
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
