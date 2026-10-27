package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"stargate-backend/bitcoin"
	"stargate-backend/storage"
)

func TestRememberHeightInsertsWithoutWiping(t *testing.T) {
	api := &DataAPI{
		heightsCache:    []int64{152000, 151999, 145900},
		heightsCacheTTL: 15 * time.Second,
	}
	api.rememberHeight(145901)
	api.rememberHeight(152464)
	api.rememberHeight(152000) // dup
	want := []int64{152464, 152000, 151999, 145901, 145900}
	if len(api.heightsCache) != len(want) {
		t.Fatalf("len=%d want %d: %v", len(api.heightsCache), len(want), api.heightsCache)
	}
	for i := range want {
		if api.heightsCache[i] != want[i] {
			t.Fatalf("cache=%v want %v", api.heightsCache, want)
		}
	}
}

func TestRememberHeightEmptyCacheStaysEmpty(t *testing.T) {
	api := &DataAPI{}
	api.rememberHeight(100)
	if len(api.heightsCache) != 0 {
		t.Fatalf("empty cache should stay empty so the next walk rebuilds, got %v", api.heightsCache)
	}
}

// Verify that text inscriptions include inline content even when requesting fields=summary.
func TestHandleGetBlockInscriptionsPaginated_TextContentIncluded(t *testing.T) {
	mock := &mockDataStorage{
		block: &storage.BlockDataCache{
			BlockHeight: 123,
			BlockHash:   "abc",
			Inscriptions: []bitcoin.InscriptionData{
				{
					TxID:        "tx123",
					InputIndex:  0,
					ContentType: "text/plain",
					Content:     "hello world",
					SizeBytes:   11,
					FileName:    "note.txt",
					FilePath:    "note.txt",
				},
			},
			Images:         []bitcoin.ExtractedImageData{},
			SmartContracts: []bitcoin.SmartContractData{},
			ScanResults:    []map[string]interface{}{},
			Success:        true,
		},
	}

	api := &DataAPI{dataStorage: mock}

	req := httptest.NewRequest(http.MethodGet, "/api/data/block-inscriptions/123?fields=summary", nil)
	w := httptest.NewRecorder()

	api.HandleGetBlockInscriptionsPaginated(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", res.StatusCode)
	}

	var body struct {
		Inscriptions []map[string]interface{} `json:"inscriptions"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(body.Inscriptions) != 1 {
		t.Fatalf("expected 1 inscription, got %d", len(body.Inscriptions))
	}

	ins := body.Inscriptions[0]
	if ins["content_type"] != "text/plain" {
		t.Fatalf("expected content_type text/plain, got %v", ins["content_type"])
	}

	content, ok := ins["content"].(string)
	if !ok || content == "" {
		t.Fatalf("expected inline text content, got %v", ins["content"])
	}
	if content != "hello world" {
		t.Fatalf("unexpected content: %s", content)
	}
}

func TestHandleGetBlockInscriptionsPaginated_ContractFilter(t *testing.T) {
	mock := &mockDataStorage{
		block: &storage.BlockDataCache{
			BlockHeight: 456,
			BlockHash:   "def",
			Inscriptions: []bitcoin.InscriptionData{
				{
					TxID:        "imgtx",
					InputIndex:  0,
					ContentType: "image/png",
					FileName:    "random.png",
					FilePath:    "random.png",
				},
				{
					TxID:        "txtx",
					InputIndex:  0,
					ContentType: "text/plain",
					Content:     "brc-20",
					FileName:    "note.txt",
					FilePath:    "note.txt",
				},
			},
			Images: []bitcoin.ExtractedImageData{},
			SmartContracts: []bitcoin.SmartContractData{
				{
					ContractID:  "wish-contract-1",
					BlockHeight: 456,
					ImagePath:   "images/stego.png",
					Metadata: map[string]any{
						"tx_id":      "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
						"image_file": "stego.png",
						"is_stego":   true,
					},
				},
			},
			ScanResults: []map[string]interface{}{},
			Success:     true,
		},
	}

	api := &DataAPI{dataStorage: mock}

	req := httptest.NewRequest(http.MethodGet, "/api/data/block-inscriptions/456?fields=summary&filter=contract", nil)
	w := httptest.NewRecorder()
	api.HandleGetBlockInscriptionsPaginated(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", res.StatusCode)
	}

	var body struct {
		Inscriptions []map[string]interface{} `json:"inscriptions"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(body.Inscriptions) != 1 {
		t.Fatalf("expected 1 contract inscription, got %d", len(body.Inscriptions))
	}
	ins := body.Inscriptions[0]
	imageURL, _ := ins["image_url"].(string)
	if imageURL == "" || !strings.Contains(imageURL, "/block-image/") {
		t.Fatalf("expected block-image URL, got %v", ins["image_url"])
	}
	meta, _ := ins["metadata"].(map[string]interface{})
	if meta == nil || meta["contract_id"] != "wish-contract-1" {
		t.Fatalf("expected contract_id in metadata, got %v", ins["metadata"])
	}
}

func TestPickBlockCardThumbnail_MarksContractCover(t *testing.T) {
	block := &storage.BlockDataCache{
		BlockHeight: 456,
		Inscriptions: []bitcoin.InscriptionData{
			{
				TxID:        "imgtx",
				InputIndex:  0,
				ContentType: "image/png",
				FileName:    "random.png",
			},
		},
		SmartContracts: []bitcoin.SmartContractData{
			{
				ContractID: "wish-contract-1",
				ImagePath:  "images/stego.png",
				Metadata: map[string]any{
					"tx_id":      "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
					"image_file": "stego.png",
					"is_stego":   true,
				},
			},
		},
	}
	url, isContract := pickBlockCardThumbnail(block)
	if !isContract {
		t.Fatalf("expected contract thumbnail, url=%q", url)
	}
	if url != "/api/block-image/456/stego.png" {
		t.Fatalf("unexpected contract thumbnail url %q", url)
	}
}

func TestPickBlockCardThumbnail_RegularImageIsNotContract(t *testing.T) {
	block := &storage.BlockDataCache{
		BlockHeight: 152155,
		Inscriptions: []bitcoin.InscriptionData{
			{
				TxID:        "8d6dcac8cce141dc592a8d9e4a18ad5c8ca7b7a06f24cb68c308c63ffe2557b2",
				InputIndex:  0,
				ContentType: "image/jpeg",
				FileName:    "spam.jpg",
			},
		},
	}
	url, isContract := pickBlockCardThumbnail(block)
	if isContract {
		t.Fatalf("regular inscription thumbnail must not be marked contract, url=%q", url)
	}
	if url != "/content/8d6dcac8cce141dc592a8d9e4a18ad5c8ca7b7a06f24cb68c308c63ffe2557b2?witness=0" {
		t.Fatalf("unexpected inscription thumbnail url %q", url)
	}
}

func TestPickBlockCardThumbnail_SyntheticStegoIgnored(t *testing.T) {
	block := &storage.BlockDataCache{
		BlockHeight: 99,
		SmartContracts: []bitcoin.SmartContractData{
			{
				ContractID: "stego_0_1",
				ImagePath:  "images/false-positive.png",
				Metadata:   map[string]any{"image_file": "false-positive.png"},
			},
		},
	}
	url, isContract := pickBlockCardThumbnail(block)
	if url != "" || isContract {
		t.Fatalf("synthetic stego_ contracts must not become card thumbnails, url=%q isContract=%v", url, isContract)
	}
}

// --- mocks ---

type mockDataStorage struct {
	block  *storage.BlockDataCache
	blocks map[int64]*storage.BlockDataCache
}

// bitcoin.DataStorageInterface methods
func (m *mockDataStorage) StoreBlockData(*bitcoin.BlockInscriptionsResponse, []map[string]interface{}) error {
	return nil
}

func (m *mockDataStorage) GetBlockData(height int64) (interface{}, error) {
	if m.blocks != nil {
		if b, ok := m.blocks[height]; ok && b != nil {
			return b, nil
		}
		return nil, fmt.Errorf("not found")
	}
	if m.block != nil && m.block.BlockHeight == height {
		return m.block, nil
	}
	return nil, fmt.Errorf("not found")
}

func (m *mockDataStorage) GetRecentBlocks(int) ([]interface{}, error) {
	return nil, fmt.Errorf("not found")
}
func (m *mockDataStorage) GetSteganographyStats() map[string]interface{} {
	return map[string]interface{}{}
}
func (m *mockDataStorage) ValidateDataIntegrity(int64) error { return nil }
func (m *mockDataStorage) GetMaxBlockHeight() (int64, error) { return 0, nil }

// ExtendedDataStorage methods
func (m *mockDataStorage) CreateRealtimeUpdate(string, int64, interface{}) *storage.RealtimeUpdate {
	return nil
}

func (m *mockDataStorage) ReadTextContent(int64, string) (string, error) {
	return "", fmt.Errorf("not found")
}

func TestBlockContainsSmartContract(t *testing.T) {
	real := &storage.BlockDataCache{
		SmartContracts: []bitcoin.SmartContractData{{
			ContractID:  "wish-contract-1",
			BlockHeight: 40,
			ImagePath:   "images/stego.png",
		}},
	}
	if !blockContainsSmartContract(real) {
		t.Fatal("real contract should count")
	}

	synthetic := &storage.BlockDataCache{
		SmartContracts: []bitcoin.SmartContractData{{
			ContractID:  "stego_0_1",
			BlockHeight: 20,
		}},
	}
	if blockContainsSmartContract(synthetic) {
		t.Fatal("synthetic stego placeholder is not a smart contract")
	}

	stego := &storage.BlockDataCache{
		SteganographySummary: &bitcoin.SteganographySummary{StegoDetected: true, StegoCount: 2},
	}
	if blockContainsSmartContract(stego) {
		t.Fatal("a classifier hit with no extracted message is not a smart contract")
	}
	extracted := &storage.BlockDataCache{
		ScanResults: []map[string]interface{}{
			{"is_stego": true, "extracted_message": `{"v":2}`},
		},
		SteganographySummary: &bitcoin.SteganographySummary{StegoDetected: true, StegoCount: 1},
	}
	if !blockContainsSmartContract(extracted) {
		t.Fatal("an extracted stego payload should count as a contract block")
	}

	clean := &storage.BlockDataCache{TxCount: 12}
	if blockContainsSmartContract(clean) || blockContainsSmartContract(nil) {
		t.Fatal("clean and nil blocks should be hidden")
	}
}

func TestLoadBlockFromDiskIncludesStegoScan(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BLOCKS_DIR", dir)
	blockDir := filepath.Join(dir, "100_abcdef01")
	if err := os.MkdirAll(blockDir, 0o755); err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{
		"block_height": 100,
		"block_hash": "abc",
		"tx_count": 3,
		"smart_contracts": [{"contract_id": "stego_1_2", "block_height": 100}],
		"steganography_scan": {"total_images": 1, "stego_detected": true, "stego_count": 1, "scan_timestamp": 10}
	}`)
	if err := os.WriteFile(filepath.Join(blockDir, "inscriptions.json"), payload, 0o644); err != nil {
		t.Fatal(err)
	}

	block, err := (&DataAPI{}).loadBlockFromDisk(100)
	if err != nil {
		t.Fatal(err)
	}
	if block.SteganographySummary == nil || block.SteganographySummary.StegoCount != 1 {
		t.Fatalf("stego summary = %#v", block.SteganographySummary)
	}
	if blockContainsSmartContract(block) {
		t.Fatal("synthetic stego row with no extracted message is not a smart contract")
	}
}

func TestHandleGetBlockSummariesContractsOnly(t *testing.T) {
	prevCap := contractsOnlyMaxScan
	contractsOnlyMaxScan = 2
	t.Cleanup(func() { contractsOnlyMaxScan = prevCap })

	clean := func(height int64) *storage.BlockDataCache {
		return &storage.BlockDataCache{BlockHeight: height, BlockHash: fmt.Sprintf("h%d", height), TxCount: 4}
	}
	blocks := map[int64]*storage.BlockDataCache{
		50: clean(50),
		40: {
			BlockHeight: 40,
			BlockHash:   "contract",
			SmartContracts: []bitcoin.SmartContractData{{
				ContractID:  "wish-contract-1",
				BlockHeight: 40,
				ImagePath:   "images/stego.png",
				Metadata:    map[string]any{"image_file": "stego.png"},
			}},
		},
		30: clean(30),
		20: {
			BlockHeight: 20,
			BlockHash:   "stego",
			ScanResults: []map[string]interface{}{
				{"is_stego": true, "extracted_message": `{"v":2}`},
			},
			SteganographySummary: &bitcoin.SteganographySummary{
				StegoDetected: true,
				StegoCount:    1,
			},
		},
		10: clean(10),
	}
	api := &DataAPI{
		dataStorage:      &mockDataStorage{blocks: blocks},
		heightsCache:     []int64{50, 40, 30, 20, 10},
		heightsCacheTime: time.Now(),
		heightsCacheTTL:  time.Minute,
	}

	first := getBlockSummaries(t, api, "/api/data/block-summaries?contracts_only=1&limit=1")
	if got := summaryHeights(t, first); len(got) != 1 || got[0] != 40 {
		t.Fatalf("first page = %v, want [40]", got)
	}
	if first["has_more"] != true || first["next_cursor"] != "40" {
		t.Fatalf("first cursor = %v has_more=%v", first["next_cursor"], first["has_more"])
	}
	if firstBlock(t, first)["has_smart_contract"] != true {
		t.Fatalf("expected has_smart_contract, got %#v", firstBlock(t, first))
	}

	rest := getBlockSummaries(t, api, "/api/data/block-summaries?contracts_only=1&limit=10&cursor_height=40")
	if got := summaryHeights(t, rest); len(got) != 1 || got[0] != 20 {
		t.Fatalf("second page = %v, want [20]", got)
	}
	// Scan cap stops on the stego block, before the last clean height.
	if rest["has_more"] != true || rest["next_cursor"] != "20" {
		t.Fatalf("second cursor = %v has_more=%v", rest["next_cursor"], rest["has_more"])
	}

	tail := getBlockSummaries(t, api, "/api/data/block-summaries?contracts_only=1&limit=10&cursor_height=20")
	if got := summaryHeights(t, tail); len(got) != 0 {
		t.Fatalf("tail = %v, want none", got)
	}
	if tail["has_more"] != false {
		t.Fatalf("expected end of history, got %#v", tail["has_more"])
	}

	all := getBlockSummaries(t, api, "/api/data/block-summaries?limit=2")
	if got := summaryHeights(t, all); len(got) != 2 || got[0] != 50 || got[1] != 40 {
		t.Fatalf("unfiltered page = %v, want [50 40]", got)
	}
	if all["has_more"] != true || all["next_cursor"] != "40" {
		t.Fatalf("unfiltered cursor = %v has_more=%v", all["next_cursor"], all["has_more"])
	}
}

func TestHandleGetBlockSummariesContractsOnlyAdvancesPastCleanGap(t *testing.T) {
	prevCap := contractsOnlyMaxScan
	contractsOnlyMaxScan = 2
	t.Cleanup(func() { contractsOnlyMaxScan = prevCap })

	blocks := map[int64]*storage.BlockDataCache{}
	for _, h := range []int64{5, 4, 3, 2, 1} {
		blocks[h] = &storage.BlockDataCache{BlockHeight: h, BlockHash: fmt.Sprintf("c%d", h), TxCount: 1}
	}
	api := &DataAPI{
		dataStorage:      &mockDataStorage{blocks: blocks},
		heightsCache:     []int64{5, 4, 3, 2, 1},
		heightsCacheTime: time.Now(),
		heightsCacheTTL:  time.Minute,
	}

	page := getBlockSummaries(t, api, "/api/data/block-summaries?contracts_only=true&limit=10")
	if got := summaryHeights(t, page); len(got) != 0 {
		t.Fatalf("clean page = %v", got)
	}
	if page["has_more"] != true || page["next_cursor"] != "4" {
		t.Fatalf("cursor = %v has_more=%v, want 4/true", page["next_cursor"], page["has_more"])
	}
}

func getBlockSummaries(t *testing.T, api *DataAPI, path string) map[string]interface{} {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	api.HandleGetBlockSummaries(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("%s: status %d body %s", path, w.Code, w.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return body
}

func summaryHeights(t *testing.T, body map[string]interface{}) []int64 {
	t.Helper()
	raw, _ := body["blocks"].([]interface{})
	out := make([]int64, 0, len(raw))
	for _, item := range raw {
		block, _ := item.(map[string]interface{})
		height, _ := block["block_height"].(float64)
		out = append(out, int64(height))
	}
	return out
}

func firstBlock(t *testing.T, body map[string]interface{}) map[string]interface{} {
	t.Helper()
	raw, _ := body["blocks"].([]interface{})
	if len(raw) == 0 {
		t.Fatal("no blocks")
	}
	block, _ := raw[0].(map[string]interface{})
	return block
}
