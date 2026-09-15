package bitcoin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"stargate-backend/core/smart_contract"
)

// recordingReconciler notes every ReconcileStego call.
type recordingReconciler struct {
	mu    sync.Mutex
	calls []string
}

func (r *recordingReconciler) ReconcileStego(_ context.Context, stegoCID, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, stegoCID)
	return nil
}

func (r *recordingReconciler) seen() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}

// stageStegoImage puts a file where reconcileOnChainArtifacts looks for it.
// Without this it logs "not yet on disk" and never reaches the reconciler, so
// the test would pass for the wrong reason.
func stageStegoImage(t *testing.T, stegoHash string) {
	t.Helper()
	uploads := t.TempDir()
	t.Setenv("UPLOADS_DIR", uploads)
	if err := os.WriteFile(filepath.Join(uploads, stegoHash), []byte("not-a-real-png"), 0o600); err != nil {
		t.Fatalf("stage stego image: %v", err)
	}
}

func provisionalTask(taskID, contractID, txid string) smart_contract.Task {
	return smart_contract.Task{
		TaskID:     taskID,
		ContractID: contractID,
		MerkleProof: &smart_contract.MerkleProof{
			TxID:               txid,
			BlockHeight:        100,
			ConfirmationStatus: "provisional",
			SeenAt:             time.Now(),
		},
	}
}

// Confirm is confirm-only. The task-proof path used to re-run stego reconcile
// after maybeConfirmContract so the confirmed-status extract branch would fire
// (stargate-22a). Extract is distribution, not settlement — confirm must not
// invoke ReconcileStego.
func TestProofConfirmPathDoesNotReconcileStego(t *testing.T) {
	t.Setenv("CHAIN_SETTLEMENT_CONFIRMATIONS", "20")
	t.Setenv("BITCOIN_NETWORK", "testnet4")
	resetTipLagStateForTest()

	const stegoHash = "beef11beef22beef33beef44beef55beef66beef77beef88beef99beef00aabb"
	stageStegoImage(t, stegoHash)

	txid := strings.Repeat("dd", 32)
	store := &fullMockSweepStore{
		proofs: make(map[string]*smart_contract.MerkleProof),
		tasks:  []smart_contract.Task{provisionalTask("task-1", "contract-22a", txid)},
		contracts: []smart_contract.Contract{{
			ContractID: "contract-22a",
			Status:     "active",
			Metadata:   map[string]interface{}{"stego_contract_id": stegoHash},
		}},
	}

	rec := &recordingReconciler{}
	bm := NewBlockMonitor(NewBitcoinNodeClient("http://localhost:0"))
	bm.SetSweepDependencies(store, NewMempoolClient())
	bm.SetChainBackend(&mockChain{height: 119})
	bm.SetStegoReconciler(rec)

	bm.promoteProvisionalProofs(119)

	if got := store.proofs["task-1"]; got == nil || got.ConfirmationStatus != "confirmed" {
		t.Fatalf("task proof was not promoted, so the no-reconcile assertion is vacuous: %+v", got)
	}
	confirmed := false
	for _, c := range store.contracts {
		if c.ContractID == "contract-22a" && strings.EqualFold(c.Status, "confirmed") {
			confirmed = true
		}
	}
	if !confirmed {
		t.Fatalf("contract was not confirmed, so this test proves nothing")
	}

	if calls := rec.seen(); len(calls) != 0 {
		t.Errorf("confirm invoked ReconcileStego: %v", calls)
	}
}

