package bitcoin

import (
	"bytes"
	"context"
	"encoding/hex"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/btcsuite/btcd/chainhash/v2"
	"github.com/btcsuite/btcd/txscript/v2"
	"github.com/btcsuite/btcd/wire/v2"

	"stargate-backend/core/smart_contract"
)

func opReturnWishStego(t *testing.T, wish, stego byte) (wishHex, stegoHex string, script []byte) {
	t.Helper()
	wishBytes := bytes.Repeat([]byte{wish}, 32)
	stegoBytes := bytes.Repeat([]byte{stego}, 32)
	payload := append(append([]byte{}, wishBytes...), stegoBytes...)
	script, err := txscript.NewScriptBuilder().AddOp(txscript.OP_RETURN).AddData(payload).Script()
	if err != nil {
		t.Fatalf("op_return: %v", err)
	}
	return hex.EncodeToString(wishBytes), hex.EncodeToString(stegoBytes), script
}

func mustRawBlockHex(t *testing.T, outputs ...[]byte) string {
	t.Helper()
	var prev chainhash.Hash
	prev[0] = 0x11
	tx := wire.NewMsgTx(2)
	tx.AddTxIn(wire.NewTxIn(wire.NewOutPoint(&prev, math.MaxUint32), nil, nil))
	if len(outputs) == 0 {
		tx.AddTxOut(wire.NewTxOut(0, []byte{txscript.OP_TRUE}))
	}
	for _, script := range outputs {
		tx.AddTxOut(wire.NewTxOut(0, script))
	}
	block := wire.NewMsgBlock(&wire.BlockHeader{Version: 1})
	block.AddTransaction(tx)
	var buf bytes.Buffer
	if err := block.Serialize(&buf); err != nil {
		t.Fatalf("serialize block: %v", err)
	}
	return hex.EncodeToString(buf.Bytes())
}

