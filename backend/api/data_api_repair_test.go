package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"stargate-backend/bitcoin"
	"stargate-backend/storage"
)

// CRC-20 transfer from tx 1487d9245bfc4d9d…ab64d9 in block 157665.
const (
	crcWish   = "7b2270223a226372632d3230222c226f70223a227472616e73666572222c2274"
	crcStego  = "69636b223a224c454146222c22616d74223a223130363639303236393233227d"
	realWish  = "41a974b813b024a3817c9c99b5406cc5131a00406c76a4e01fd7519974ccfb40"
	realStego = "693e48a65e37e6ca2d631ae43192e15448a691d0312648eafb14eea87c0bbe60"
)

func crcContract(height int64) bitcoin.SmartContractData {
	return bitcoin.SmartContractData{
		ContractID:  crcWish,
		BlockHeight: height,
		ImagePath:   "images/" + crcStego,
		Metadata: map[string]any{
			"match_type":         "op_return",
			"wish_hash":          crcWish,
			"stego_hash":         crcStego,
			"visible_pixel_hash": crcWish,
			"tx_id":              "1487d9245bfc4d9df97c96b2846426ffad3511cf599e850f9f8caa932aab64d9",
		},
	}
}

func realContract(height int64) bitcoin.SmartContractData {
	return bitcoin.SmartContractData{
		ContractID:  realWish,
		BlockHeight: height,
		ImagePath:   "images/" + realStego,
		Metadata: map[string]any{
			"match_type": "op_return",
			"wish_hash":  realWish,
			"stego_hash": realStego,
			"tx_id":      "real-funding",
		},
	}
}

