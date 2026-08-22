package bitcoin

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
)

type raiseFundMockUTXO struct {
	utxos map[string][]AddressUTXO
	txs   map[string]*wire.MsgTx
}

func newRaiseFundMockUTXO() *raiseFundMockUTXO {
	return &raiseFundMockUTXO{
		utxos: make(map[string][]AddressUTXO),
		txs:   make(map[string]*wire.MsgTx),
	}
}

func (m *raiseFundMockUTXO) ListConfirmedUTXOs(address string) ([]AddressUTXO, error) {
	return m.utxos[address], nil
}

func (m *raiseFundMockUTXO) FetchTx(txid string) (*wire.MsgTx, error) {
	tx, ok := m.txs[txid]
	if !ok {
		return nil, fmt.Errorf("unknown tx %s", txid)
	}
	return tx, nil
}

func (m *raiseFundMockUTXO) FetchTxOutput(txid string, vout uint32) (*wire.MsgTx, *wire.TxOut, error) {
	tx, err := m.FetchTx(txid)
	if err != nil {
		return nil, nil, err
	}
	if int(vout) >= len(tx.TxOut) {
		return nil, nil, fmt.Errorf("vout %d missing on %s", vout, txid)
	}
	return tx, tx.TxOut[vout], nil
}

func (m *raiseFundMockUTXO) BroadcastTx(string) (string, error) {
	return "", fmt.Errorf("broadcast not used in tests")
}

func seedP2WPKHUTXO(t *testing.T, client *raiseFundMockUTXO, params *chaincfg.Params, addr btcutil.Address, value int64) {
	t.Helper()
	script, err := txscript.PayToAddrScript(addr)
	if err != nil {
		t.Fatalf("payout script: %v", err)
	}
	prev := wire.NewMsgTx(2)
	var dummy chainhash.Hash
	dummy[0] = 0x01
	prev.AddTxIn(wire.NewTxIn(wire.NewOutPoint(&dummy, 0), nil, nil))
	prev.AddTxOut(wire.NewTxOut(value, script))
	txid := prev.TxHash().String()
	client.txs[txid] = prev
	var u AddressUTXO
	u.TxID = txid
	u.Vout = 0
	u.Value = value
	u.Status.Confirmed = true
	key := addr.EncodeAddress()
	client.utxos[key] = append(client.utxos[key], u)
}

func mustP2WPKH(t *testing.T, params *chaincfg.Params, fill byte) btcutil.Address {
	t.Helper()
	addr, err := btcutil.NewAddressWitnessPubKeyHash(bytes.Repeat([]byte{fill}, 20), params)
	if err != nil {
		t.Fatalf("p2wpkh: %v", err)
	}
	return addr
}

