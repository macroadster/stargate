package bitcoin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"stargate-backend/core/smart_contract"
)

func TestSanitizeInscriptionsForDisk_SVG(t *testing.T) {
	// Setup test data
	svgContent := `<svg xmlns="http://www.w3.org/2000/svg"><text>Hello</text></svg>`
	
	// Add binary garbage at the start (simulating pushdata or other noise)
	// The SVG cleanup logic should remove this by finding the first '<'.
	// The generic image logic will NOT remove this because it's not a known image signature wrapper.
	garbage := string([]byte{0x04, 0xDE, 0xAD, 0xBE, 0xEF})
	fullContent := garbage + svgContent
	
inscriptions := []InscriptionData{
		{
			TxID:        "test_tx",
			ContentType: "image/svg+xml",
			Content:     fullContent,
			FileName:    "test.svg",
		},
	}

	// Run sanitization
	cleaned := sanitizeInscriptionsForDisk(inscriptions)

	// Check results
	if len(cleaned) != 1 {
		t.Fatalf("Expected 1 inscription, got %d", len(cleaned))
	}

	result := cleaned[0].Content
	
	// If bug exists (SVG cleanup skipped), result will still contain garbage.
	// If fixed, result should be cleaned (starting with <).
	
	if result != svgContent {
		t.Errorf("SVG content was NOT cleaned up.\nExpected: %s\nGot (hex): %x", svgContent, result)
	}
}

// --- confirmContractTasks / fetchTxStatus tests ---

// fullMockSweepStore implements SweepTaskStore for testing.
type fullMockSweepStore struct {
	tasks  []smart_contract.Task
	proofs map[string]*smart_contract.MerkleProof
}

func (m *fullMockSweepStore) UpdateTaskProof(_ context.Context, taskID string, proof *smart_contract.MerkleProof) error {
	m.proofs[taskID] = proof
	return nil
}
func (m *fullMockSweepStore) ListTasks(filter smart_contract.TaskFilter) ([]smart_contract.Task, error) {
	var out []smart_contract.Task
	for _, t := range m.tasks {
		if filter.ContractID != "" && t.ContractID != filter.ContractID {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}
func (m *fullMockSweepStore) UpdateContractStatus(_ context.Context, _, _ string) error { return nil }
func (m *fullMockSweepStore) ConfirmContract(_ context.Context, _ string, _ int, _ string) error {
	return nil
}

func TestConfirmContractTasks_ConfirmedTx(t *testing.T) {
	fakeTxID := strings.Repeat("aa", 32)

	store := &fullMockSweepStore{
		proofs: make(map[string]*smart_contract.MerkleProof),
		tasks: []smart_contract.Task{
			{
				TaskID:     "task-1",
				ContractID: "contract-1",
				MerkleProof: &smart_contract.MerkleProof{
					TxID:               fakeTxID,
					ConfirmationStatus: "provisional",
				},
			},
		},
	}

	// Set up a fake Esplora endpoint (needed for NewBlockMonitor).
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := NewBitcoinNodeClient(server.URL)
	mempool := NewMempoolClient()
	bm := NewBlockMonitor(client)
	bm.SetSweepDependencies(store, mempool)

	// Act: call confirmContractTasks directly.
	bm.confirmContractTasks("contract-1", fakeTxID, 100)

	// Assert: the task proof should be updated with block height and confirmed status.
	proof := store.proofs["task-1"]
	if proof == nil {
		t.Fatal("expected task-1 proof to be updated")
	}
	if proof.BlockHeight != 100 {
		t.Errorf("expected BlockHeight=100, got %d", proof.BlockHeight)
	}
	if proof.ConfirmationStatus != "confirmed" {
		t.Errorf("expected ConfirmationStatus=confirmed, got %q", proof.ConfirmationStatus)
	}
	if proof.TxID != fakeTxID {
		t.Errorf("expected TxID=%s, got %s", fakeTxID, proof.TxID)
	}
}

func TestFetchTxStatus_UnconfirmedTx(t *testing.T) {
	fakeTxID := strings.Repeat("bb", 32)
	txJSON := map[string]any{
		"txid": fakeTxID,
		"status": map[string]any{
			"confirmed": false,
		},
		"vout": []any{},
	}
	txBody, _ := json.Marshal(txJSON)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(txBody)
	}))
	defer server.Close()

	client := NewBitcoinNodeClient(server.URL)
	bm := NewBlockMonitor(client)

	_, _, confirmed, err := bm.fetchTxStatus(fakeTxID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if confirmed {
		t.Error("expected confirmed=false for unconfirmed tx")
	}
}

func TestConfirmContractTasks_NoSweepDeps(t *testing.T) {
	// Without sweep dependencies wired, confirmContractTasks should be a no-op (no panic).
	client := NewBitcoinNodeClient("http://localhost:0")
	bm := NewBlockMonitor(client)
	// Should not panic or error.
	bm.confirmContractTasks("c", "tx", 0)
}
