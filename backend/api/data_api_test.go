package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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
	block *storage.BlockDataCache
}

// bitcoin.DataStorageInterface methods
func (m *mockDataStorage) StoreBlockData(*bitcoin.BlockInscriptionsResponse, []map[string]interface{}) error {
	return nil
}

func (m *mockDataStorage) GetBlockData(height int64) (interface{}, error) {
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
