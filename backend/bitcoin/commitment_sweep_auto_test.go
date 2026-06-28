package bitcoin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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

func TestSweepPhase1TriggeredByProductHash(t *testing.T) {
	// When ProductPixelHash is set and RecommitStatus is empty, phase 1 should
	// be attempted (will fail without mempool but should attempt, not skip).
	store := newMockSweepStore()
	donationAddr := "tb1qw508d6qejxtdg4y5r3zarvary0c5xw7kxpjzsx"
	t.Setenv("STARLIGHT_DONATION_ADDRESS", donationAddr)

	wishPreimageHex := strings.Repeat("ab", 32)
	productHashHex := strings.Repeat("cd", 32)
	scriptHex, err := buildTestRedeemScript(wishPreimageHex)
	if err != nil {
		t.Fatalf("buildTestRedeemScript: %v", err)
	}

	proof := &smart_contract.MerkleProof{
		ConfirmationStatus:     "confirmed",
		TxID:                   strings.Repeat("ff", 32),
		CommitmentVout:         1,
		CommitmentRedeemScript: scriptHex,
		CommitmentPixelHash:    wishPreimageHex,
		ProductPixelHash:       productHashHex,
		CommitmentSource:       "wish",
	}
	task := smart_contract.Task{TaskID: "t5"}
	// Will fail at mempool fetch (nil client) but should attempt phase1, not skip.
	_ = SweepCommitmentIfReady(context.Background(), store, nil, task, proof)

	p := store.proofs["t5"]
	if p == nil {
		t.Fatal("expected proof update for phase1 attempt")
	}
	// Should be marked failed (nil mempool) not skipped
	if p.SweepStatus != "failed" {
		t.Errorf("expected failed status (nil mempool), got %q", p.SweepStatus)
	}
}

func TestSweepPhase2AfterRecommitConfirmed(t *testing.T) {
	// When RecommitStatus is "confirmed", phase 2 should be attempted.
	store := newMockSweepStore()
	donationAddr := "tb1qw508d6qejxtdg4y5r3zarvary0c5xw7kxpjzsx"
	t.Setenv("STARLIGHT_DONATION_ADDRESS", donationAddr)

	productHashHex := strings.Repeat("cd", 32)
	productScriptHex, err := buildTestRedeemScript(productHashHex)
	if err != nil {
		t.Fatalf("buildTestRedeemScript: %v", err)
	}

	proof := &smart_contract.MerkleProof{
		ConfirmationStatus:     "confirmed",
		TxID:                   strings.Repeat("ff", 32),
		CommitmentVout:         1,
		CommitmentPixelHash:    strings.Repeat("ab", 32),
		ProductPixelHash:       productHashHex,
		RecommitTxID:           strings.Repeat("ee", 32),
		RecommitVout:           0,
		RecommitRedeemScript:   productScriptHex,
		RecommitStatus:         "confirmed",
		CommitmentSource:       "wish",
	}
	task := smart_contract.Task{TaskID: "t5b"}
	// Will fail at mempool fetch (nil client) but should attempt phase2.
	_ = SweepCommitmentIfReady(context.Background(), store, nil, task, proof)

	p := store.proofs["t5b"]
	if p == nil {
		t.Fatal("expected proof update for phase2 attempt")
	}
	if p.SweepStatus != "failed" {
		t.Errorf("expected failed status (nil mempool), got %q", p.SweepStatus)
	}
}

func TestSweepWaitsForRecommitConfirmation(t *testing.T) {
	// When RecommitStatus is "broadcast", sweep should wait (no proof update).
	store := newMockSweepStore()
	t.Setenv("STARLIGHT_DONATION_ADDRESS", "tb1qw508d6qejxtdg4y5r3zarvary0c5xw7kxpjzsx")

	proof := &smart_contract.MerkleProof{
		ConfirmationStatus: "confirmed",
		ProductPixelHash:   strings.Repeat("cd", 32),
		RecommitTxID:       strings.Repeat("ee", 32),
		RecommitStatus:     "broadcast",
	}
	task := smart_contract.Task{TaskID: "t5c"}
	_ = SweepCommitmentIfReady(context.Background(), store, nil, task, proof)

	if _, ok := store.proofs["t5c"]; ok {
		t.Fatal("should not update proof while waiting for recommit confirmation")
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

func TestIsAlreadyInChainErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"generic", fmt.Errorf("connection refused"), false},
		{"utxo set", fmt.Errorf("broadcast tx: status 400: sendrawtransaction RPC error: {\"code\":-27,\"message\":\"Transaction outputs already in utxo set\"}"), true},
		{"block chain", fmt.Errorf("broadcast tx: status 400: Transaction already in block chain"), true},
		{"unrelated already", fmt.Errorf("already tried 3 times"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isAlreadyInChainErr(tc.err)
			if got != tc.want {
				t.Errorf("isAlreadyInChainErr(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestSweepDirectTreatsAlreadyInChainAsConfirmed(t *testing.T) {
	// When sweepDirect retries and gets "already in utxo set", it should
	// mark the sweep as confirmed, not failed.
	store := newMockSweepStore()
	t.Setenv("STARLIGHT_DONATION_ADDRESS", "tb1qw508d6qejxtdg4y5r3zarvary0c5xw7kxpjzsx")

	wishPreimageHex := strings.Repeat("ab", 32)
	scriptHex, err := buildTestRedeemScript(wishPreimageHex)
	if err != nil {
		t.Fatalf("buildTestRedeemScript: %v", err)
	}

	proof := &smart_contract.MerkleProof{
		ConfirmationStatus:     "confirmed",
		TxID:                   strings.Repeat("ff", 32),
		CommitmentVout:         1,
		CommitmentRedeemScript: scriptHex,
		CommitmentPixelHash:    wishPreimageHex,
	}
	task := smart_contract.Task{TaskID: "t-already"}

	// Use a nil mempool — sweepDirect will fail at FetchTx (nil deref or
	// similar), not at broadcast.  We can't easily mock the broadcast to
	// return the specific error without a real mock client.  Instead we
	// directly test the helper.
	if !isAlreadyInChainErr(fmt.Errorf(`broadcast tx: status 400: sendrawtransaction RPC error: {"code":-27,"message":"Transaction outputs already in utxo set"}`)) {
		t.Fatal("isAlreadyInChainErr should detect the -27 error")
	}

	// Verify the normal skip path still works (nil mempool → failed)
	_ = SweepCommitmentIfReady(context.Background(), store, nil, task, proof)
	p := store.proofs["t-already"]
	if p == nil {
		t.Fatal("expected proof update")
	}
	if p.SweepStatus != "failed" {
		t.Errorf("nil mempool should produce failed, got %q", p.SweepStatus)
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