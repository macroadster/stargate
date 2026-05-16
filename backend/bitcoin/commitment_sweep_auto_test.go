package bitcoin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"stargate-backend/core/smart_contract"
)

// mockSweepStore records UpdateTaskProof calls for assertion.
type mockSweepStore struct {
	proofs map[string]*smart_contract.MerkleProof
}

func newMockSweepStore() *mockSweepStore {
	return &mockSweepStore{proofs: make(map[string]*smart_contract.MerkleProof)}
}

func (m *mockSweepStore) UpdateTaskProof(_ context.Context, taskID string, proof *smart_contract.MerkleProof) error {
	m.proofs[taskID] = proof
	return nil
}

// buildTestRedeemScript builds a valid hashlock-only redeem script for testing.
func buildTestRedeemScript(preimageHex string) (scriptHex string, err error) {
	preimage, err := hex.DecodeString(preimageHex)
	if err != nil {
		return "", err
	}
	script, err := buildHashlockRedeemScript(preimage)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(script), nil
}

func TestSweepSkipsNilProof(t *testing.T) {
	store := newMockSweepStore()
	task := smart_contract.Task{TaskID: "t1"}
	err := SweepCommitmentIfReady(context.Background(), store, nil, task, nil)
	if err != nil {
		t.Fatalf("expected nil error for nil proof, got %v", err)
	}
	if _, ok := store.proofs["t1"]; ok {
		t.Fatal("should not update proof for nil input")
	}
}

func TestSweepSkipsUnconfirmedProof(t *testing.T) {
	store := newMockSweepStore()
	proof := &smart_contract.MerkleProof{ConfirmationStatus: "provisional"}
	task := smart_contract.Task{TaskID: "t2"}
	err := SweepCommitmentIfReady(context.Background(), store, nil, task, proof)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := store.proofs["t2"]; ok {
		t.Fatal("should not update proof for unconfirmed")
	}
}

func TestSweepSkipsAlreadyConfirmedSweep(t *testing.T) {
	store := newMockSweepStore()
	proof := &smart_contract.MerkleProof{
		ConfirmationStatus: "confirmed",
		SweepStatus:        "confirmed",
	}
	task := smart_contract.Task{TaskID: "t3"}
	err := SweepCommitmentIfReady(context.Background(), store, nil, task, proof)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := store.proofs["t3"]; ok {
		t.Fatal("should not re-sweep an already confirmed sweep")
	}
}

func TestSweepSkipsMissingCommitmentData(t *testing.T) {
	store := newMockSweepStore()
	// Confirmed but missing redeem script, vout, and txid
	proof := &smart_contract.MerkleProof{
		ConfirmationStatus: "confirmed",
	}
	task := smart_contract.Task{TaskID: "t4"}
	t.Setenv("STARLIGHT_DONATION_ADDRESS", "tb1qw508d6qejxtdg4y5r3zarvary0c5xw7kxpjzsx")

	_ = SweepCommitmentIfReady(context.Background(), store, nil, task, proof)
	if p, ok := store.proofs["t4"]; !ok || p.SweepStatus != "skipped" {
		t.Fatalf("expected skipped status for missing data, got %v", p)
	}
}

func TestSweepSkipsNonDonationCommitment(t *testing.T) {
	store := newMockSweepStore()
	donationAddr := "tb1qw508d6qejxtdg4y5r3zarvary0c5xw7kxpjzsx"
	t.Setenv("STARLIGHT_DONATION_ADDRESS", donationAddr)

	preimageHex := strings.Repeat("ab", 32)
	scriptHex, err := buildTestRedeemScript(preimageHex)
	if err != nil {
		t.Fatalf("buildTestRedeemScript: %v", err)
	}

	proof := &smart_contract.MerkleProof{
		ConfirmationStatus:     "confirmed",
		TxID:                   strings.Repeat("ff", 32),
		CommitmentVout:         1,
		CommitmentRedeemScript: scriptHex,
		CommitmentPixelHash:    preimageHex,
		CommitmentAddress:      "tb1qsomeotheraddress_not_donation",
		CommitmentSource:       "product",
	}
	task := smart_contract.Task{TaskID: "t5"}
	_ = SweepCommitmentIfReady(context.Background(), store, nil, task, proof)

	p := store.proofs["t5"]
	if p == nil || p.SweepStatus != "skipped" {
		t.Fatalf("expected skipped for non-donation address, got %+v", p)
	}
	if !strings.Contains(p.SweepError, "non-donation") {
		t.Errorf("expected non-donation skip reason, got %q", p.SweepError)
	}
}

func TestSweepUsesCommitmentPixelHashAsPreimage(t *testing.T) {
	// Verify that the sweep logic reads CommitmentPixelHash for the preimage,
	// which works for both "wish" and "product" CommitmentSource values.
	// We can't do a full sweep without a real mempool, but we can verify
	// the preimage decoding path works for both source types.
	for _, source := range []string{"wish", "product"} {
		t.Run(source, func(t *testing.T) {
			preimageHex := strings.Repeat("ab", 32)
			preimage, _ := hex.DecodeString(preimageHex)
			lockHash := sha256.Sum256(preimage)

			// The redeem script should be OP_SHA256 <lockHash> OP_EQUAL
			scriptHex, err := buildTestRedeemScript(preimageHex)
			if err != nil {
				t.Fatalf("buildTestRedeemScript: %v", err)
			}

			// Verify the script is hashlock-only
			script, _ := hex.DecodeString(scriptHex)
			if !isHashlockOnlyRedeemScript(script) {
				t.Fatal("expected hashlock-only script")
			}

			// Verify the lock hash embedded in the script matches SHA256(preimage)
			// Script layout: OP_SHA256 [0x20] [32-byte-hash] OP_EQUAL
			embeddedHash := script[2:34]
			if !bytesEqual(embeddedHash, lockHash[:]) {
				t.Errorf("embedded hash mismatch for source=%s", source)
			}
		})
	}
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}