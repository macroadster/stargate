package handlers

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/ecdsa"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
)

func TestChooseParamsPicksTestnet4ForTaproot(t *testing.T) {
	prog := bytes.Repeat([]byte{0x01}, 32)
	addr, err := btcutil.NewAddressTaproot(prog, &chaincfg.TestNet4Params)
	if err != nil {
		t.Fatalf("failed to build taproot address: %v", err)
	}

	encoded := addr.EncodeAddress()
	t.Logf("taproot address: %s", encoded)
	if _, err := btcutil.DecodeAddress(encoded, &chaincfg.TestNet4Params); err != nil {
		t.Fatalf("expected decode with testnet4 params to succeed: %v", err)
	}
	if decoded, err := btcutil.DecodeAddress(encoded, &chaincfg.MainNetParams); err == nil && decoded.IsForNet(&chaincfg.MainNetParams) {
		t.Fatalf("address %s should not belong to mainnet", encoded)
	}

	params := chooseParams(encoded)
	if params == nil {
		t.Fatalf("expected params for address %s", encoded)
	}
	if params.Name != chaincfg.TestNet4Params.Name {
		t.Fatalf("expected %s params, got %s", chaincfg.TestNet4Params.Name, params.Name)
	}

	if decoded, err := btcutil.DecodeAddress(encoded, params); err != nil {
		t.Fatalf("decode failed with chosen params: %v", err)
	} else if decoded.EncodeAddress() != encoded {
		t.Fatalf("decoded address mismatch: %s", decoded.EncodeAddress())
	}
}

func TestChooseParamsMainnet(t *testing.T) {
	pkh := bytes.Repeat([]byte{0x02}, 20)
	addr, err := btcutil.NewAddressWitnessPubKeyHash(pkh, &chaincfg.MainNetParams)
	if err != nil {
		t.Fatalf("failed to build mainnet address: %v", err)
	}
	encoded := addr.EncodeAddress()
	t.Logf("mainnet address: %s", encoded)
	if _, err := btcutil.DecodeAddress(encoded, &chaincfg.MainNetParams); err != nil {
		t.Fatalf("expected decode with mainnet params to succeed: %v", err)
	}
	params := chooseParams(encoded)
	if params == nil {
		t.Fatalf("expected params for address %s", encoded)
	}
	if params.Name != chaincfg.MainNetParams.Name {
		t.Fatalf("expected mainnet params, got %s", params.Name)
	}
}

func TestVerifySignatureHexMessageFallback(t *testing.T) {
	msg := "08d0ff0d35038832e4ddecdcee21baa5" // hex-looking nonce
	priv, err := btcec.NewPrivateKey()
	if err != nil {
		t.Fatalf("failed to create key: %v", err)
	}
	pubKeyHash := btcutil.Hash160(priv.PubKey().SerializeCompressed())
	addr, err := btcutil.NewAddressWitnessPubKeyHash(pubKeyHash, &chaincfg.TestNet4Params)
	if err != nil {
		t.Fatalf("failed to build address: %v", err)
	}

	msgHash := hashBitcoinMessage(string(mustDecodeHex(msg)))
	sig := ecdsa.SignCompact(priv, msgHash, true)
	sigB64 := base64.StdEncoding.EncodeToString(sig)

	ok, err := verifyBTCSignature(addr.EncodeAddress(), sigB64, msg)
	if err != nil {
		t.Fatalf("verify returned error: %v", err)
	}
	if !ok {
		t.Fatalf("expected verification to pass for hex-looking message")
	}
}

func mustDecodeHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

func TestLegacySignatureAcceptsSegwitAddresses(t *testing.T) {
	msg := "hello-world"
	priv, err := btcec.NewPrivateKey()
	if err != nil {
		t.Fatalf("failed to create key: %v", err)
	}
	pubKey := priv.PubKey()
	pubKeyHash := btcutil.Hash160(pubKey.SerializeCompressed())

	addr, err := btcutil.NewAddressWitnessPubKeyHash(pubKeyHash, &chaincfg.TestNet4Params)
	if err != nil {
		t.Fatalf("failed to build address: %v", err)
	}

	hash := hashBitcoinMessage(msg)
	sig := ecdsa.SignCompact(priv, hash, true)
	sigB64 := base64.StdEncoding.EncodeToString(sig)

	ok, err := verifyLegacySignMessage(addr.EncodeAddress(), sigB64, msg)
	if err != nil {
		t.Fatalf("verify returned error: %v", err)
	}
	if !ok {
		t.Fatalf("expected signature to verify for segwit address")
	}
}
