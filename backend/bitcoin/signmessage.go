package bitcoin

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math"
	"strings"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/ecdsa"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
)

// VerifyBTCSignature supports Bitcoin Core wallet "signmessage" (compact ECDSA)
// and BIP-322 simple witness signatures. Compact signmessage is a wallet
// protocol (not an app compatibility shim); both formats remain supported.
// It tries both the provided message and a hex-decoded variant to be lenient
// with wallets that interpret hex-looking nonces differently.
func VerifyBTCSignature(address, signature, message string) (bool, error) {
	result := VerifyBTCSignatureWithDetails(address, signature, message)
	return result.Success, result.Error
}

// VerifyBTCSignatureWithDetails provides detailed verification results.
func VerifyBTCSignatureWithDetails(address, signature, message string) SignatureVerificationResult {
	result := SignatureVerificationResult{Success: false}

	msgTrimmed := strings.TrimSpace(message)

	if ok, err := VerifyLegacySignMessage(address, signature, msgTrimmed); err == nil {
		if ok {
			result.Success = true
			result.Format = "legacy"
			result.Message = msgTrimmed
			return result
		}
		result.LegacyErrors = append(result.LegacyErrors, "legacy verification failed")
	} else {
		result.LegacyErrors = append(result.LegacyErrors, err.Error())
	}

	if ok, err := VerifyBIP322Simple(address, signature, msgTrimmed); err == nil {
		if ok {
			result.Success = true
			result.Format = "bip322"
			result.Message = msgTrimmed
			return result
		}
		result.BIP322Errors = append(result.BIP322Errors, "BIP-322 verification failed")
	} else {
		result.BIP322Errors = append(result.BIP322Errors, err.Error())
	}

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

// VerifyLegacySignMessage verifies a Bitcoin Core wallet signmessage signature
// (base64 compact ECDSA) against a wallet address. Name retains "Legacy" for
// historical call sites; this is protocol support.
func VerifyLegacySignMessage(address, signatureB64, message string) (bool, error) {
	params := ChooseParams(address)
	if params == nil {
		return false, fmt.Errorf("unsupported address network")
	}
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

	msgHash := HashBitcoinMessage(message)

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

// HashBitcoinMessage returns the double-SHA256 of a Bitcoin signed message.
func HashBitcoinMessage(message string) []byte {
	var buf bytes.Buffer
	_ = wire.WriteVarString(&buf, 0, "Bitcoin Signed Message:\n")
	_ = wire.WriteVarString(&buf, 0, message)
	h1 := sha256.Sum256(buf.Bytes())
	h2 := sha256.Sum256(h1[:])
	return h2[:]
}

// VerifyBIP322Simple implements the "simple" flow from BIP-322 for
// P2PKH/P2WPKH/P2SH-P2WPKH. It accepts witness encoded as hex (preferred) or
// base64, as produced by Bitcoin Core `signmessage` with a segwit address.
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

	toSign := wire.NewMsgTx(0)
	toSign.AddTxIn(&wire.TxIn{
		PreviousOutPoint: wire.OutPoint{
			Hash:  toSpend.TxHash(),
			Index: 0,
		},
		Sequence: 0,
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
	if dec, err := hex.DecodeString(s); err == nil {
		return dec, nil
	}
	return base64.StdEncoding.DecodeString(s)
}

// ChooseParams picks network params by decoding the address (prefers testnet4
// for tb1/m/n/2).
func ChooseParams(address string) *chaincfg.Params {
	addr := strings.TrimSpace(address)
	if addr == "" {
		return nil
	}

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

// SignLegacyMessage produces a base64 compact ECDSA Bitcoin signed message.
func SignLegacyMessage(priv *btcec.PrivateKey, message string) string {
	if priv == nil {
		return ""
	}
	sig := ecdsa.SignCompact(priv, HashBitcoinMessage(message), true)
	return base64.StdEncoding.EncodeToString(sig)
}
