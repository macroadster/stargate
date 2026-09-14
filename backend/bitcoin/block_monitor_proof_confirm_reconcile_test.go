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

// A contract that confirms only through task proofs must re-run stego reconcile.
//
// Reconcile starts the sandbox extract only when the contract already reads
// confirmed, so the pass at one confirmation reconciles and skips the extract.
// stargate-4u5 added the re-run for the funded path and the OP_RETURN scan path;
// the task-proof path confirmed and stopped, so the tarball was never extracted
// (stargate-22a).
func TestProofConfirmPathReconcilesSandboxArtifacts(t *testing.T) {
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

	// Positive control: without the confirm there is nothing to reconcile after,
	// and the rest of the assertion would say nothing.
	if got := store.proofs["task-1"]; got == nil || got.ConfirmationStatus != "confirmed" {
		t.Fatalf("task proof was not promoted, so the reconcile assertion is vacuous: %+v", got)
	}
	confirmed := false
	for _, c := range store.contracts {
		if c.ContractID == "contract-22a" && strings.EqualFold(c.Status, "confirmed") {
			confirmed = true
		}
	}
	if !confirmed {
		t.Fatalf("contract was not confirmed, so there is no post-confirm reconcile to observe")
	}

	calls := rec.seen()
	if len(calls) != 1 || calls[0] != stegoHash {
		t.Errorf("expected exactly one reconcile for %s after the proof confirm, got %v", stegoHash, calls)
	}
}

// Several tasks on one contract promote in the same pass. The contract should be
// reconciled once, not once per task: each reconcile re-reads the stego image and
// can spawn an extract.
func TestProofConfirmReconcilesOncePerContract(t *testing.T) {
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
			t.Fatalf("%s was not promoted, so the dedupe assertion is vacuous: %+v", id, got)
		}
	}
	if calls := rec.seen(); len(calls) != 1 {
		t.Errorf("three tasks on one contract produced %d reconciles, want 1: %v", len(calls), calls)
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

// A failed ConfirmContract must read as false, and must not trigger the
// post-confirm reconcile: the status did not change, so the extract the
// reconcile exists to start would be skipped anyway.
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
