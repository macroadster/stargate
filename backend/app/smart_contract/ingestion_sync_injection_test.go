package smart_contract

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"stargate-backend/services"
	scstore "stargate-backend/storage/smart_contract"
)

// t04: StartIngestionSync used to take a DSN and build its own IngestionService,
// a third handle after the two stargate-a49 consolidated. Its DSN derivation read
// STARGATE_PG_DSN but never DATABASE_URL and never the configured storage type,
// so with DATABASE_URL set and STARGATE_PG_DSN unset the process ran AllStores on
// Postgres while this loop opened sqlite and polled an empty database.
//
// This asserts by behaviour rather than by identity: a record written through the
// passed service has to be the one the loop acts on. A loop that built its own
// service would see an empty database and leave the record alone, which is
// exactly the production symptom.
func TestIngestionSyncActsOnThePassedService(t *testing.T) {
	dir := t.TempDir()
	ingest, err := services.NewIngestionService(filepath.Join(dir, "ingestions.db"))
	if err != nil {
		t.Fatalf("ingestion service: %v", err)
	}

	// No ImageBase64 and no ipfs_image_cid, so processRecord takes its earliest
	// decision and marks the record "ignored". That transition is the signal that
	// the loop read this service.
	rec := services.IngestionRecord{
		ID:       "rec-t04",
		Filename: "wish.png",
		Method:   "lsb",
		Status:   "pending",
	}
	if err := ingest.Create(rec); err != nil {
		t.Fatalf("seed record: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := StartIngestionSync(ctx, ingest, scstore.NewMemoryStore(time.Hour), 10*time.Millisecond); err != nil {
		t.Fatalf("StartIngestionSync: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		got, err := ingest.Get("rec-t04")
		if err != nil {
			t.Fatalf("read back record: %v", err)
		}
		if got.Status != "pending" {
			if got.Status != "ignored" {
				t.Fatalf("record moved to %q, expected \"ignored\"", got.Status)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("record was never touched: the sync loop is not reading the service it was given")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestIngestionSyncRefusesNilService pins that an unconfigured ingestion service
// is refused rather than papered over. Taking a DSN meant this could not be
// expressed: any string produced some service, which is how the loop ended up
// pointed at a database nobody was writing to.
func TestIngestionSyncRefusesNilService(t *testing.T) {
	err := StartIngestionSync(context.Background(), nil, scstore.NewMemoryStore(time.Hour), time.Second)
	if err == nil {
		t.Fatal("nil ingestion service was accepted; the loop would tick against nothing")
	}
}