// Several tasks on one contract promote in the same pass. Confirm still must
// not call ReconcileStego, once or many times.
func TestProofConfirmDoesNotReconcilePerTask(t *testing.T) {
	t.Setenv("CHAIN_SETTLEMENT_CONFIRMATIONS", "20")
	t.Setenv("BITCOIN_NETWORK", "testnet4")
	resetTipLagStateForTest()

	const stegoHash = "cafe11cafe22cafe33cafe44cafe55cafe66cafe77cafe88cafe99cafe00ddee"
	stageStegoImage(t, stegoHash)

	txid := strings.Repeat("ab", 32)
	store := &fullMockSweepStore{
		proofs: make(map[string]*smart_contract.MerkleProof),
		tasks: []smart_contract.Task{
			provisionalTask("task-1", "contract-shared", txid),
			provisionalTask("task-2", "contract-shared", txid),
			provisionalTask("task-3", "contract-shared", txid),
		},
		contracts: []smart_contract.Contract{{
			ContractID: "contract-shared",
			Status:     "active",
			Metadata:   map[string]interface{}{"stego_contract_id": stegoHash},
		}},
	}

	rec := &recordingReconciler{}
	bm := NewBlockMonitor(NewBitcoinNodeClient("http://localhost:0"))
	bm.SetSweepDependencies(store, NewMempoolClient())
	bm.SetChainBackend(&mockChain{height: 119})
	bm.SetStegoReconciler(rec)

	bm.promoteProvisionalProofs(119)

	for _, id := range []string{"task-1", "task-2", "task-3"} {
		if got := store.proofs[id]; got == nil || got.ConfirmationStatus != "confirmed" {
			t.Fatalf("%s was not promoted, so the no-reconcile assertion is vacuous: %+v", id, got)
		}
	}
	if calls := rec.seen(); len(calls) != 0 {
		t.Errorf("confirm invoked ReconcileStego %d times: %v", len(calls), calls)
	}
}

// A contract with no stego hash has nothing to reconcile from, and must not
// reach the reconciler at all.
func TestProofConfirmSkipsContractsWithoutStegoHash(t *testing.T) {
	t.Setenv("CHAIN_SETTLEMENT_CONFIRMATIONS", "20")
	t.Setenv("BITCOIN_NETWORK", "testnet4")
	resetTipLagStateForTest()

	txid := strings.Repeat("cd", 32)
	store := &fullMockSweepStore{
		proofs: make(map[string]*smart_contract.MerkleProof),
		tasks:  []smart_contract.Task{provisionalTask("task-plain", "contract-plain", txid)},
		contracts: []smart_contract.Contract{{
			ContractID: "contract-plain",
			Status:     "active",
			Metadata:   map[string]interface{}{},
		}},
	}

	rec := &recordingReconciler{}
	bm := NewBlockMonitor(NewBitcoinNodeClient("http://localhost:0"))
	bm.SetSweepDependencies(store, NewMempoolClient())
	bm.SetChainBackend(&mockChain{height: 119})
	bm.SetStegoReconciler(rec)

	bm.promoteProvisionalProofs(119)

	if got := store.proofs["task-plain"]; got == nil || got.ConfirmationStatus != "confirmed" {
		t.Fatalf("task was not promoted, so this test proves nothing: %+v", got)
	}
	if calls := rec.seen(); len(calls) != 0 {
		t.Errorf("reconciled a contract carrying no stego hash: %v", calls)
	}
}

// maybeConfirmContract has to report whether it confirmed, because that is what
// gates the follow-up. A settlement-not-ready bail must read as false.
func TestMaybeConfirmContractReportsWhetherItConfirmed(t *testing.T) {
	t.Setenv("CHAIN_SETTLEMENT_CONFIRMATIONS", "20")
	t.Setenv("BITCOIN_NETWORK", "testnet4")
	resetTipLagStateForTest()

	store := &fullMockSweepStore{
		proofs: make(map[string]*smart_contract.MerkleProof),
		contracts: []smart_contract.Contract{{
			ContractID: "contract-report",
			Status:     "active",
		}},
	}
	bm := NewBlockMonitor(NewBitcoinNodeClient("http://localhost:0"))
	bm.SetSweepDependencies(store, NewMempoolClient())
	bm.SetChainBackend(&mockChain{height: 119})

	if !bm.maybeConfirmContract("contract-report", strings.Repeat("dd", 32), 100) {
		t.Error("a contract 19 blocks deep with 20 required should confirm and report true")
	}
	// Two blocks deep against 20 required: not settled, so no confirm.
	if bm.maybeConfirmContract("contract-report", strings.Repeat("dd", 32), 118) {
		t.Error("reported a confirm while settlement was not ready")
	}
	if bm.maybeConfirmContract("", strings.Repeat("dd", 32), 100) {
		t.Error("reported a confirm for an empty contract id")
	}
}