func TestCommitmentsFromRawBlock(t *testing.T) {
	wish, stego, script := opReturnWishStego(t, 0xab, 0xcd)
	hits, err := commitmentsFromRawBlock(mustDecodeHex(t, mustRawBlockHex(t, script)))
	if err != nil {
		t.Fatalf("commitments: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits=%d", len(hits))
	}
	if hits[0].WishHash != wish || hits[0].StegoHash != stego || hits[0].TxID == "" {
		t.Fatalf("commitment=%+v", hits[0])
	}
	empty, err := commitmentsFromRawBlock(mustDecodeHex(t, mustRawBlockHex(t)))
	if err != nil {
		t.Fatalf("empty block: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("empty block hits=%d", len(empty))
	}
}

func mustDecodeHex(t *testing.T, s string) []byte {
	t.Helper()
	raw, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func newReplicationMonitor(t *testing.T, tip int64) (*BlockMonitor, *fullMockSweepStore) {
	t.Helper()
	t.Setenv("BLOCKS_DIR", t.TempDir())
	t.Setenv("UPLOADS_DIR", t.TempDir())
	t.Setenv("BITCOIN_NETWORK", "testnet4")
	t.Setenv("CHAIN_SETTLEMENT_CONFIRMATIONS", "20")
	resetTipLagStateForTest()
	store := &fullMockSweepStore{proofs: map[string]*smart_contract.MerkleProof{}}
	bm := NewBlockMonitor(NewBitcoinNodeClient("http://127.0.0.1:1"))
	bm.SetSweepDependencies(store, NewMempoolClient())
	bm.SetChainBackend(&mockChain{height: tip})
	return bm, store
}

func TestNoCandidateOPReturnWaitsForMirrorThenConfirms(t *testing.T) {
	wish, stego, script := opReturnWishStego(t, 0x11, 0x22)
	bm, store := newReplicationMonitor(t, 500)
	uploads := os.Getenv("UPLOADS_DIR")
	var calls int
	bm.SetStegoReconciler(StegoReconcilerFunc(func(ctx context.Context, stegoCID, _ string) error {
		calls++
		if stegoCID != stego {
			t.Errorf("reconcile cid=%s", stegoCID)
		}
		existing, err := store.GetContract(wish)
		if err == nil && (existing.Status == "confirmed" || existing.Status == "completed") {
			return nil
		}
		return store.UpsertContractWithTasks(ctx, smart_contract.Contract{
			ContractID: wish,
			Title:      "Mirrored wish",
			Status:     "active",
		}, nil)
	}))

	tx := Transaction{
		TxID: strings.Repeat("aa", 32),
		Outputs: []TxOutput{{
			Value:        0,
			ScriptPubKey: script,
		}},
	}
	parsed := &ParsedBlock{Transactions: []Transaction{tx}}
	got := bm.reconcileOracleIngestions(t.TempDir(), parsed, nil, 100)
	if len(got) != 1 || got[0].ContractID != wish {
		t.Fatalf("block contracts=%+v", got)
	}
	if got[0].Metadata["match_type"] != "op_return" || got[0].Metadata["image_file"] != stego {
		t.Fatalf("metadata=%v", got[0].Metadata)
	}
	if calls != 0 {
		t.Fatalf("reconciled before the image arrived: %d", calls)
	}
	bm.replicationMu.Lock()
	pending := len(bm.replication.Pending)
	bm.replicationMu.Unlock()
	if pending != 1 {
		t.Fatalf("pending=%d", pending)
	}
	if _, err := store.GetContract(wish); err == nil {
		t.Fatal("contract row exists before the stego image arrived")
	}

	if err := os.WriteFile(filepath.Join(uploads, stego), []byte("stego-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	bm.NoteStegoFile(stego)
	if calls != 1 {
		t.Fatalf("reconcile calls=%d", calls)
	}
	gotContract, err := store.GetContract(wish)
	if err != nil {
		t.Fatal(err)
	}
	if gotContract.Status != "confirmed" || gotContract.Title != "Mirrored wish" {
		t.Fatalf("contract=%+v", gotContract)
	}
	bm.replicationMu.Lock()
	pending = len(bm.replication.Pending)
	bm.replicationMu.Unlock()
	if pending != 0 {
		t.Fatalf("pending after confirm=%d", pending)
	}

	store.confirmErr = errConfirmRewrite
	done, _ := bm.applyChainCommitment(chainCommitment{
		Height: 100, TxID: tx.TxID, WishHash: wish, StegoHash: stego,
	})
	if !done {
		t.Fatal("second apply rewrote or failed a confirmed contract")
	}
	if calls != 2 {
		t.Fatalf("second apply reconcile calls=%d, want 2 (enrich, no confirm)", calls)
	}
}

var errConfirmRewrite = errString("confirm rewrite")

type errString string

func (e errString) Error() string { return string(e) }

func TestSnapshotStatsReadsDiscoverCursor(t *testing.T) {
	t.Setenv("BLOCKS_DIR", t.TempDir())
	t.Setenv("BLOCK_MONITOR_CONTRACT_DISCOVER", "off")
	bm := NewBlockMonitor(NewBitcoinNodeClient("http://127.0.0.1:1"))
	bm.replicationLoaded = true
	bm.replication.NextHeight = 62900
	bm.replication.Pending = []chainCommitment{{
		Height: 156928, TxID: strings.Repeat("ab", 32),
		WishHash: strings.Repeat("11", 32), StegoHash: strings.Repeat("22", 32),
	}}
	bm.discoverWaitLogged = true
	st := bm.SnapshotStats()
	if st.DiscoverEnabled {
		t.Fatal("discover reported enabled")
	}
	if st.DiscoverNext != 62900 || st.DiscoverPending != 1 || !st.DiscoverWaiting {
		t.Fatalf("stats=%+v", st)
	}
}

func TestDiscoverContractBlocksQueuesHistory(t *testing.T) {
	t.Setenv("BLOCK_MONITOR_CONTRACT_DISCOVER_STORE_ATTEMPTS", "1")
	_, stego, script := opReturnWishStego(t, 0x33, 0x44)
	bm, _ := newReplicationMonitor(t, 2)
	chain := &mockChain{
		height: 2,
		hexByHeight: map[int64]string{
			0: mustRawBlockHex(t),
			1: mustRawBlockHex(t),
			2: mustRawBlockHex(t, script),
		},
	}
	bm.SetChainBackend(chain)
	bm.discoverContractBlocks(context.Background())

	bm.replicationMu.Lock()
	defer bm.replicationMu.Unlock()
	if bm.replication.NextHeight != 3 {
		t.Fatalf("cursor=%d, pending=%d", bm.replication.NextHeight, len(bm.replication.Pending))
	}
	if len(bm.replication.Pending) != 1 {
		t.Fatalf("pending=%+v", bm.replication.Pending)
	}
	if bm.replication.Pending[0].Height != 2 || bm.replication.Pending[0].StegoHash != stego {
		t.Fatalf("pending=%+v", bm.replication.Pending[0])
	}
}
