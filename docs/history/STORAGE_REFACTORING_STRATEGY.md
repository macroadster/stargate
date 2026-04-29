# Stargate Backend Storage Refactoring Strategy

**Date**: 2026-04 (analysis performed by Grok 4.3)  
**Status**: Proposed  
**Related**: STARGATE_STORAGE env, backend/storage/, MCP + API Gateway + Middleware fragmentation  
**Goal**: Consolidate all storage initialization, selection, interfaces, and domain logic into the `backend/storage` package for consistency, maintainability, and single source of truth.

---

## Executive Summary

The Stargate backend supports multiple storage backends:
- **PostgreSQL** (shared, production-grade, used when `STARGATE_PG_DSN` / `DATABASE_URL` set)
- **SQLite** (embedded disk-persisted, for `mcp.db`, `api_keys.db`, `ingestions.db`)
- **Memory** (pure in-RAM, with fixture seeding, fallback)
- **Filesystem** (legacy for block data / `DataStorage`)

Selection is currently driven by inconsistent mechanisms:
- `STARGATE_STORAGE=postgres|sqlite|memory|filesystem` (only partially honored in `container/container.go` for data layer)
- Presence/absence of `STARGATE_PG_DSN` (primary toggle in `stargate_backend.go`)

Storage logic is **fragmented** across:
- `backend/storage/` (partial: data + auth + smart_contract impls)
- `backend/middleware/smart_contract/storage.go` (defines the main `Store` interface)
- `backend/stargate_backend.go` (heavy initialization + path consolidation)
- `backend/container/container.go` (data storage selection)
- `backend/services/ingestion_service.go` (PG-only ingestion persistence)
- Various sync modules in middleware that type-assert concrete store types

This leads to:
- Duplicated schema definitions (SQLite vs PG)
- Inconsistent backend choice between DataStorage vs MCP Store vs Auth
- Brittle type assertions (`store.(*scstore.PGStore)`)
- Main.go bloat
- Difficult to test or extend (e.g., add "sqlite for block data")

**Proposed Outcome**: Move *all* storage concerns (interfaces, factories, config, schema, selection heuristics) under `backend/storage/**`. `main`, handlers, middleware, and MCP become pure consumers.

---

## Current Architecture Analysis

### 1. Environment Variable Handling (Inconsistent)

| Location                        | Env Var(s) Used                          | Logic                                                                 | Scope                  |
|---------------------------------|------------------------------------------|-----------------------------------------------------------------------|------------------------|
| `stargate_backend.go:consolidateEnvironmentPaths()` | `STARGATE_DATA_DIR`, many `STARGATE_*_DB` | Sets defaults under `dataDir/sqlite/` for mcp/api_keys/ingestions    | All sqlite paths      |
| `stargate_backend.go:initializeMCPComponents()` | `STARGATE_PG_DSN`, `STARGATE_MCP_DB`, `STARGATE_API_KEYS_DB`, `STARGATE_SEED_FIXTURES`, TTLs | PG_DSN ? PGStore+PGKeys : try SQLiteStore+SQLiteKeys : Memory        | MCP Store + Auth Keys |
| `container/container.go:NewContainer()` | `STARGATE_STORAGE`, `STARGATE_PG_DSN`, `BLOCKS_DIR` | `storage=="postgres" && dsn` ? PostgresStorage : DataStorage(fs)     | Block/Data layer      |
| `run_dev.sh` / `Dockerfile`     | `STARGATE_STORAGE` (default "filesystem") | Only affects echo / image label                                       | Documentation only    |
| `services/ingestion_service.go` | Implicit via dsn passed from main       | Always PG (pgx driver)                                                | Ingestion             |

**Observation**: `STARGATE_STORAGE` is under-utilized and its documented values (`sqlite|postgres|memory`) do not match runtime reality (`filesystem` default + PG_DSN override). No single place interprets the env var for *all* layers.

### 2. Storage Interfaces & Implementations

**Core Smart Contract / MCP `Store` interface** (60+ methods: contracts, tasks, claims, proposals, submissions, escrow, rework, sync ops...):

- **Defined in**: `middleware/smart_contract/storage.go:23` (`type Store interface { ... }`)
- **Implemented in**:
  - `storage/smart_contract/memory_store.go` (`MemoryStore`)
  - `storage/smart_contract/sqlite_store.go` (`SQLiteStore`)
  - `storage/smart_contract/pg_store.go` (`PGStore`)
- **Used by**: `mcp/`, `middleware/smart_contract/*` (server, handlers, syncs), `handlers/`, `container.SetSmartContractHandler`