// A failed ConfirmContract must read as false. Confirm is confirm-only, so a
// failed confirm also must not reach the reconciler.
//
// This case is here because a mutation survived without it. The shared mock
// never failed, so returning true after a ConfirmContract error was
// indistinguishable from correct behaviour.
func TestProofConfirmDoesNotReconcileWhenConfirmFails(t *testing.T) {
	t.Setenv("CHAIN_SETTLEMENT_CONFIRMATIONS", "20")
	t.Setenv("BITCOIN_NETWORK", "testnet4")
	resetTipLagStateForTest()

	const stegoHash = "dead11dead22dead33dead44dead55dead66dead77dead88dead99dead00bbcc"
	stageStegoImage(t, stegoHash)

	store := &fullMockSweepStore{
		proofs:     make(map[string]*smart_contract.MerkleProof),
		tasks:      []smart_contract.Task{provisionalTask("task-fail", "contract-fail", strings.Repeat("dd", 32))},
		confirmErr: errors.New("store is down"),
		contracts: []smart_contract.Contract{{
			ContractID: "contract-fail",
			Status:     "active",
			Metadata:   map[string]interface{}{"stego_contract_id": stegoHash},
		}},
	}

	rec := &recordingReconciler{}
	bm := NewBlockMonitor(NewBitcoinNodeClient("http://localhost:0"))
	bm.SetSweepDependencies(store, NewMempoolClient())
	bm.SetChainBackend(&mockChain{height: 119})
	bm.SetStegoReconciler(rec)

	if bm.maybeConfirmContract("contract-fail", strings.Repeat("dd", 32), 100) {
		t.Error("reported a confirm after ConfirmContract returned an error")
	}

	// And through the real caller: the proof still promotes, the contract does not
	// confirm, so nothing should reconcile.
	bm.promoteProvisionalProofs(119)
	if got := store.proofs["task-fail"]; got == nil || got.ConfirmationStatus != "confirmed" {
		t.Fatalf("task proof was not promoted, so this test proves nothing: %+v", got)
	}
	if calls := rec.seen(); len(calls) != 0 {
		t.Errorf("reconciled despite a failed confirm: %v", calls)
	}
}

// promoteFundedContracts used to re-run reconcileOnChainArtifacts after
// ConfirmContract (stargate-4u5) so the confirmed-status extract would fire.
func TestPromoteFundedContractsDoesNotReconcileAfterConfirm(t *testing.T) {
	t.Setenv("CHAIN_SETTLEMENT_CONFIRMATIONS", "20")
	t.Setenv("BITCOIN_NETWORK", "testnet4")
	resetTipLagStateForTest()

	const stegoHash = "feed11feed22feed33feed44feed55feed66feed77feed88feed99feed00aabb"
	stageStegoImage(t, stegoHash)

	store := &fullMockSweepStore{
		proofs: make(map[string]*smart_contract.MerkleProof),
		contracts: []smart_contract.Contract{{
			ContractID: "wish-funded",
			Status:     "funded",
			Metadata: map[string]interface{}{
				"confirmed_height": int64(100),
				"confirmed_txid":   strings.Repeat("22", 32),
				"stego_hash":       stegoHash,
			},
		}},
	}
	rec := &recordingReconciler{}
	bm := NewBlockMonitor(NewBitcoinNodeClient("http://localhost:0"))
	bm.SetSweepDependencies(store, NewMempoolClient())
	bm.SetChainBackend(&mockChain{height: 119})
	bm.SetStegoReconciler(rec)

	bm.promoteFundedContracts(119)
	if store.contracts[0].Status != "confirmed" {
		t.Fatalf("funded contract was not confirmed, got %q", store.contracts[0].Status)
	}
	if calls := rec.seen(); len(calls) != 0 {
		t.Errorf("promoteFundedContracts invoked ReconcileStego: %v", calls)
	}
}