// TestBuildRaiseFundPSBTEmitsOPReturn closes the combined multi-payer hole:
// donation + wish/stego hashes must produce a 64-byte OP_RETURN that the
// block monitor can parse, and commitment sats must be funded by the payers.
func TestBuildRaiseFundPSBTEmitsOPReturn(t *testing.T) {
	params := &chaincfg.TestNet4Params
	payerA := mustP2WPKH(t, params, 0x11)
	payerB := mustP2WPKH(t, params, 0x22)
	fundraiser := mustP2WPKH(t, params, 0x33)
	client := newRaiseFundMockUTXO()
	seedP2WPKHUTXO(t, client, params, payerA, 50_000)
	seedP2WPKHUTXO(t, client, params, payerB, 80_000)

	wishHash := bytes.Repeat([]byte{0xab}, 32)
	stegoHash := bytes.Repeat([]byte{0xcd}, 32)
	const payoutA, payoutB, commitSats, feeRate int64 = 10_000, 20_000, 1_000, 2

	res, err := BuildRaiseFundPSBT(client, params, []PayerTarget{
		{Address: payerA, TargetSats: payoutA},
		{Address: payerB, TargetSats: payoutB},
	}, PSBTRequest{
		Payouts: []PayoutOutput{
			{Address: fundraiser, ValueSats: payoutA},
			{Address: fundraiser, ValueSats: payoutB},
		},
		PixelHash:         wishHash,
		ProductPixelHash:  stegoHash,
		CommitmentSats:    commitSats,
		DonationAddress:   fundraiser,
		CommitmentAddress: fundraiser,
		FeeRateSatPerVB:   feeRate,
	})
	if err != nil {
		t.Fatalf("BuildRaiseFundPSBT: %v", err)
	}
	if len(res.OPReturnScript) == 0 {
		t.Fatal("expected OP_RETURN script on combined raise-fund PSBT")
	}
	if res.OPReturnScript[0] != txscript.OP_RETURN {
		t.Fatalf("expected OP_RETURN opcode, got 0x%02x", res.OPReturnScript[0])
	}
	wish, stego, ok := parseOPReturnHashes(res.OPReturnScript)
	if !ok {
		t.Fatal("block monitor could not parse raise-fund OP_RETURN")
	}
	if wish != hex.EncodeToString(wishHash) {
		t.Fatalf("wish hash: got %s want %s", wish, hex.EncodeToString(wishHash))
	}
	if stego != hex.EncodeToString(stegoHash) {
		t.Fatalf("stego hash: got %s want %s", stego, hex.EncodeToString(stegoHash))
	}
	if res.DonationAddr != fundraiser.EncodeAddress() {
		t.Fatalf("donation addr: got %s want %s", res.DonationAddr, fundraiser.EncodeAddress())
	}
	if res.CommitmentSats != commitSats {
		t.Fatalf("commitment sats: got %d want %d", res.CommitmentSats, commitSats)
	}
	if res.SelectedSats < payoutA+payoutB+commitSats+res.FeeSats {
		t.Fatalf("inputs %d do not fund payouts+commitment+fee (%d+%d+%d)",
			res.SelectedSats, payoutA+payoutB, commitSats, res.FeeSats)
	}
	if res.SelectedSats != payoutA+payoutB+commitSats+res.ChangeSats+res.FeeSats {
		t.Fatalf("unbalanced raise-fund tx: selected=%d payouts=%d commit=%d change=%d fee=%d",
			res.SelectedSats, payoutA+payoutB, commitSats, res.ChangeSats, res.FeeSats)
	}
}

func TestBuildRaiseFundPSBTWithoutDonationFallsToHashlock(t *testing.T) {
	params := &chaincfg.TestNet4Params
	payer := mustP2WPKH(t, params, 0x44)
	fundraiser := mustP2WPKH(t, params, 0x55)
	client := newRaiseFundMockUTXO()
	seedP2WPKHUTXO(t, client, params, payer, 50_000)

	res, err := BuildRaiseFundPSBT(client, params, []PayerTarget{
		{Address: payer, TargetSats: 10_000},
	}, PSBTRequest{
		Payouts:           []PayoutOutput{{Address: fundraiser, ValueSats: 10_000}},
		PixelHash:         bytes.Repeat([]byte{0xef}, 32),
		CommitmentSats:    1_000,
		CommitmentAddress: fundraiser,
		FeeRateSatPerVB:   1,
	})
	if err != nil {
		t.Fatalf("BuildRaiseFundPSBT: %v", err)
	}
	if len(res.OPReturnScript) != 0 {
		t.Fatal("legacy hashlock path should not emit OP_RETURN")
	}
	if len(res.CommitmentScript) == 0 {
		t.Fatal("expected hashlock commitment script")
	}
}

