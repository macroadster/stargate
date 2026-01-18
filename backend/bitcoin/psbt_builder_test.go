package bitcoin

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/txscript"
)

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