**Problems**:
- Application-layer package (`middleware/`) owns the persistence contract.
- No `storage/smart_contract/store.go` or `interfaces.go`.
- `scmiddleware.Store` alias in main creates indirection.

**Data Layer**:
- `storage.ExtendedDataStorage` + `bitcoin.DataStorageInterface` defined in `storage/data_storage.go`
- Impl: `DataStorage` (fs + json files + cache) and `PostgresStorage`

**Auth**:
- Interfaces in `storage/auth/apikey_store.go`: `APIKeyIssuer`, `APIKeyValidator`, etc.
- Impls: `APIKeyStore` (mem), `SQLiteAPIKeyStore`, `PGAPIKeyStore`
- `ChallengeStore` (mem only, TTL-based)

**Ingestion**:
- No dedicated interface; concrete `*services.IngestionService` (PG only) passed around.

### 3. Initialization & Factory Logic (Duplicated + Scattered)

**Primary hot spot**: `stargate_backend.go:initializeMCPComponents()` (~100 lines):
- Determines `actualStoreType`
- Instantiates MCP `Store`
- Instantiates matching API key store (PG/SQLite/Mem)
- Returns 5 values to `main` and `runHTTPServer`
- Has fallback logic and logging

**Secondary**: `container.NewContainer()`:
- Separate `storageType` decision for `DataStorage`
- Creates `IngestionService` if pgDSN (but different retry wrapper)

**Other**:
- `startMCPServices()` re-derives ingestDsn based on PG_DSN or `STARGATE_INGESTIONS_DB`
- `consolidateEnvironmentPaths()` mutates env for sqlite subpaths

Result: **Two independent decision trees** that can disagree (e.g., PG for MCP but filesystem data, or vice versa).

### 4. Schema & Persistence Details (Duplication)

- `storage/smart_contract/schema_manager.go`: PG-only schema for `mcp_*` tables (contracts, tasks, claims, submissions, proposals, escort_status, etc.)
- `sqlite_store.go:initSchema()`: Nearly identical `CREATE TABLE IF NOT EXISTS` statements (SQLite dialect)
- `postgres_storage.go` and `ingestion_service.go`: Own table patterns + `ensureSchema`
- No shared `storage/schema/` or migration system.

**Risk**: Drift between PG and SQLite schemas; SQLite lacks some PG features (arrays, GIN indexes) used in queries.

### 5. Cross-Layer Coupling & Brittle Patterns

- `middleware/smart_contract/ingestion_sync.go:62`:
  ```go
  pgStore, ok := store.(*scstore.PGStore)
  if !ok { return fmt.Errorf("ingestion sync requires PostgreSQL store") }
  ```
  Only works for PG; SQLite path uses separate `ingestDsn` file but sync may be limited.
- Similar assumptions in `ipfs_ingest_sync.go`, `funding_sync.go`, `stego_reconcile.go`.
- `blockMonitor.SetSweepDependencies(sweepStore, ...)` does `store.(bitcoin.SweepTaskStore)` interface assertion (in `stargate_backend.go:730`).
- `DataStorage` and `PostgresStorage` both implement `bitcoin.DataStorageInterface` but Postgres one is incomplete for some file ops.

### 6. "memory+disk" Hybrid Mode

- MemoryStore: RAM + optional seed fixtures. Lost on restart.
- SQLiteStore: "disk" persistence via single `mcp.db` file (plus separate dbs for keys/ingest).
- No true hybrid (e.g., MemoryStore with WAL snapshot, or LRU cache over SQLite).
- Data layer has no SQLite equivalent (only fs json or PG).

### 7. Package Dependency Graph (Current)

```
main (stargate_backend.go)
  ├── container
  │     └── storage (DataStorage / PostgresStorage)
  ├── middleware/smart_contract (defines Store interface + syncs + server)
  │     └── storage/smart_contract (impls)   <--- impls live "below" definer
  ├── mcp (uses scmiddleware.Store)
  ├── services (IngestionService)
  └── storage/auth (keys, challenges)
```

**Violation**: `middleware` (higher layer) defines what `storage` implements.

---

## Proposed Refactoring Strategy

### Guiding Principles

