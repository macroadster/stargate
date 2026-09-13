package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"stargate-backend/bitcoin"
	"stargate-backend/storage"
)

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
