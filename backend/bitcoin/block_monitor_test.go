package bitcoin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

// --- ReconcileFundingTx tests ---

// fullMockSweepStore implements SweepTaskStore for testing ReconcileFundingTx.
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

func TestReconcileFundingTx_ConfirmedTx(t *testing.T) {
	// Set up a fake Esplora-style /tx/{txid} endpoint that returns a confirmed tx.
	walletAddr := "tb1qw508d6qejxtdg4y5r3zarvary0c5xw7kxpjzsx"
	fakeTxID := strings.Repeat("aa", 32)

	// Build a realistic scriptPubKey for the wallet address (v0 witness program).
	scriptPubKey := "0014751e76e8199196d454941c45d1b3a323f1433bd6"

	txJSON := map[string]any{
		"txid": fakeTxID,
		"status": map[string]any{
			"confirmed":    true,
			"block_height": float64(100),
			"block_time":   float64(time.Now().Unix()),
		},
		"vout": []any{
			map[string]any{
				"scriptpubkey": scriptPubKey,
				"value":        float64(50000),
			},
		},
	}
	txBody, _ := json.Marshal(txJSON)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/tx/"+fakeTxID) {
			w.Write(txBody)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	// Build a BlockMonitor with the test server as Bitcoin client.
	client := NewBitcoinNodeClient(server.URL)
	store := &fullMockSweepStore{
		proofs: make(map[string]*smart_contract.MerkleProof),
		tasks: []smart_contract.Task{
			{
				TaskID:           "task-1",
				ContractID:       "contract-1",
				ContractorWallet: walletAddr,
				MerkleProof: &smart_contract.MerkleProof{
					TxID:               fakeTxID,
					ContractorWallet:   walletAddr,
					ConfirmationStatus: "provisional",
				},
			},
		},
	}
	mempool := NewMempoolClient()
	bm := NewBlockMonitor(client)
	bm.SetSweepDependencies(store, mempool)

	// Act: call ReconcileFundingTx.
	err := bm.ReconcileFundingTx(context.Background(), "contract-1", fakeTxID)
	if err != nil {
		t.Fatalf("ReconcileFundingTx returned error: %v", err)
	}

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

func TestReconcileFundingTx_UnconfirmedTx(t *testing.T) {
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
	store := &fullMockSweepStore{proofs: make(map[string]*smart_contract.MerkleProof)}
	mempool := NewMempoolClient()
	bm := NewBlockMonitor(client)
	bm.SetSweepDependencies(store, mempool)

	err := bm.ReconcileFundingTx(context.Background(), "contract-2", fakeTxID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// No tasks should be updated for unconfirmed tx.
	if len(store.proofs) != 0 {
		t.Errorf("expected no proof updates for unconfirmed tx, got %d", len(store.proofs))
	}
}

func TestReconcileFundingTx_NoSweepDeps(t *testing.T) {
	// Without sweep dependencies wired, ReconcileFundingTx should be a no-op.
	client := NewBitcoinNodeClient("http://localhost:0")
	bm := NewBlockMonitor(client)
	err := bm.ReconcileFundingTx(context.Background(), "c", "tx")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
