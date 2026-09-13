package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"stargate-backend/bitcoin"
)

func writeInscriptionsJSON(t *testing.T, dir string, payload map[string]any) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "inscriptions.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadCacheSkipsReorgOverwrite(t *testing.T) {
	root := t.TempDir()
	canonical := filepath.Join(root, "000", "152", "152146_00000000")
	writeInscriptionsJSON(t, canonical, map[string]any{
		"block_height":       152146,
		"block_hash":         "canonical",
		"timestamp":          1700000000,
		"total_transactions": 2,
		"images": []map[string]any{{
			"tx_id":      "aabbcc",
			"format":     "avif",
			"size_bytes": 3609,
			"file_name":  "1175afaf30a73f32_in0_w1_i0.avif",
			"file_path":  "images/1175afaf30a73f32_in0_w1_i0.avif",
		}},
		"smart_contracts": []any{},
	})
	if err := os.MkdirAll(filepath.Join(canonical, "images"), 0o755); err != nil {
		t.Fatal(err)
	}
	reorg := filepath.Join(root, "reorgs", "000", "152", "152146_00000000")
	writeInscriptionsJSON(t, reorg, map[string]any{
		"block_height":       152146,
		"block_hash":         "orphan",
		"timestamp":          1700000000,
		"total_transactions": 1,
		"images":             []any{},
		"inscriptions":       []any{},
		"smart_contracts":    []any{},
	})

	ds := NewDataStorage(root)
	got, err := ds.GetBlockData(152146)
	if err != nil {
		t.Fatalf("GetBlockData: %v", err)
	}
	cache, ok := got.(*BlockDataCache)
	if !ok {
		t.Fatalf("unexpected type %T", got)
	}
	if cache.BlockHash != "canonical" {
		t.Fatalf("reorg copy overwrote live cache: hash=%q", cache.BlockHash)
	}
	if len(cache.Inscriptions) != 1 || len(cache.Images) != 1 {
		t.Fatalf("expected 1 inscription/image from canonical dir, got insc=%d images=%d", len(cache.Inscriptions), len(cache.Images))
	}
	if cache.TxCount != 2 {
		t.Fatalf("expected tx_count 2, got %d", cache.TxCount)
	}
}

func TestIsArchivedBlockPathUsedByFinder(t *testing.T) {
	if bitcoin.IsArchivedBlockPath("/blocks", "/blocks/reorgs/1") != true {
		t.Fatal("expected reorgs to be archived")
	}
}
