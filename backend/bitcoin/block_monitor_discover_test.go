package bitcoin

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/btcsuite/btcd/chainhash/v2"
	"github.com/btcsuite/btcd/txscript/v2"
	"github.com/btcsuite/btcd/wire/v2"

	"stargate-backend/core/smart_contract"
	"stargate-backend/services"
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
	wish, stego, script := opReturnWishStego(t, 0x11, 0x81)
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
	if len(got) != 0 {
		t.Fatalf("recorded a contract with no wish image: %+v", got)
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
		t.Fatal("contract row exists before the wish image arrived")
	}

	// The stego file alone is the product image. Other anchor protocols can
	// look like wish||stego and must not become a smart contract.
	if err := os.WriteFile(filepath.Join(uploads, stego), []byte("stego-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	got = bm.reconcileOracleIngestions(t.TempDir(), parsed, nil, 100)
	if len(got) != 0 {
		t.Fatalf("recorded a contract with only the stego file: %+v", got)
	}
	bm.NoteStegoFile(stego)
	if calls != 0 {
		t.Fatalf("reconciled from the stego file without a wish image: %d", calls)
	}

	if err := os.WriteFile(filepath.Join(uploads, wish), []byte("wish-image"), 0o600); err != nil {
		t.Fatal(err)
	}
	blockDir := t.TempDir()
	got = bm.reconcileOracleIngestions(blockDir, parsed, nil, 100)
	if len(got) != 1 || got[0].ContractID != wish {
		t.Fatalf("block contracts=%+v", got)
	}
	if got[0].Metadata["match_type"] != "op_return" || got[0].Metadata["image_file"] != wish {
		t.Fatalf("metadata=%v", got[0].Metadata)
	}
	if got[0].ImagePath != filepath.Join("images", wish) {
		t.Fatalf("image path %q", got[0].ImagePath)
	}
	copied, readErr := os.ReadFile(filepath.Join(blockDir, "images", wish))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(copied) != "wish-image" {
		t.Fatalf("copied wish image %q", copied)
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
	}, pendingClock{})
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
	t.Setenv("UPLOADS_DIR", t.TempDir())
	t.Setenv("BLOCK_MONITOR_CONTRACT_DISCOVER", "off")
	wish := strings.Repeat("11", 32)
	if err := os.WriteFile(filepath.Join(os.Getenv("UPLOADS_DIR"), wish), []byte("wish"), 0o600); err != nil {
		t.Fatal(err)
	}
	bm := NewBlockMonitor(NewBitcoinNodeClient("http://127.0.0.1:1"))
	bm.replicationLoaded = true
	bm.replication.NextHeight = 62900
	bm.replication.Pending = []chainCommitment{
		{
			Height: 156928, TxID: strings.Repeat("ab", 32),
			WishHash: wish, StegoHash: strings.Repeat("22", 32),
		},
		{
			Height: 100, TxID: strings.Repeat("cd", 32),
			WishHash: strings.Repeat("33", 32), StegoHash: strings.Repeat("44", 32),
		},
	}
	bm.discoverWaitLogged = true
	st := bm.SnapshotStats()
	if st.DiscoverEnabled {
		t.Fatal("discover reported enabled")
	}
	// The anchor with no wish image stays queued but is not a waiting contract.
	if st.DiscoverNext != 62900 || st.DiscoverPending != 1 || !st.DiscoverWaiting {
		t.Fatalf("stats=%+v", st)
	}
}