1. **Storage is a foundational package**: `backend/storage` (and subpackages) owns *everything* related to persistence.
2. **One decision point**: `STARGATE_STORAGE` + `STARGATE_PG_DSN` (and data dir) drive a single `StorageConfig` → all stores.
3. **Interfaces live with implementations**: Define `Store`, `DataStorage`, `IngestionStore` etc. inside `storage/*`.
4. **Factory over scattered New* calls**: Provide `storage.New*` or `storage.InitializeAll(...)` that returns a bag of ready-to-use stores.
5. **No type assertions in application code**: Sync logic, reconciliation, etc. must work through interfaces or explicit capability interfaces (e.g., `IngestionSyncer`).
6. **SQLite as first-class "embedded"**: Promote `sqlite` mode; `memory` is for tests/CI; `postgres` for HA.
7. **Schema as code**: Single source (or well-tested parallel sources) for table definitions. Consider lightweight migration runner.

### Target Package Layout (after refactor)

```
backend/storage/
├── factory.go                 # StorageConfig, LoadFromEnv(), NewStores()
├── config.go                  # struct StorageConfig { Type string; PGDSN string; DataDir string; ... }
├── data/
│   ├── interface.go           # ExtendedDataStorage, DataStorageInterface
│   ├── filesystem.go          # DataStorage (renamed?)
│   └── postgres.go            # PostgresStorage
├── auth/
│   ├── apikey_store.go        # interfaces + Memory impl
│   ├── apikey_store_sqlite.go
│   ├── apikey_store_pg.go
│   └── challenge_store.go
├── smart_contract/
│   ├── store.go               # MOVED: type Store interface { ... } + Err*
│   ├── memory_store.go
│   ├── sqlite_store.go
│   ├── pg_store.go
│   ├── schema.go              # unified or dialect-aware schema strings + Apply()
│   ├── proposals_repository.go
│   ├── tasks_repository.go
│   ├── contract_cache.go
│   └── ...
├── ingestion/
│   ├── service.go             # MOVED + generalized (support sqlite too?)
│   └── schema.go
├── schema_manager.go          # shared migration helper (for PG + SQLite)
└── ...
```

`middleware/smart_contract/storage.go` → deleted or becomes a 5-line re-export for compatibility during transition:
```go
// Deprecated: import "stargate-backend/storage/smart_contract" directly
type Store = smart_contract.Store
```

### Detailed Phases

#### Phase 0: Preparation (no behavior change)
- Audit all direct DB opens, `CREATE TABLE`, and `sql.DB` usage. (Already done via grep.)
- Add `go:generate` or tests that assert schema equivalence between PG/SQLite.
- Introduce `bd create "Storage refactoring: centralize into backend/storage" -t task -p 1`

#### Phase 1: Move Interface + Centralize Smart Contract Store
- Create `storage/smart_contract/store.go` containing the full `Store` interface + error vars (copy from middleware).
- Update `memory/sqlite/pg_store.go` (they already satisfy it implicitly).
- Change `middleware/smart_contract/storage.go` to:
  ```go
  import scstore "stargate-backend/storage/smart_contract"
  type Store = scstore.Store
  var (
      ErrTaskNotFound = scstore.ErrTaskNotFound
      ...
  )
  ```
  (or delete after updating all imports).
- Update all import sites (`mcp/`, `handlers/`, tests, `container/`) to prefer `storage/smart_contract`.
- **Verify**: `go build ./...` + all tests pass.

**Deliverable**: Interface definition now lives in `storage/`.

#### Phase 2: Unified Factory + Config (Core of Refactor)
- Introduce `storage/config.go`:
  ```go
  type StorageType string
  const (
      StorageMemory    StorageType = "memory"
      StorageSQLite    StorageType = "sqlite"
      StoragePostgres  StorageType = "postgres"
      StorageFilesystem StorageType = "filesystem" // legacy for data only
  )

  type StorageConfig struct {
      Type                StorageType
      PGDSN               string
      DataDir             string
      MCPDBPath           string
      APIKeysDBPath       string
      IngestionsDBPath    string
      ClaimTTL            time.Duration
      SeedFixtures        bool
      ContractCacheTTL    time.Duration
      ContractCacheSize   int
      // block data cache params etc.
  }

  func LoadStorageConfigFromEnv() StorageConfig {
      // central logic reading STARGATE_STORAGE, STARGATE_PG_DSN, STARGATE_DATA_DIR,
      // STARGATE_MCP_DB, STARGATE_*_DB, STARGATE_DEFAULT_CLAIM_TTL_HOURS, etc.
      // Falls back intelligently: PG_DSN present => postgres; else sqlite if dataDir writable else memory.
  }
  ```
