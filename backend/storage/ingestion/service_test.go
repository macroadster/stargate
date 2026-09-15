package ingestion

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
)

func TestSQLiteConcurrentCreates(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "ingestion.db")
	svc, err := NewIngestionService(dbPath)
	if err != nil {
		t.Fatalf("NewIngestionService: %v", err)
	}

	const workers = 8
	const perWorker = 5
	var wg sync.WaitGroup
	errCh := make(chan error, workers*perWorker)

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				id := fmt.Sprintf("wish-%d-%d", worker, i)
				rec := IngestionRecord{
					ID:            id,
					Filename:      id + ".png",
					Method:        "alpha",
					MessageLength: 12,
					ImageBase64:   "ZmFrZQ==",
					Metadata: map[string]interface{}{
						"embedded_message":   "* test wish",
						"visible_pixel_hash": id,
					},
					Status: "pending",
				}
				if err := svc.Create(rec); err != nil {
					errCh <- fmt.Errorf("create %s: %w", id, err)
				}
			}
		}(w)
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}
}

func TestListRecentMetaOmitsImageBase64(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "ingestion-meta.db")
	svc, err := NewIngestionService(dbPath)
	if err != nil {
		t.Fatalf("NewIngestionService: %v", err)
	}
	blob := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
	rec := IngestionRecord{
		ID:            "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899",
		Filename:      "wish.png",
		Method:        "alpha",
		MessageLength: 4,
		ImageBase64:   blob,
		Metadata:      map[string]interface{}{"visible_pixel_hash": "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899"},
		Status:        "pending",
	}
	if err := svc.Create(rec); err != nil {
		t.Fatalf("create: %v", err)
	}

	full, err := svc.ListRecent("", 10)
	if err != nil {
		t.Fatalf("ListRecent: %v", err)
	}
	if len(full) != 1 || full[0].ImageBase64 != blob {
		t.Fatalf("ListRecent should keep image_base64, got %+v", full)
	}

	meta, err := svc.ListRecentMeta("", 10)
	if err != nil {
		t.Fatalf("ListRecentMeta: %v", err)
	}
	if len(meta) != 1 {
		t.Fatalf("ListRecentMeta count=%d", len(meta))
	}
	if meta[0].ImageBase64 != "" {
		t.Fatalf("ListRecentMeta must not load image_base64, got %d bytes", len(meta[0].ImageBase64))
	}
	if meta[0].ID != rec.ID {
		t.Fatalf("id=%s", meta[0].ID)
	}
	if got, _ := meta[0].Metadata["visible_pixel_hash"].(string); got != rec.ID {
		t.Fatalf("metadata missing, got %v", meta[0].Metadata)
	}
}

func TestSQLiteIngestionDSN(t *testing.T) {
	got := sqliteIngestionDSN("/tmp/ingest.db")
	want := "/tmp/ingest.db?_foreign_keys=on&_journal_mode=WAL&_busy_timeout=10000"
	if got != want {
		t.Fatalf("dsn mismatch:\n got: %s\nwant: %s", got, want)
	}
}
