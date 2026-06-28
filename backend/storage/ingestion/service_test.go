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
						"embedded_message": "* test wish",
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

func TestSQLiteIngestionDSN(t *testing.T) {
	got := sqliteIngestionDSN("/tmp/ingest.db")
	want := "/tmp/ingest.db?_foreign_keys=on&_journal_mode=WAL&_busy_timeout=10000"
	if got != want {
		t.Fatalf("dsn mismatch:\n got: %s\nwant: %s", got, want)
	}
}