- `storage/factory.go`:
  ```go
  type AllStores struct {
      DataStorage         ExtendedDataStorage
      SmartContractStore  smart_contract.Store
      APIKeyIssuer        auth.APIKeyIssuer
      APIKeyValidator     auth.APIKeyValidator
      ChallengeStore      *auth.ChallengeStore
      IngestionService    *IngestionService // or interface
      ContractCache       *smart_contract.ContractCache
      // ...
  }

  func NewAllStores(cfg StorageConfig) (*AllStores, error) {
      // single decision tree
      // for each domain:
      //   switch cfg.Type {
      //   case StoragePostgres: NewPGXXX(cfg.PGDSN)
      //   case StorageSQLite: NewSQLiteXXX(resolvedPath)
      //   case StorageMemory: NewMemoryXXX()
      //   }
      // Also calls consolidate paths internally or returns resolved paths.
      // Initializes schemas via schema_manager.
  }
  ```
- **Migrate** `initializeMCPComponents()` body into `NewAllStores`.
- **Migrate** data selection from `container.NewContainer()` — have it accept or call into factory, or pass pre-built stores.
- Update `main()`:
  ```go
  cfg := storage.LoadStorageConfigFromEnv()
  stores, err := storage.NewAllStores(cfg)
  // pass stores.SmartContractStore, stores.APIKeyIssuer, ... down
  container := container.NewContainerFromStores(stores, ...) // or adapt
  ```
- Remove or deprecate old `NewPGStore`, `NewSQLiteStore` etc. (keep for tests, or make unexported).

**Key win**: `STARGATE_STORAGE` now consistently controls MCP + Auth + Data + Ingestion.

#### Phase 3: Move IngestionService into storage/ingestion/
- Move `services/ingestion_service.go` → `storage/ingestion/service.go`
- Generalize constructor to accept driver or auto-detect from DSN/path (support file-based SQLite ingestion DB for non-PG mode).
- Update `NewAllStores` to create it consistently (for sqlite use the ingestions.db path).
- Update `middleware/smart_contract/ingestion_sync.go` to use interface methods instead of `*PGStore` cast. Add to `Store` interface:
  ```go
  // optional capability
  type IngestionAwareStore interface {
      Store
      RecordIngestion(...) error
      // or rely on existing UpsertTask / CreateProposal
  }
  ```
  Or keep sync in terms of high-level `Store` methods only.

#### Phase 4: Unify Schema Management
- Enhance `storage/smart_contract/schema.go` (or `schema_manager.go`) to export:
  - `GetSmartContractSchema(dialect string) string` // "postgres" | "sqlite"
  - `ApplySchema(ctx, db *sql.DB, dialect) error`
- Same for ingestion schema.
- In SQLite and PG store constructors, call the shared applier instead of inline `CREATE TABLE` strings.
- Add test that asserts PG and SQLite schemas produce compatible table structures (column names, types ignoring dialect differences).

#### Phase 5: Eliminate Type Assertions & Brittle Coupling
- Audit all `.(ConcreteType)` on stores:
  - `ingestion_sync.go` → replace with interface or store-provided `SyncFromIngestion(ctx, records)`
  - `stargate_backend.go:730` `.(bitcoin.SweepTaskStore)` → make `Store` embed or return `bitcoin.SweepTaskStore` capability.
  - `blockMonitor` interactions.
- For features that only make sense on PG (e.g., certain indexes, pubsub), add `SupportsFeature(Feature) bool` or separate optional interfaces.
- Move background sync starters (`StartIngestionSync`, `StartFundingSync`, `StartIPFSIngestionSync`) into the factory or `AllStores` (e.g., `stores.StartBackgroundServices(ctx)`).

#### Phase 6: Data Layer SQLite Support + "memory+disk" Clarification (Optional but Recommended)
- Implement `SQLiteDataStorage` in `storage/data/sqlite.go` (similar to PostgresStorage but using `modernc.org/sqlite` or `mattn/go-sqlite3`).
- Update factory: when `Type=="sqlite"`, allow choosing SQLiteDataStorage for blocks too (or keep fs for images/binaries + sqlite for metadata).
- Document "memory+disk":
  - `memory`: ephemeral (good for tests)
  - `sqlite`: durable embedded (default for single-binary)
  - `postgres`: durable shared
  - `filesystem`: legacy block json mode (deprecated)

#### Phase 7: Cleanup, Documentation, Deployment
- Delete old duplicated code in `stargate_backend.go` (initialize fn shrinks dramatically).
- Update `container/container.go` to be thinner (receive stores from caller).
- Update `Dockerfile`, `run_dev.sh`, `.env.example` (if exists), docs to use `STARGATE_STORAGE=sqlite` as sensible default for local/dev.
- Update `README.md`, `backend/docs/`, architecture docs.
- Add integration test: start with each STORAGE type, assert correct concrete types are used and data roundtrips.
- `bd close` the tracking issue.
- Commit including `.beads/issues.jsonl`.

