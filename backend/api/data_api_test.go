package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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

// ExtendedDataStorage methods
func (m *mockDataStorage) CreateRealtimeUpdate(string, int64, interface{}) *storage.RealtimeUpdate {
	return nil
}

func (m *mockDataStorage) ReadTextContent(int64, string) (string, error) {
	return "", fmt.Errorf("not found")
}