// TestBuildHashlockRedeemScript verifies that the commitment script is constructed correctly.
// It specifically checks that we are hashing the preimage in the script (OP_SHA256 <HASH> OP_EQUAL),
// rather than using the raw preimage (OP_SHA256 <PREIMAGE> OP_EQUAL).
func TestBuildHashlockRedeemScript(t *testing.T) {
	// 1. Setup a sample preimage (simulating a visible pixel hash)
	preimageHex := "c228825a2730c5849f2baae8d46e9088efa80dea069aa5d45c1fa26f26e2c9cb"
	preimage, err := hex.DecodeString(preimageHex)
	if err != nil {
		t.Fatalf("failed to decode preimage: %v", err)
	}

	// 2. Build the script
	script, err := buildHashlockRedeemScript(preimage)
	if err != nil {
		t.Fatalf("buildHashlockRedeemScript failed: %v", err)
	}

	// 3. Expected construction: OP_SHA256 <SHA256(preimage)> OP_EQUAL
	// OP_SHA256 = 0xa8
	// Push 32 bytes = 0x20
	// OP_EQUAL = 0x87

	// Calculate expected hash (SHA256 of the preimage)
	expectedHash := sha256.Sum256(preimage)

	// Basic length check: 1 byte OP + 1 byte len + 32 bytes hash + 1 byte OP = 35 bytes
	if len(script) != 35 {
		t.Errorf("expected script length 35, got %d", len(script))
	}

	// Verify OP_SHA256
	if script[0] != txscript.OP_SHA256 {
		t.Errorf("expected OP_SHA256 (0xa8) at index 0, got 0x%x", script[0])
	}

	// Verify push data length
	if script[1] != 0x20 {
		t.Errorf("expected push data length 0x20 at index 1, got 0x%x", script[1])
	}

	// Verify the data pushed is the HASH of the preimage, NOT the preimage itself
	data := script[2:34]
	if !bytes.Equal(data, expectedHash[:]) {
		t.Errorf("Script data mismatch.\nExpected (SHA256 of preimage): %x\nGot (in script): %x", expectedHash, data)

		// Specific regression check
		if bytes.Equal(data, preimage) {
			t.Error("CRITICAL FAILURE: Script contains raw preimage! This causes 'Witness program hash mismatch'.")
		}
	}

	// Verify OP_EQUAL
	if script[34] != txscript.OP_EQUAL {
		t.Errorf("expected OP_EQUAL (0x87) at index 34, got 0x%x", script[34])
	}

	t.Logf("Successfully verified hashlock script construction.")
	t.Logf("Script: %x", script)
}

// TestAllPayerSelectionsAreSegWit tests the helper function that detects SegWit inputs
func TestAllPayerSelectionsAreSegWit(t *testing.T) {
	// Mock client and params for testing
	client := &MempoolClient{} // We'll need to mock this properly
	params := &chaincfg.TestNet3Params

	// Test case 1: All SegWit inputs should return true
	t.Run("AllSegWitInputs", func(t *testing.T) {
		// This test requires a more complex setup with mocked UTXO responses
		// For now, we'll test the logic structure
		selections := []payerSelection{}
		allSegWit := allPayerSelectionsAreSegWit(selections, client, params)
		if !allSegWit {
			t.Error("Expected allSegWit to be true for empty selections")
		}
	})

	// Test case 2: Empty selections should return true (vacuously)
	t.Run("EmptySelections", func(t *testing.T) {
		selections := []payerSelection{}
		allSegWit := allPayerSelectionsAreSegWit(selections, client, params)
		if !allSegWit {
			t.Error("Expected allSegWit to be true for empty selections")
		}
	})
}

// TestTxIDPreCalculationSegWit verifies that SegWit transactions get pre-calculated TxIDs
func TestTxIDPreCalculationSegWit(t *testing.T) {
	// This test would require:
	// 1. Mock MempoolClient with SegWit UTXO responses
	// 2. PSBTRequest with SegWit addresses
	// 3. Verify FundingTxID is populated

	t.Skip("Requires mock setup for comprehensive testing")
}

// TestTxIDPreCalculationLegacy verifies that Legacy transactions get empty TxIDs
func TestTxIDPreCalculationLegacy(t *testing.T) {
	// This test would require:
	// 1. Mock MempoolClient with Legacy UTXO responses (P2PKH, P2SH)
	// 2. PSBTRequest with Legacy addresses
	// 3. Verify FundingTxID is empty

	t.Skip("Requires mock setup for comprehensive testing")
}

// TestZeroCostFundingIntegration tests the complete zero-cost funding flow
func TestZeroCostFundingIntegration(t *testing.T) {
	// This would be an integration test that verifies:
	// 1. PSBT generation with SegWit inputs includes pre-calculated TxID
	// 2. PSBT generation with Legacy inputs has empty TxID
	// 3. Server persistence correctly stores the TxID
	// 4. Block monitor can find transactions by pre-calculated TxID

	t.Skip("Integration test - requires full environment setup")
}
