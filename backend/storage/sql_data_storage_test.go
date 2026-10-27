package storage

import (
	"testing"

	"stargate-backend/bitcoin"
	"stargate-backend/storage/gormdb"
)

func TestSQLDataStorageSQLiteRoundTrip(t *testing.T) {
	path := t.TempDir() + "/blocks.db"
	store, err := NewSQLiteDataStorage(path)
	if err != nil {
		t.Fatalf("NewSQLiteDataStorage: %v", err)
	}
	defer store.Close()

	resp := &bitcoin.BlockInscriptionsResponse{
		BlockHeight:       100,
		BlockHash:         "abc",
		Timestamp:         123456,
		TotalTransactions: 3,
		Success:           true,
		Images: []bitcoin.ExtractedImageData{
			{TxID: "t1"},
		},
	}
	scans := []map[string]interface{}{
		{"is_stego": true, "confidence": 0.9, "stego_type": "lsb"},
	}
	if err := store.StoreBlockData(resp, scans); err != nil {
		t.Fatalf("StoreBlockData: %v", err)
	}

	raw, err := store.GetBlockData(100)
	if err != nil {
		t.Fatalf("GetBlockData: %v", err)
	}
	entry, ok := raw.(*BlockDataCache)
	if !ok || entry.BlockHash != "abc" {
		t.Fatalf("unexpected entry: %#v", raw)
	}
	if entry.SteganographySummary == nil || !entry.SteganographySummary.StegoDetected {
		t.Fatal("expected stego summary")
	}

	// Upsert same height
	resp.BlockHash = "def"
	if err := store.StoreBlockData(resp, nil); err != nil {
		t.Fatal(err)
	}
	raw, _ = store.GetBlockData(100)
	entry = raw.(*BlockDataCache)
	if entry.BlockHash != "def" {
		t.Fatalf("upsert hash: %s", entry.BlockHash)
	}

	maxH, err := store.GetMaxBlockHeight()
	if err != nil || maxH != 100 {
		t.Fatalf("max height: %d err=%v", maxH, err)
	}

	recent, err := store.GetRecentBlocks(5)
	if err != nil || len(recent) != 1 {
		t.Fatalf("recent: len=%d err=%v", len(recent), err)
	}

	stats := store.GetSteganographyStats()
	if stats["total_blocks"].(int64) != 1 {
		// GORM may scan as int64 or int depending on driver
		switch v := stats["total_blocks"].(type) {
		case int64:
			if v != 1 {
				t.Fatalf("stats blocks: %v", stats)
			}
		case int:
			if v != 1 {
				t.Fatalf("stats blocks: %v", stats)
			}
		default:
			t.Fatalf("stats blocks type %T: %v", stats["total_blocks"], stats)
		}
	}
}

func TestReplaceSmartContractsKeepsStegoSummary(t *testing.T) {
	path := t.TempDir() + "/blocks.db"
	store, err := NewSQLiteDataStorage(path)
	if err != nil {
		t.Fatalf("NewSQLiteDataStorage: %v", err)
	}
	defer store.Close()

	crcWish := "7b2270223a226372632d3230222c226f70223a227472616e73666572222c2274"
	crcStego := "69636b223a224c454146222c22616d74223a223130363639303236393233227d"
	realWish := "41a974b813b024a3817c9c99b5406cc5131a00406c76a4e01fd7519974ccfb40"
	resp := &bitcoin.BlockInscriptionsResponse{
		BlockHeight:       157665,
		BlockHash:         "000000000000000286c9ff46dff4ce574f78744ef9666b2daf676a36837252a0",
		Timestamp:         1793019911,
		TotalTransactions: 261,
		Success:           true,
		SmartContracts: []bitcoin.SmartContractData{
			{ContractID: realWish, BlockHeight: 157665, ImagePath: "images/real"},
			{
				ContractID:  crcWish,
				BlockHeight: 157665,
				ImagePath:   "images/" + crcStego,
				Metadata: map[string]any{
					"match_type": "op_return",
					"wish_hash":  crcWish,
					"stego_hash": crcStego,
				},
			},
		},
	}
	scans := []map[string]interface{}{{"is_stego": true, "confidence": 0.5, "stego_type": "alpha"}}
	if err := store.StoreBlockData(resp, scans); err != nil {
		t.Fatal(err)
	}
	kept, _ := bitcoin.DropForeignOPReturnContracts(resp.SmartContracts)
	if err := store.ReplaceSmartContracts(157665, kept); err != nil {
		t.Fatal(err)
	}
	raw, err := store.GetBlockData(157665)
	if err != nil {
		t.Fatal(err)
	}
	entry := raw.(*BlockDataCache)
	if len(entry.SmartContracts) != 1 || entry.SmartContracts[0].ContractID != realWish {
		t.Fatalf("contracts=%+v", entry.SmartContracts)
	}
	if entry.SteganographySummary == nil || !entry.SteganographySummary.StegoDetected || entry.BlockHash == "" {
		t.Fatalf("payload lost fields: %+v", entry.SteganographySummary)
	}
	heights, err := store.ListBlockHeights(157000, 158000)
	if err != nil || len(heights) != 1 || heights[0] != 157665 {
		t.Fatalf("heights=%v err=%v", heights, err)
	}
	if err := store.ReplaceSmartContracts(999, nil); err != nil {
		t.Fatalf("missing height: %v", err)
	}
}

func TestSQLDataStorageMemory(t *testing.T) {
	db, err := gormdb.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer gormdb.Close(db)

	store, err := NewSQLDataStorage(db)
	if err != nil {
		t.Fatal(err)
	}
	resp := &bitcoin.BlockInscriptionsResponse{BlockHeight: 1, BlockHash: "h", Success: true}
	if err := store.StoreBlockData(resp, nil); err != nil {
		t.Fatal(err)
	}
	maxH, err := store.GetMaxBlockHeight()
	if err != nil || maxH != 1 {
		t.Fatalf("max=%d err=%v", maxH, err)
	}
}

// TestSQLDataStorageSurvivesReopen ensures AutoMigrate + block data survive process restart
// (same root cause as stargate-6wu API key invalidation).
func TestSQLDataStorageSurvivesReopen(t *testing.T) {
	path := t.TempDir() + "/blocks.db"
	store1, err := NewSQLiteDataStorage(path)
	if err != nil {
		t.Fatalf("open1: %v", err)
	}
	resp := &bitcoin.BlockInscriptionsResponse{BlockHeight: 42, BlockHash: "persist", Success: true}
	if err := store1.StoreBlockData(resp, nil); err != nil {
		t.Fatal(err)
	}
	if err := store1.Close(); err != nil {
		t.Fatal(err)
	}

	store2, err := NewSQLiteDataStorage(path)
	if err != nil {
		t.Fatalf("open2 (AutoMigrate must succeed): %v", err)
	}
	defer store2.Close()

	raw, err := store2.GetBlockData(42)
	if err != nil {
		t.Fatalf("GetBlockData after reopen: %v", err)
	}
	entry, ok := raw.(*BlockDataCache)
	if !ok || entry.BlockHash != "persist" {
		t.Fatalf("unexpected after reopen: %#v", raw)
	}
}