func stageWishImage(t *testing.T, hash string) {
	t.Helper()
	uploads := t.TempDir()
	t.Setenv("UPLOADS_DIR", uploads)
	if err := os.WriteFile(filepath.Join(uploads, hash), []byte("wish-image"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeInscriptions(t *testing.T, root string, height int64, contracts []bitcoin.SmartContractData) string {
	t.Helper()
	dir := bitcoin.BlockDir(root, height, "00000000")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	doc := map[string]any{
		"block_hash":         "000000000000000286c9ff46dff4ce574f78744ef9666b2daf676a36837252a0",
		"block_height":       height,
		"total_transactions": 261,
		"images":             []any{},
		"inscriptions":       []any{},
		"smart_contracts":    contracts,
		"success":            true,
	}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	path := filepath.Join(dir, "inscriptions.json")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func contractIDs(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		BlockHash      string                      `json:"block_hash"`
		TotalTx        int                         `json:"total_transactions"`
		SmartContracts []bitcoin.SmartContractData `json:"smart_contracts"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.BlockHash == "" || doc.TotalTx != 261 {
		t.Fatalf("repair dropped block fields: hash=%q txs=%d", doc.BlockHash, doc.TotalTx)
	}
	ids := make([]string, 0, len(doc.SmartContracts))
	for _, c := range doc.SmartContracts {
		ids = append(ids, c.ContractID)
	}
	return ids
}

func postScan(t *testing.T, api *DataAPI, body string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/data/scan", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	api.HandleScanBlockOnDemand(w, req)
	res := w.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d body %s", res.StatusCode, w.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestScanRepairsCachedCRC20Contract(t *testing.T) {
	root := t.TempDir()
	t.Setenv("BLOCKS_DIR", root)
	stageWishImage(t, realWish)
	path := writeInscriptions(t, root, 157665, []bitcoin.SmartContractData{realContract(157665), crcContract(157665)})
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	ds := storage.NewDataStorage(root)
	api := &DataAPI{dataStorage: ds} // no block monitor: a cached scan must not reprocess

	body := postScan(t, api, `{"block_height":157665}`)
	if body["cached"] != true {
		t.Fatalf("cached=%v", body["cached"])
	}
	if body["repaired_contracts"].(float64) != 1 || body["repaired_blocks"].(float64) != 1 {
		t.Fatalf("repair counts: %+v", body)
	}
	ids := contractIDs(t, path)
	if len(ids) != 1 || ids[0] != realWish {
		t.Fatalf("file contracts=%v", ids)
	}
	if bytes.Equal(before, mustRead(t, path)) {
		t.Fatal("dirty file was not rewritten")
	}

	raw, err := ds.GetBlockData(157665)
	if err != nil {
		t.Fatal(err)
	}
	cache := raw.(*storage.BlockDataCache)
	if len(cache.SmartContracts) != 1 || cache.SmartContracts[0].ContractID != realWish {
		t.Fatalf("cache still has %+v", cache.SmartContracts)
	}
	if !blockContainsSmartContract(cache) {
		t.Fatal("real commitment should still flag the block")
	}

	// A second scan finds nothing left to repair and does not rewrite the file.
	cleaned := mustRead(t, path)
	again := postScan(t, api, `{"block_height":157665}`)
	if again["repaired_contracts"].(float64) != 0 {
		t.Fatalf("second scan repaired %+v", again)
	}
	if !bytes.Equal(cleaned, mustRead(t, path)) {
		t.Fatal("clean inscriptions.json was rewritten")
	}
}

func TestScanRepairsSQLitePayloadAndFile(t *testing.T) {
	root := t.TempDir()
	t.Setenv("BLOCKS_DIR", root)
	stageWishImage(t, realWish)
	path := writeInscriptions(t, root, 157665, []bitcoin.SmartContractData{crcContract(157665), realContract(157665)})
	store, err := storage.NewSQLiteDataStorage(filepath.Join(t.TempDir(), "blocks.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	if err := store.StoreBlockData(&bitcoin.BlockInscriptionsResponse{
		BlockHeight:       157665,
		BlockHash:         "000000000000000286c9ff46dff4ce574f78744ef9666b2daf676a36837252a0",
		Timestamp:         1793019911,
		TotalTransactions: 261,
		Success:           true,
		SmartContracts:    []bitcoin.SmartContractData{crcContract(157665), realContract(157665)},
	}, []map[string]interface{}{{"is_stego": true, "confidence": 0.4, "stego_type": "alpha"}}); err != nil {
		t.Fatal(err)
	}
	api := &DataAPI{dataStorage: store}
	body := postScan(t, api, `{"block_height":157665}`)
	if body["repaired_contracts"].(float64) != 1 {
		t.Fatalf("repair counts: %+v", body)
	}
	block := body["block_data"].(map[string]any)
	contracts := block["smart_contracts"].([]any)
	if len(contracts) != 1 {
		t.Fatalf("response contracts=%v", contracts)
	}
	got := contracts[0].(map[string]any)["contract_id"]
	if got != realWish {
		t.Fatalf("response contract=%v", got)
	}
	summary := block["steganography_summary"].(map[string]any)
	if summary["stego_detected"] != true {
		t.Fatalf("stego summary wiped: %+v", summary)
	}
	ids := contractIDs(t, path)
	if len(ids) != 1 || ids[0] != realWish {
		t.Fatalf("file contracts=%v", ids)
	}
}

func TestScanRangeRepairsWithoutChain(t *testing.T) {
	root := t.TempDir()
	t.Setenv("BLOCKS_DIR", root)
	store, err := storage.NewSQLiteDataStorage(filepath.Join(t.TempDir(), "blocks.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	for _, height := range []int64{157665, 157566} {
		writeInscriptions(t, root, height, []bitcoin.SmartContractData{crcContract(height)})
		if err := store.StoreBlockData(&bitcoin.BlockInscriptionsResponse{
			BlockHeight:    height,
			BlockHash:      "hh",
			Success:        true,
			SmartContracts: []bitcoin.SmartContractData{crcContract(height)},
		}, nil); err != nil {
			t.Fatal(err)
		}
	}
	api := &DataAPI{dataStorage: store}
	body := postScan(t, api, `{"start_height":157000,"end_height":158000}`)
	if body["message"] != "Repaired stored smart contracts" {
		t.Fatalf("message=%v", body["message"])
	}
	if body["repaired_blocks"].(float64) != 2 || body["repaired_contracts"].(float64) != 2 {
		t.Fatalf("repair counts: %+v", body)
	}
	for _, height := range []int64{157665, 157566} {
		raw, err := store.GetBlockData(height)
		if err != nil {
			t.Fatal(err)
		}
		cache := raw.(*storage.BlockDataCache)
		if blockContainsSmartContract(cache) {
			t.Fatalf("height %d still flagged: %+v", height, cache.SmartContracts)
		}
		ids := contractIDs(t, filepath.Join(bitcoin.BlockDir(root, height, "00000000"), "inscriptions.json"))
		if len(ids) != 0 {
			t.Fatalf("height %d file contracts=%v", height, ids)
		}
	}
}

func TestScanDropsAnchorWithoutWishImage(t *testing.T) {
	root := t.TempDir()
	t.Setenv("BLOCKS_DIR", root)
	t.Setenv("UPLOADS_DIR", t.TempDir())
	path := writeInscriptions(t, root, 157665, []bitcoin.SmartContractData{realContract(157665)})
	ds := storage.NewDataStorage(root)
	api := &DataAPI{dataStorage: ds}

	body := postScan(t, api, `{"block_height":157665}`)
	if body["repaired_contracts"].(float64) != 1 || body["repaired_blocks"].(float64) != 1 {
		t.Fatalf("repair counts: %+v", body)
	}
	if ids := contractIDs(t, path); len(ids) != 0 {
		t.Fatalf("file contracts=%v", ids)
	}
	raw, err := ds.GetBlockData(157665)
	if err != nil {
		t.Fatal(err)
	}
	cache := raw.(*storage.BlockDataCache)
	if blockContainsSmartContract(cache) {
		t.Fatalf("anchor without a wish image still flagged: %+v", cache.SmartContracts)
	}
}

func TestScanClearsEmptyPaletteHit(t *testing.T) {
	root := t.TempDir()
	t.Setenv("BLOCKS_DIR", root)
	height := int64(157526)
	dir := bitcoin.BlockDir(root, height, "00000000")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	doc := map[string]any{
		"block_hash":         "00000000000000008a64db1c6c4e255245a195289d1aa0b00cf56133eab47ca8",
		"block_height":       height,
		"total_transactions": 830,
		"smart_contracts": []map[string]any{{
			"contract_id":  "stego_0_1792956913",
			"block_height": height,
			"image_path":   "83e87417b73b8c1d_in0_w1_i0.png",
			"confidence":   51.29,
			"metadata": map[string]any{
				"extracted_message": "",
				"stego_type":        "palette",
				"tx_id":             "83e87417b73b8c1d4cab6c6e7a00a258a0da6930f8507c8c177e579241d7604f",
			},
		}},
		"steganography_scan": map[string]any{
			"total_images":   5,
			"stego_detected": true,
			"stego_count":    1,
			"stego_types":    []string{"palette"},
		},
		"success": true,
	}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "inscriptions.json")
	if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := storage.NewSQLiteDataStorage(filepath.Join(t.TempDir(), "blocks.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	if err := store.StoreBlockData(&bitcoin.BlockInscriptionsResponse{
		BlockHeight:       height,
		BlockHash:         doc["block_hash"].(string),
		TotalTransactions: 830,
		Success:           true,
		SmartContracts: []bitcoin.SmartContractData{{
			ContractID:  "stego_0_1792956913",
			BlockHeight: height,
			Metadata:    map[string]any{"extracted_message": "", "stego_type": "palette"},
		}},
	}, []map[string]interface{}{{
		"is_stego":          true,
		"stego_type":        "palette",
		"confidence":        51.29,
		"extracted_message": "",
	}}); err != nil {
		t.Fatal(err)
	}

	api := &DataAPI{dataStorage: store}
	body := postScan(t, api, `{"block_height":157526}`)
	if body["repaired_contracts"].(float64) != 1 {
		t.Fatalf("repair counts: %+v", body)
	}
	if body["cached"] != true {
		t.Fatalf("stored block should repair in place, cached=%v", body["cached"])
	}
	block := body["block_data"].(map[string]any)
	if contracts, ok := block["smart_contracts"].([]any); ok && len(contracts) != 0 {
		t.Fatalf("response contracts=%v", contracts)
	}
	summary := block["steganography_summary"].(map[string]any)
	if summary["stego_detected"] == true || summary["stego_count"].(float64) != 0 {
		t.Fatalf("stego summary still flags the block: %+v", summary)
	}
	disk, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var file map[string]any
	if err := json.Unmarshal(disk, &file); err != nil {
		t.Fatal(err)
	}
	scan := file["steganography_scan"].(map[string]any)
	if scan["stego_detected"] == true {
		t.Fatalf("file still flags stego: %+v", scan)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