---

## Benefits

- **Consistency**: One env var + one factory decides storage for the entire backend.
- **Testability**: `storage.NewAllStores(StorageConfig{Type: "memory"})` gives fully wired in-mem system for unit tests.
- **Maintainability**: Schema in one place; adding a new backend (e.g. "mysql", "badger") touches only `storage/`.
- **Correctness**: No more PG_MCP + FS_data mismatches; no silent fallbacks that leave data on different backends.
- **Layering**: `storage` is leaf package. Middleware, MCP, handlers, bitcoin layer all import it cleanly.
- **Single Binary UX**: `STARGATE_STORAGE=sqlite` (or default) "just works" with zero external DB for demos / edge deployments.

---

## Risks & Mitigations

| Risk                              | Impact | Mitigation |
|-----------------------------------|--------|------------|
| Large surface (touches main, container, middleware, mcp, services, tests) | High | Incremental phases; keep backward-compatible aliases during transition; extensive `go test ./...` after each phase |
| Breaking changes to internal packages | Med | Use `go list -f '{{.Imports}}'` + replace in PR; update all test helpers |
| SQLite vs PG query differences (e.g. `[]string` arrays, JSON ops) | Med | Keep dialect-specific query paths behind interface methods in repositories; test both |
| Performance regression from indirection | Low | Factory is init-time only; hot path uses concrete impls or direct interface calls |
| Existing deployments using `STARGATE_STORAGE=filesystem` or implicit PG_DSN | Med | Factory treats "filesystem" as alias for data layer; presence of PG_DSN forces postgres type |
| Circular import after moving interface | Low | `storage` imports nothing from `middleware` or `mcp`; they import `storage` |

---

## Success Criteria (Definition of Done)

- `STARGATE_STORAGE` (or derived `StorageConfig.Type`) is the single source of truth.
- All `New*Store` calls for production paths originate from `storage/factory.go`.
- `middleware/smart_contract/storage.go` no longer contains the authoritative interface definition.
- `go test ./...` + manual smoke with `memory`, `sqlite` (file), `postgres` (if available) all pass.
- No `.(*ConcreteStore)` casts remain in `middleware/`, `mcp/`, `handlers/`, or `main`.
- New developer docs: "Storage Backends" section in `backend/docs/` or root `README`.
- `docs/history/STORAGE_REFACTORING_STRATEGY.md` (this file) is referenced in commit.

---

## Example Usage After Refactor (Target)

```go
// main.go
func main() {
    cfg := storage.LoadStorageConfigFromEnv()
    all, err := storage.NewAllStores(cfg)
    if err != nil { log.Fatal(err) }
    defer all.SmartContractStore.Close()

    // start syncs etc via all.StartSyncServices(...) or individually

    httpServer := NewHTTPServer(all, ...)
    ...
}

// container.go (adapted)
func NewContainerFromStores(s *storage.AllStores, ...) *Container {
    // uses s.DataStorage, s.SmartContractStore (via scmiddleware.Store = storage...), s.APIKey* etc.
}
```

---

## Open Questions for Implementation

1. Should `IngestionService` be generalized to support SQLite ingestion DB (for fully embedded mode), or keep ingestion always PG-only?
2. For DataStorage in sqlite mode: store full block JSON in sqlite `block_scans` table (like PG) or keep filesystem for large images + sqlite metadata?
3. Migration path for existing `smart_contracts.json` + `inscriptions.json` files when switching modes?
4. Do we want a `storage.HealthChecker` interface that each backend implements for `/api/health`?

---

## References to Key Files (for implementers)

- Current decision logic: `stargate_backend.go:233-333` (`initializeMCPComponents`)
- Data selection: `container/container.go:98-107`
- Interface: `middleware/smart_contract/storage.go:23-60`
- Schemas: `storage/smart_contract/{pg_store.go,sqlite_store.go,schema_manager.go}`
- Path logic: `stargate_backend.go:403-436`
- Type assertions: `middleware/smart_contract/ingestion_sync.go:62`, `stargate_backend.go:730`

**Next Step Recommendation**: Create tracking issue via `bd create "Refactor storage into centralized backend/storage factory" -t epic -p 1`, then implement Phase 1.

This strategy ensures the storage package becomes the single, coherent home for all persistence concerns as intended by the package structure.
