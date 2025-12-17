package handlers

import (
	"bytes"
	"testing"

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
