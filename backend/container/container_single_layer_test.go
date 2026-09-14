package container

import (
	"path/filepath"
	"testing"

	"stargate-backend/storage"
)

// a49: NewContainer used to construct its own ingestion service, data storage and
// contract cache from the environment, giving the process two independent data
// layers over the same data. The block monitor then read one and the IPFS ingest
// sync read the other.
//
// These assert identity rather than equivalence. Anything rebuilt internally
// would still be a working object of the right type, which is exactly why the
// duplication survived so long, so the only useful question is whether the
// container is holding the same object NewAllStores made.
func TestContainerSharesTheOneDataLayer(t *testing.T) {
	dir := t.TempDir()
	cfg := storage.StorageConfig{
		Type:             storage.StorageSQLite,
		DataDir:          dir,
		SQLiteDir:        filepath.Join(dir, "sqlite"),
		MCPDBPath:        filepath.Join(dir, "sqlite", "mcp.db"),
		APIKeysDBPath:    filepath.Join(dir, "sqlite", "api_keys.db"),
		IngestionsDBPath: filepath.Join(dir, "sqlite", "ingestions.db"),
	}

	stores, err := storage.NewAllStores(cfg)
	if err != nil {
		t.Fatalf("NewAllStores: %v", err)
	}

	c := NewContainer(stores)

	if c.IngestionService != stores.IngestionService {
		t.Error("container holds a different ingestion service than AllStores built; " +
			"the block monitor and the IPFS ingest sync would be reading separate handles")
	}
	if c.DataStorage != stores.DataStorage {
		t.Error("container holds a different data storage than AllStores built")
	}
	// AllStores' cache was allocated and never read, because the container made
	// its own and the handlers took that one.
	if c.ContractCache != stores.ContractCache {
		t.Error("container holds a different contract cache than AllStores built")
	}
}

// TestContainerIgnoresStorageEnvironment pins that construction is driven by the
// passed config alone. The container preferred the Postgres DSN for ingestion
// whenever the environment carried one, even with the configured type set to
// sqlite, so these two variables alone used to send the two layers at different
// databases.
func TestContainerIgnoresStorageEnvironment(t *testing.T) {
	t.Setenv("STARGATE_STORAGE", "postgres")
	t.Setenv("DATABASE_URL", "postgres://nobody@127.0.0.1:1/nowhere")
	t.Setenv("STARGATE_PG_DSN", "postgres://nobody@127.0.0.1:1/nowhere")

	dir := t.TempDir()
	cfg := storage.StorageConfig{
		Type:             storage.StorageSQLite,
		DataDir:          dir,
		SQLiteDir:        filepath.Join(dir, "sqlite"),
		MCPDBPath:        filepath.Join(dir, "sqlite", "mcp.db"),
		APIKeysDBPath:    filepath.Join(dir, "sqlite", "api_keys.db"),
		IngestionsDBPath: filepath.Join(dir, "sqlite", "ingestions.db"),
	}

	stores, err := storage.NewAllStores(cfg)
	if err != nil {
		t.Fatalf("NewAllStores: %v", err)
	}

	// Previously this call would have spent ~15s retrying that unreachable
	// Postgres and then set IngestionService to nil, while AllStores' SQLite
	// service kept working.
	c := NewContainer(stores)

	if c.IngestionService == nil {
		t.Fatal("ingestion service is nil: construction still consults the environment")
	}
	if c.IngestionService != stores.IngestionService {
		t.Error("ingestion service was rebuilt from the environment rather than taken from AllStores")
	}
}
