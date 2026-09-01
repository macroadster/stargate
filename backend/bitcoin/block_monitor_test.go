package bitcoin

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/btcsuite/btcd/txscript"

	"stargate-backend/core/smart_contract"
	"stargate-backend/services"
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
	tasks     []smart_contract.Task
	proofs    map[string]*smart_contract.MerkleProof
	contracts []smart_contract.Contract
}

func (m *fullMockSweepStore) UpdateTaskProof(_ context.Context, taskID string, proof *smart_contract.MerkleProof) error {
	if m.proofs == nil {
		m.proofs = make(map[string]*smart_contract.MerkleProof)
	}
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
	if filter.Offset > 0 {
		if filter.Offset >= len(out) {
			return nil, nil
		}
		out = out[filter.Offset:]
	}
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}
func (m *fullMockSweepStore) UpdateContractStatus(_ context.Context, _, _ string) error { return nil }
func (m *fullMockSweepStore) ConfirmContract(_ context.Context, contractID string, _ int, _ string) error {
	for i := range m.contracts {
		if m.contracts[i].ContractID == contractID {
			m.contracts[i].Status = "confirmed"
		}
	}
	return nil
}
func (m *fullMockSweepStore) ListContracts(filter smart_contract.ContractFilter) ([]smart_contract.Contract, error) {
	var out []smart_contract.Contract
	for _, c := range m.contracts {
		if filter.Status != "" && !strings.EqualFold(c.Status, filter.Status) {
			continue
		}
		out = append(out, c)
	}
	if filter.Offset > 0 {
		if filter.Offset >= len(out) {
			return nil, nil
		}
		out = out[filter.Offset:]
	}
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
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

	t.Setenv("CHAIN_SETTLEMENT_CONFIRMATIONS", "1")
	resetTipLagStateForTest()
	client := NewBitcoinNodeClient(server.URL)
	mempool := NewMempoolClient()
	bm := NewBlockMonitor(client)
	bm.SetSweepDependencies(store, mempool)
	bm.SetChainBackend(&mockChain{height: 100})

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

// TestReconcileCombinedRaiseOPReturnWithoutFundingTxID is the raise-fund
// fallback: when mixed-input FundingTxID was left empty, the monitor still
// confirms via parseOPReturnHashes on wish_hash||stego_hash.
func TestReconcileCombinedRaiseOPReturnWithoutFundingTxID(t *testing.T) {
	wishHash := bytes.Repeat([]byte{0xab}, 32)
	stegoHash := bytes.Repeat([]byte{0xcd}, 32)
	wishHex := hex.EncodeToString(wishHash)
	stegoHex := hex.EncodeToString(stegoHash)
	payload := append(append([]byte{}, wishHash...), stegoHash...)
	opReturn, err := txscript.NewScriptBuilder().AddOp(txscript.OP_RETURN).AddData(payload).Script()
	if err != nil {
		t.Fatalf("op_return: %v", err)
	}
	gotWish, gotStego, ok := parseOPReturnHashes(opReturn)
	if !ok || gotWish != wishHex || gotStego != stegoHex {
		t.Fatalf("parseOPReturnHashes: ok=%v wish=%s stego=%s", ok, gotWish, gotStego)
	}

	ingest, err := services.NewIngestionService(filepath.Join(t.TempDir(), "raise-monitor.db"))
	if err != nil {
		t.Fatalf("ingestion: %v", err)
	}
	// No funding_txid — the combined-raise builder left it empty (mixed inputs).
	if err := ingest.Create(services.IngestionRecord{
		ID:          "wish-" + wishHex,
		Filename:    wishHex + ".png",
		Method:      "alpha",
		ImageBase64: "ZmFrZQ==",
		Metadata:    map[string]interface{}{"visible_pixel_hash": wishHex},
		Status:      "pending",
	}); err != nil {
		t.Fatalf("create ingestion: %v", err)
	}

	store := &fullMockSweepStore{
		proofs: make(map[string]*smart_contract.MerkleProof),
		tasks: []smart_contract.Task{{
			TaskID:     "raise-task",
			ContractID: "wish-" + wishHex,
			MerkleProof: &smart_contract.MerkleProof{
				ConfirmationStatus: "provisional",
			},
		}},
	}
	t.Setenv("CHAIN_SETTLEMENT_CONFIRMATIONS", "1")
	resetTipLagStateForTest()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()
	bm := NewBlockMonitor(NewBitcoinNodeClient(srv.URL))
	bm.SetIngestionService(ingest)
	bm.SetSweepDependencies(store, NewMempoolClient())
	bm.SetChainBackend(&mockChain{height: 4242})

	parsed := &ParsedBlock{
		Transactions: []Transaction{{
			TxID: strings.Repeat("ee", 32),
			Outputs: []TxOutput{{
				Value:        0,
				ScriptPubKey: opReturn,
			}},
		}},
	}
	got := bm.reconcileOracleIngestions(t.TempDir(), parsed, nil, 4242)
	var matched *SmartContractData
	for i := range got {
		if got[i].Metadata["match_type"] == "op_return" {
			matched = &got[i]
			break
		}
	}
	if matched == nil {
		t.Fatalf("expected OP_RETURN match with empty FundingTxID, contracts=%+v", got)
	}
	if matched.Metadata["wish_hash"] != wishHex || matched.Metadata["stego_hash"] != stegoHex {
		t.Fatalf("match hashes: %+v", matched.Metadata)
	}
	proof := store.proofs["raise-task"]
	if proof == nil || proof.ConfirmationStatus != "confirmed" {
		t.Fatalf("expected confirmContractTasks via OP_RETURN, proof=%+v", proof)
	}
}

func TestReconcileFundingTxIDPathStillWorks(t *testing.T) {
	fundingTxID := strings.Repeat("aa", 32)
	ingest, err := services.NewIngestionService(filepath.Join(t.TempDir(), "txid-monitor.db"))
	if err != nil {
		t.Fatalf("ingestion: %v", err)
	}
	if err := ingest.Create(services.IngestionRecord{
		ID:          "ing-funded",
		Filename:    "funded.png",
		Method:      "alpha",
		ImageBase64: "ZmFrZQ==",
		Metadata: map[string]interface{}{
			"funding_txid":  fundingTxID,
			"funding_txids": []string{fundingTxID},
		},
		Status: "pending",
	}); err != nil {
		t.Fatalf("create ingestion: %v", err)
	}
	store := &fullMockSweepStore{
		proofs: make(map[string]*smart_contract.MerkleProof),
		tasks: []smart_contract.Task{{
			TaskID:      "funded-task",
			ContractID:  "ing-funded",
			MerkleProof: &smart_contract.MerkleProof{ConfirmationStatus: "provisional"},
		}},
	}
	t.Setenv("CHAIN_SETTLEMENT_CONFIRMATIONS", "1")
	resetTipLagStateForTest()
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	bm := NewBlockMonitor(NewBitcoinNodeClient(srv.URL))
	bm.SetIngestionService(ingest)
	bm.SetSweepDependencies(store, NewMempoolClient())
	bm.SetChainBackend(&mockChain{height: 99})

	got := bm.reconcileOracleIngestions(t.TempDir(), &ParsedBlock{
		Transactions: []Transaction{{TxID: fundingTxID}},
	}, nil, 99)
	var matched *SmartContractData
	for i := range got {
		if got[i].Metadata["match_type"] == "funding_txid" {
			matched = &got[i]
			break
		}
	}
	if matched == nil {
		t.Fatalf("expected funding_txid match, contracts=%+v", got)
	}
}

func TestConfirmContractTasks_StaysProvisionalUntilDepth(t *testing.T) {
	t.Setenv("CHAIN_SETTLEMENT_CONFIRMATIONS", "20")
	t.Setenv("BITCOIN_NETWORK", "testnet4")
	resetTipLagStateForTest()

	fakeTxID := strings.Repeat("cc", 32)
	store := &fullMockSweepStore{
		proofs: make(map[string]*smart_contract.MerkleProof),
		tasks: []smart_contract.Task{{
			TaskID:     "task-shallow",
			ContractID: "contract-shallow",
			MerkleProof: &smart_contract.MerkleProof{
				TxID:               fakeTxID,
				ConfirmationStatus: "provisional",
			},
		}},
	}
	bm := NewBlockMonitor(NewBitcoinNodeClient("http://localhost:0"))
	bm.SetSweepDependencies(store, NewMempoolClient())
	bm.SetChainBackend(&mockChain{height: 100})

	bm.confirmContractTasks("contract-shallow", fakeTxID, 100)
	proof := store.proofs["task-shallow"]
	if proof == nil {
		t.Fatal("expected proof update")
	}
	if proof.ConfirmationStatus != "provisional" {
		t.Fatalf("1-conf must stay provisional, got %q", proof.ConfirmationStatus)
	}
	if proof.BlockHeight != 100 {
		t.Fatalf("height=%d", proof.BlockHeight)
	}
}

func TestPromoteProvisionalProofs_AfterDepth(t *testing.T) {
	t.Setenv("CHAIN_SETTLEMENT_CONFIRMATIONS", "20")
	t.Setenv("BITCOIN_NETWORK", "testnet4")
	resetTipLagStateForTest()

	fakeTxID := strings.Repeat("dd", 32)
	store := &fullMockSweepStore{
		proofs: make(map[string]*smart_contract.MerkleProof),
		tasks: []smart_contract.Task{{
			TaskID:     "task-promote",
			ContractID: "contract-promote",
			MerkleProof: &smart_contract.MerkleProof{
				TxID:               fakeTxID,
				BlockHeight:        100,
				ConfirmationStatus: "provisional",
				SeenAt:             time.Now(),
			},
		}},
	}
	bm := NewBlockMonitor(NewBitcoinNodeClient("http://localhost:0"))
	bm.SetSweepDependencies(store, NewMempoolClient())
	bm.SetChainBackend(&mockChain{height: 119})

	bm.promoteProvisionalProofs(119)
	proof := store.proofs["task-promote"]
	if proof == nil || proof.ConfirmationStatus != "confirmed" {
		t.Fatalf("expected promotion at 20 confs, proof=%+v", proof)
	}
}

func TestPromoteProvisionalProofs_BlockedOnHashMismatch(t *testing.T) {
	t.Setenv("CHAIN_SETTLEMENT_CONFIRMATIONS", "1")
	resetTipLagStateForTest()
	st := EvaluateTipLag(119, 119, 3, nil, time.Now())
	ApplyTipHashCheck(st, 119, "aaa", "bbb")

	fakeTxID := strings.Repeat("ee", 32)
	store := &fullMockSweepStore{
		proofs: make(map[string]*smart_contract.MerkleProof),
		tasks: []smart_contract.Task{{
			TaskID:     "task-fork",
			ContractID: "contract-fork",
			MerkleProof: &smart_contract.MerkleProof{
				TxID:               fakeTxID,
				BlockHeight:        100,
				ConfirmationStatus: "provisional",
				SeenAt:             time.Now(),
			},
		}},
	}
	bm := NewBlockMonitor(NewBitcoinNodeClient("http://localhost:0"))
	bm.SetSweepDependencies(store, NewMempoolClient())
	bm.promoteProvisionalProofs(119)
	if _, ok := store.proofs["task-fork"]; ok {
		t.Fatal("must not promote while tip hash mismatches explorer")
	}
}

func TestPromoteProvisionalProofs_IgnoresOldSeenAt(t *testing.T) {
	t.Setenv("CHAIN_SETTLEMENT_CONFIRMATIONS", "20")
	t.Setenv("BITCOIN_NETWORK", "testnet4")
	resetTipLagStateForTest()

	fakeTxID := strings.Repeat("11", 32)
	store := &fullMockSweepStore{
		proofs: make(map[string]*smart_contract.MerkleProof),
		tasks: []smart_contract.Task{{
			TaskID:     "task-old",
			ContractID: "contract-old",
			MerkleProof: &smart_contract.MerkleProof{
				TxID:               fakeTxID,
				BlockHeight:        100,
				ConfirmationStatus: "provisional",
				SeenAt:             time.Now().Add(-15 * 24 * time.Hour),
			},
		}},
	}
	bm := NewBlockMonitor(NewBitcoinNodeClient("http://localhost:0"))
	bm.SetSweepDependencies(store, NewMempoolClient())
	bm.SetChainBackend(&mockChain{height: 119})
	bm.promoteProvisionalProofs(119)
	proof := store.proofs["task-old"]
	if proof == nil || proof.ConfirmationStatus != "confirmed" {
		t.Fatalf("15-day-old SeenAt must still promote, proof=%+v", proof)
	}
}

func TestPromoteFundedContracts_AfterNConf(t *testing.T) {
	t.Setenv("CHAIN_SETTLEMENT_CONFIRMATIONS", "20")
	t.Setenv("BITCOIN_NETWORK", "testnet4")
	resetTipLagStateForTest()

	store := &fullMockSweepStore{
		proofs: make(map[string]*smart_contract.MerkleProof),
		contracts: []smart_contract.Contract{{
			ContractID: "wish-notask",
			Status:     "funded",
			Metadata: map[string]interface{}{
				"confirmed_height": int64(100),
				"confirmed_txid":   strings.Repeat("22", 32),
				"stego_hash":       strings.Repeat("33", 32),
			},
		}},
	}
	bm := NewBlockMonitor(NewBitcoinNodeClient("http://localhost:0"))
	bm.SetSweepDependencies(store, NewMempoolClient())
	bm.SetChainBackend(&mockChain{height: 100})

	bm.promoteFundedContracts(100)
	if store.contracts[0].Status != "funded" {
		t.Fatalf("1-conf must stay funded, got %q", store.contracts[0].Status)
	}

	bm.promoteFundedContracts(119)
	if store.contracts[0].Status != "confirmed" {
		t.Fatalf("N-conf must confirm funded contract, got %q", store.contracts[0].Status)
	}
}

func TestPromoteFundedContracts_DoesNotSkipAfterFullConfirmPage(t *testing.T) {
	t.Setenv("CHAIN_SETTLEMENT_CONFIRMATIONS", "20")
	t.Setenv("BITCOIN_NETWORK", "testnet4")
	resetTipLagStateForTest()

	const n = 201
	contracts := make([]smart_contract.Contract, n)
	for i := 0; i < n; i++ {
		contracts[i] = smart_contract.Contract{
			ContractID: "wish-page-" + strconv.Itoa(i),
			Status:     "funded",
			Metadata: map[string]interface{}{
				"confirmed_height": int64(100),
				"confirmed_txid":   strings.Repeat("22", 32),
			},
		}
	}
	store := &fullMockSweepStore{contracts: contracts}
	bm := NewBlockMonitor(NewBitcoinNodeClient("http://localhost:0"))
	bm.SetSweepDependencies(store, NewMempoolClient())
	bm.SetChainBackend(&mockChain{height: 119})

	done := make(chan struct{})
	go func() {
		bm.promoteFundedContracts(119)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("promoteFundedContracts hung (offset must still advance on a full not-ready page)")
	}

	var leftover int
	for _, c := range store.contracts {
		if strings.EqualFold(c.Status, "funded") {
			leftover++
		}
	}
	if leftover != 0 {
		t.Fatalf("all %d SettlementReady funded rows must confirm in one pass, leftover=%d", n, leftover)
	}
}