func TestDiscoverContractBlocksQueuesHistory(t *testing.T) {
	t.Setenv("BLOCK_MONITOR_CONTRACT_DISCOVER_STORE_ATTEMPTS", "1")
	_, stego, script := opReturnWishStego(t, 0x91, 0xa2)
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

func TestParseOPReturnHashesRejectsForeignPayloads(t *testing.T) {
	wish, stego, script := opReturnWishStego(t, 0xab, 0xcd)
	gotWish, gotStego, ok := parseOPReturnHashes(script)
	if !ok || gotWish != wish || gotStego != stego {
		t.Fatalf("real pair ok=%v wish=%s stego=%s", ok, gotWish, gotStego)
	}

	// The on-chain shape checked against block 157248.
	realWish, _ := hex.DecodeString("41a974b813b024a3817c9c99b5406cc5131a00406c76a4e01fd7519974ccfb40")
	realStego, _ := hex.DecodeString("693e48a65e37e6ca2d631ae43192e15448a691d0312648eafb14eea87c0bbe60")
	realScript, err := txscript.NewScriptBuilder().AddOp(txscript.OP_RETURN).AddData(append(append([]byte{}, realWish...), realStego...)).Script()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := parseOPReturnHashes(realScript); !ok {
		t.Fatal("rejected a real wish||stego commitment")
	}

	crc := []byte(`{"p":"crc-20","op":"transfer","tick":"LEAF","amt":"10000000000"}`)
	for len(crc) < 64 {
		crc = append(crc, ' ')
	}
	crc = crc[:64]
	crcScript, err := txscript.NewScriptBuilder().AddOp(txscript.OP_RETURN).AddData(crc).Script()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := parseOPReturnHashes(crcScript); ok {
		t.Fatal("accepted CRC-20 JSON as a commitment")
	}

	split, err := txscript.NewScriptBuilder().AddOp(txscript.OP_RETURN).AddData(bytes.Repeat([]byte{0xab}, 32)).AddData(bytes.Repeat([]byte{0xcd}, 32)).Script()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := parseOPReturnHashes(split); ok {
		t.Fatal("accepted two pushes concatenated to 64 bytes")
	}

	same := append(bytes.Repeat([]byte{0xab}, 32), bytes.Repeat([]byte{0xab}, 32)...)
	sameScript, err := txscript.NewScriptBuilder().AddOp(txscript.OP_RETURN).AddData(same).Script()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := parseOPReturnHashes(sameScript); ok {
		t.Fatal("accepted wish==stego")
	}

	ascii := []byte("00000000c8b6799badf159a98a41b4a82a0585637e73b7a07432e8179006f242")
	if len(ascii) != 64 {
		t.Fatalf("ascii fixture len %d", len(ascii))
	}
	asciiScript, err := txscript.NewScriptBuilder().AddOp(txscript.OP_RETURN).AddData(ascii).Script()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := parseOPReturnHashes(asciiScript); ok {
		t.Fatal("accepted ASCII-hex payload")
	}
}

func TestDropForeignOPReturnContracts(t *testing.T) {
	// tx 1487d9245bfc4d9df97c96b2846426ffad3511cf599e850f9f8caa932aab64d9
	// block 157665. The OP_RETURN is 64 bytes of CRC-20 JSON.
	crcWish := "7b2270223a226372632d3230222c226f70223a227472616e73666572222c2274"
	crcStego := "69636b223a224c454146222c22616d74223a223130363639303236393233227d"
	raw, err := hex.DecodeString(crcWish + crcStego)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"p":"crc-20","op":"transfer","tick":"LEAF","amt":"10669026923"}` {
		t.Fatalf("fixture decoded to %q", raw)
	}
	realWish := "41a974b813b024a3817c9c99b5406cc5131a00406c76a4e01fd7519974ccfb40"
	realStego := "693e48a65e37e6ca2d631ae43192e15448a691d0312648eafb14eea87c0bbe60"
	t.Setenv("UPLOADS_DIR", t.TempDir())
	if err := os.WriteFile(filepath.Join(os.Getenv("UPLOADS_DIR"), realWish), []byte("wish"), 0o644); err != nil {
		t.Fatal(err)
	}
	anchorWish := hex.EncodeToString(bytes.Repeat([]byte{0x11}, 32))
	anchorStego := hex.EncodeToString(bytes.Repeat([]byte{0x81}, 32))

	contracts := []SmartContractData{
		{
			ContractID: realWish,
			ImagePath:  "images/" + realStego,
			Metadata: map[string]any{
				"match_type": "op_return",
				"wish_hash":  realWish,
				"stego_hash": realStego,
			},
		},
		{
			ContractID: crcWish,
			ImagePath:  "images/" + crcStego,
			Metadata: map[string]any{
				"match_type":         "op_return",
				"wish_hash":          crcWish,
				"stego_hash":         crcStego,
				"visible_pixel_hash": crcWish,
				"tx_id":              "1487d9245bfc4d9df97c96b2846426ffad3511cf599e850f9f8caa932aab64d9",
			},
		},
		{
			ContractID: "wish-contract-1",
			ImagePath:  "images/stego.png",
			Metadata:   map[string]any{"match_type": "funding_txid", "wish_hash": crcWish, "stego_hash": crcStego},
		},
		{
			ContractID: anchorWish,
			ImagePath:  "images/" + anchorStego,
			Metadata: map[string]any{
				"match_type": "op_return",
				"wish_hash":  anchorWish,
				"stego_hash": anchorStego,
			},
		},
	}
	kept, dropped := DropForeignOPReturnContracts(contracts)
	if dropped != 2 || len(kept) != 2 {
		t.Fatalf("dropped=%d kept=%d %+v", dropped, len(kept), kept)
	}
	wishOnly := hex.EncodeToString(bytes.Repeat([]byte{0x91}, 32))
	wishOnlyWithImage := hex.EncodeToString(bytes.Repeat([]byte{0x92}, 32))
	if err := os.WriteFile(filepath.Join(os.Getenv("UPLOADS_DIR"), wishOnlyWithImage), []byte("wish-only"), 0o644); err != nil {
		t.Fatal(err)
	}
	contracts = append(contracts,
		SmartContractData{
			ContractID: wishOnly,
			Metadata: map[string]any{
				"match_type": "op_return",
				"wish_hash":  wishOnly,
				"tx_id":      strings.Repeat("d7", 32),
			},
		},
		SmartContractData{
			ContractID: wishOnlyWithImage,
			ImagePath:  "images/" + wishOnlyWithImage,
			Metadata: map[string]any{
				"match_type":         "op_return",
				"wish_hash":          wishOnlyWithImage,
				"visible_pixel_hash": wishOnlyWithImage,
			},
		},
	)
	kept, dropped = DropForeignOPReturnContracts(contracts)
	if dropped != 3 || len(kept) != 3 {
		t.Fatalf("dropped=%d kept=%d %+v", dropped, len(kept), kept)
	}
	if kept[0].ContractID != realWish || kept[1].ContractID != "wish-contract-1" || kept[2].ContractID != wishOnlyWithImage {
		t.Fatalf("kept %+v", kept)
	}
	again, droppedAgain := DropForeignOPReturnContracts(kept)
	if droppedAgain != 0 || len(again) != 3 {
		t.Fatalf("second pass dropped=%d kept=%d", droppedAgain, len(again))
	}
}

func opReturnWishOnly(t *testing.T, b byte) (wishHex string, script []byte) {
	t.Helper()
	raw := bytes.Repeat([]byte{b}, 32)
	script, err := txscript.NewScriptBuilder().AddOp(txscript.OP_RETURN).AddData(raw).Script()
	if err != nil {
		t.Fatalf("op_return: %v", err)
	}
	return hex.EncodeToString(raw), script
}

func TestWishOnlyOPReturnIsTheImage(t *testing.T) {
	wish, script := opReturnWishOnly(t, 0x91)
	bm, _ := newReplicationMonitor(t, 500)
	tx := Transaction{
		TxID: strings.Repeat("aa", 32),
		Outputs: []TxOutput{{
			ScriptPubKey: script,
		}},
	}
	parsed := &ParsedBlock{Transactions: []Transaction{tx}}
	if got := bm.reconcileOracleIngestions(t.TempDir(), parsed, nil, 141965); len(got) != 0 {
		t.Fatalf("recorded a wish-only anchor with no image: %+v", got)
	}
	hits, err := commitmentsFromRawBlock(mustDecodeHex(t, mustRawBlockHex(t, script)))
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("queued a wish-only anchor with no image: %+v", hits)
	}

	if err := os.WriteFile(filepath.Join(os.Getenv("UPLOADS_DIR"), wish), []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	blockDir := t.TempDir()
	got := bm.reconcileOracleIngestions(blockDir, parsed, nil, 141965)
	if len(got) != 1 || got[0].ContractID != wish || got[0].Metadata["image_file"] != wish {
		t.Fatalf("contracts=%+v", got)
	}
	if _, err := os.Stat(filepath.Join(blockDir, "images", wish)); err != nil {
		t.Fatalf("wish image was not copied into the block: %v", err)
	}
	hits, err = commitmentsFromRawBlock(mustDecodeHex(t, mustRawBlockHex(t, script)))
	if err != nil || len(hits) != 1 || hits[0].WishHash != wish || hits[0].StegoHash != "" {
		t.Fatalf("hits=%+v err=%v", hits, err)
	}
}

func TestOlderFundingCopiesWishHashImage(t *testing.T) {
	bm, _ := newReplicationMonitor(t, 121662)
	wish := hex.EncodeToString(bytes.Repeat([]byte{0x93}, 32))
	if err := os.WriteFile(filepath.Join(os.Getenv("UPLOADS_DIR"), wish), []byte("jpeg-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := &services.IngestionRecord{
		ID:       "ingestion-not-the-wish",
		Filename: "missing.png",
		Metadata: map[string]interface{}{
			"visible_pixel_hash": wish,
		},
	}
	dest, err := bm.moveIngestionImageWithFilename(t.TempDir(), rec, "QmMissingCID")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(dest) != wish {
		t.Fatalf("image name %s, want the wish hash", filepath.Base(dest))
	}
	body, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "jpeg-bytes" {
		t.Fatalf("copied %q", body)
	}
	if ContractImageName(SmartContractData{
		ContractID: "ingestion-not-the-wish",
		ImagePath:  "images/QmMissingCID",
		Metadata: map[string]any{
			"image_file":         "QmMissingCID",
			"visible_pixel_hash": wish,
		},
	}) != wish {
		t.Fatal("card did not prefer the wish image")
	}
}

func TestNoteChainCommitmentKeepsLatestWish(t *testing.T) {
	bm, _ := newReplicationMonitor(t, 500)
	wish := strings.Repeat("ab", 32)
	older := chainCommitment{Height: 5, TxID: strings.Repeat("aa", 32), WishHash: wish, StegoHash: strings.Repeat("cd", 32)}
	newer := chainCommitment{Height: 9, TxID: strings.Repeat("bb", 32), WishHash: wish, StegoHash: strings.Repeat("11", 32)}
	bm.noteChainCommitment(older)
	bm.noteChainCommitment(newer)
	bm.noteChainCommitment(chainCommitment{Height: 3, TxID: strings.Repeat("cc", 32), WishHash: wish, StegoHash: strings.Repeat("ef", 32)})
	bm.replicationMu.Lock()
	defer bm.replicationMu.Unlock()
	if len(bm.replication.Pending) != 1 {
		t.Fatalf("pending=%d %+v", len(bm.replication.Pending), bm.replication.Pending)
	}
	got := bm.replication.Pending[0]
	if got.Height != 9 || got.TxID != newer.TxID || got.StegoHash != newer.StegoHash {
		t.Fatalf("kept=%+v", got)
	}
}

func TestLoadReplicationCompactsForeignCopies(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BLOCKS_DIR", dir)
	t.Setenv("UPLOADS_DIR", t.TempDir())
	t.Setenv("BITCOIN_NETWORK", "testnet4")
	wish := strings.Repeat("ab", 32)
	crc := bytes.Repeat([]byte{'{'}, 64)
	state := map[string]any{
		"next_height": 157681,
		"pending": []map[string]any{
			{"height": 100, "txid": strings.Repeat("aa", 32), "wish_hash": wish, "stego_hash": strings.Repeat("cd", 32)},
			{"height": 110, "txid": strings.Repeat("bb", 32), "wish_hash": wish, "stego_hash": strings.Repeat("11", 32)},
			{"height": 120, "txid": strings.Repeat("cc", 32), "wish_hash": hex.EncodeToString(crc[:32]), "stego_hash": hex.EncodeToString(crc[32:])},
			{"height": 130, "txid": strings.Repeat("dd", 32), "wish_hash": wish, "stego_hash": wish},
		},
	}
	blob, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "chain-contracts.json"), blob, 0o644); err != nil {
		t.Fatal(err)
	}
	bm := NewBlockMonitor(NewBitcoinNodeClient("http://127.0.0.1:1"))
	bm.SetChainBackend(&mockChain{height: 100})
	bm.replicationMu.Lock()
	bm.loadReplicationLocked()
	if bm.replication.PendingVersion != pendingQueueVersion {
		t.Fatalf("version=%d", bm.replication.PendingVersion)
	}
	if len(bm.replication.Pending) != 1 || !bm.replication.Pending[0].Legacy || bm.replication.Pending[0].Height != 110 {
		t.Fatalf("after compact=%+v", bm.replication.Pending)
	}
	bm.replicationMu.Unlock()

	bm.retryPendingChainContracts("")
	bm.replicationMu.Lock()
	defer bm.replicationMu.Unlock()
	if len(bm.replication.Pending) != 0 {
		t.Fatalf("legacy miss still pending: %+v", bm.replication.Pending)
	}
}

func TestPendingMissExpiresAfterGraceButNotWithContract(t *testing.T) {
	bm, store := newReplicationMonitor(t, 500)
	wish := strings.Repeat("ab", 32)
	stego := strings.Repeat("cd", 32)
	old := time.Now().Add(-7 * time.Hour).Unix()
	row := chainCommitment{
		Height: 10, TxID: strings.Repeat("aa", 32),
		WishHash: wish, StegoHash: stego, MissingSince: old,
	}
	clock := pendingClock{now: time.Now().Unix(), grace: 6 * time.Hour, caught: old, pastTip: true}
	done, _ := bm.applyChainCommitment(row, clock)
	if !done {
		t.Fatal("expected an old miss to expire once the walk is caught up")
	}

	store.contracts = []smart_contract.Contract{{
		ContractID: wish,
		Status:     "active",
	}}
	done, kept := bm.applyChainCommitment(row, clock)
	if done {
		t.Fatal("expired a wish that already has a contract row")
	}
	if kept.WishHash != wish {
		t.Fatalf("kept=%+v", kept)
	}

	fresh := clock
	fresh.caught = fresh.now
	freshRow := row
	freshRow.MissingSince = fresh.now
	done, _ = bm.applyChainCommitment(freshRow, fresh)
	if done {
		t.Fatal("expired a miss before the grace elapsed")
	}
}

func TestApplyGivesUpAfterReconcileLimit(t *testing.T) {
	bm, _ := newReplicationMonitor(t, 500)
	wish := strings.Repeat("ab", 32)
	stego := strings.Repeat("cd", 32)
	uploads := os.Getenv("UPLOADS_DIR")
	if err := os.WriteFile(filepath.Join(uploads, wish), []byte("wish-image"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(uploads, stego), []byte("not-stego"), 0o600); err != nil {
		t.Fatal(err)
	}
	bm.SetStegoReconciler(StegoReconcilerFunc(func(context.Context, string, string) error {
		return errString("stego payload rejected")
	}))
	row := chainCommitment{
		Height: 500, TxID: strings.Repeat("aa", 32),
		WishHash: wish, StegoHash: stego,
		Attempts: contractDiscoverReconcileLimit - 1,
	}
	done, updated := bm.applyChainCommitment(row, pendingClock{})
	if !done {
		t.Fatal("kept retrying after the reconcile limit")
	}
	if updated.Attempts != contractDiscoverReconcileLimit {
		t.Fatalf("attempts=%d", updated.Attempts)
	}
}
