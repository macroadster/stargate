package bitcoin

import (
	"bytes"
	"strings"
	"testing"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
)

func TestDecodeAddressForNetworkRejectsWrongHRP(t *testing.T) {
	tbHash := bytes.Repeat([]byte{0x11}, 20)
	tbAddr, err := btcutil.NewAddressWitnessPubKeyHash(tbHash, &chaincfg.TestNet4Params)
	if err != nil {
		t.Fatal(err)
	}
	bcAddr, err := btcutil.NewAddressWitnessPubKeyHash(tbHash, &chaincfg.MainNetParams)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := DecodeAddressForNetwork(tbAddr.EncodeAddress(), &chaincfg.TestNet4Params); err != nil {
		t.Fatalf("tb1 on testnet4: %v", err)
	}
	if _, err := DecodeAddressForNetwork(tbAddr.EncodeAddress(), &chaincfg.SigNetParams); err != nil {
		t.Fatalf("tb1 on signet (shared HRP): %v", err)
	}
	if _, err := DecodeAddressForNetwork(tbAddr.EncodeAddress(), &chaincfg.MainNetParams); err == nil {
		t.Fatal("tb1 must not be accepted as mainnet")
	} else if !strings.Contains(err.Error(), "mainnet") {
		t.Fatalf("expected mainnet in error, got %v", err)
	}
	if _, err := DecodeAddressForNetwork(bcAddr.EncodeAddress(), &chaincfg.MainNetParams); err != nil {
		t.Fatalf("bc1 on mainnet: %v", err)
	}
	if _, err := DecodeAddressForNetwork(bcAddr.EncodeAddress(), &chaincfg.TestNet4Params); err == nil {
		t.Fatal("bc1 must not be accepted as testnet4")
	}
}
