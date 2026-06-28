package storage

import (
	"context"
	"log"
	"path/filepath"
	"time"

	"stargate-backend/services"
	"stargate-backend/storage/auth"
	scstore "stargate-backend/storage/smart_contract"
)

// AllStores is the unified bag of all persistence-related components.
// It is produced by NewAllStores from a single StorageConfig so that
// STARGATE_STORAGE (and related env vars) controls the entire backend
// consistently (data layer, MCP/smart-contract store, API keys, ingestion, caches).
type AllStores struct {
	// Data / block layer
	DataStorage ExtendedDataStorage

	// MCP / Smart Contract persistence (the big Store interface)
	SmartContractStore scstore.Store

	// Auth
	APIKeyIssuer   auth.APIKeyIssuer
	APIKeyValidator auth.APIKeyValidator
	ChallengeStore *auth.ChallengeStore

	// IngestionService (SQLite or Postgres DSN — both supported)
	IngestionService *services.IngestionService

	// Caches
	ContractCache *scstore.ContractCache
}

// NewAllStores creates every storage backend according to the supplied
// config. This is the single function that should eventually replace the
// duplicated initialization logic in stargate_backend.go and container.go.
//
// It is intentionally additive in Phase 2 — callers are not yet migrated.
func NewAllStores(cfg StorageConfig) (*AllStores, error) {
	all := &AllStores{}

	// 1. Contract cache (always in-memory, configured from env)
	all.ContractCache = scstore.NewContractCache(cfg.ContractCacheTTL, cfg.ContractCacheSize)

	// 2. Challenge store (always memory + TTL)
	all.ChallengeStore = auth.NewChallengeStore(10 * time.Minute)

	// 3. Data storage layer (block monitoring, images, etc.)
	switch cfg.Type {
	case StoragePostgres:
		if cfg.PGDSN != "" {
			if pg, err := NewPostgresStorage(cfg.PGDSN); err != nil {
				log.Printf("Postgres data storage failed, falling back to filesystem: %v", err)
				all.DataStorage = NewDataStorage(cfg.DataDir)
			} else {
				log.Printf("Using Postgres data storage backend")
				all.DataStorage = pg
			}
		} else {
			all.DataStorage = NewDataStorage(cfg.DataDir)
		}
	case StorageFilesystem:
		all.DataStorage = NewDataStorage(cfg.DataDir)
	case StorageSQLite:
		// Use SQLite for block metadata (block_scans table) — consistent durable
		// embedded storage together with mcp.db and api_keys.db.
		sqliteBlocksPath := filepath.Join(cfg.SQLiteDir, "blocks.db")
		if ds, err := NewSQLiteDataStorage(sqliteBlocksPath); err != nil {
			log.Printf("failed to init SQLiteDataStorage (%v), falling back to filesystem", err)
			all.DataStorage = NewDataStorage(cfg.DataDir)
		} else {
			log.Printf("Using SQLiteDataStorage for block/inscription metadata at %s", sqliteBlocksPath)
			all.DataStorage = ds
		}
	default:
		// memory (and any unknown) → fast filesystem + RAM cache.
		// Explicitly intended for ease of debugging business logic and unit tests
		// (as clarified: no hybrid JSON+sqlite mode is supported — it duplicates data).
		all.DataStorage = NewDataStorage(cfg.DataDir)
	}

	// 4. Smart Contract / MCP Store + matching API key store + IngestionService
	var mcpStore scstore.Store
	var apiIssuer auth.APIKeyIssuer
	var apiValidator auth.APIKeyValidator
	var ingestSvc *services.IngestionService
	actualType := string(cfg.Type)

	switch cfg.Type {
	case StoragePostgres:
		if cfg.PGDSN == "" {
			log.Printf("STARGATE_STORAGE=postgres but no PGDSN — falling back to sqlite")
			cfg.Type = StorageSQLite
			// fallthrough to sqlite logic below
		} else {
			var err error
			mcpStore, err = scstore.NewPGStore(context.Background(), cfg.PGDSN, cfg.ClaimTTL, cfg.SeedFixtures)
			if err != nil {
				log.Fatalf("failed to connect to PostgreSQL MCP store (%v). Exiting to prevent data loss.", err)
			}
			actualType = "postgres"

			// API keys on PG
			pgKeys, err := auth.NewPGAPIKeyStore(context.Background(), cfg.PGDSN)
			if err != nil {
				log.Fatalf("failed to initialize PostgreSQL API key store (%v). Exiting to prevent data loss.", err)
			}
			pgKeys.SeedEnvironmentVariables()
			apiIssuer, apiValidator = pgKeys, pgKeys

			// Ingestion (PG)
			if svc, serr := services.NewIngestionService(cfg.PGDSN); serr != nil {
				log.Printf("ingestion service unavailable: %v", serr)
			} else {
				ingestSvc = svc
			}
		}
	}

	if cfg.Type == StorageSQLite || cfg.Type == StorageMemory {
		// SQLite path (preferred embedded durable mode)
		if cfg.Type == StorageSQLite {
			var err error
			mcpStore, err = scstore.NewSQLiteStore(cfg.MCPDBPath, cfg.ClaimTTL, cfg.SeedFixtures)
			if err != nil {
				log.Printf("failed to create SQLite MCP store (%v), falling back to memory store", err)
				mcpStore = scstore.NewMemoryStore(cfg.ClaimTTL)
				actualType = "memory"
			} else {
				actualType = "sqlite"
				log.Printf("Using embedded SQLite MCP store at %s", cfg.MCPDBPath)
			}

			if actualType == "memory" {
				// MCP store already fell back to memory — use in-memory API keys too
				memKeys := auth.NewAPIKeyStore()
				memKeys.SeedEnvironmentVariables()
				apiIssuer, apiValidator = memKeys, memKeys
			} else {
				// SQLite-backed API keys
				sqliteKeys, err := auth.NewSQLiteAPIKeyStore(cfg.APIKeysDBPath)
				if err != nil {
					log.Printf("failed to initialize SQLite API key store (%v), falling back to memory store", err)
					memKeys := auth.NewAPIKeyStore()
					memKeys.SeedEnvironmentVariables()
					apiIssuer, apiValidator = memKeys, memKeys
					// Downgrade overall type since we couldn't persist keys either
					actualType = "memory"
				} else {
					sqliteKeys.SeedEnvironmentVariables()
					apiIssuer, apiValidator = sqliteKeys, sqliteKeys
					log.Printf("Using SQLite API key store at %s", cfg.APIKeysDBPath)
				}
			}

			// Ingestion via SQLite
			if svc, serr := services.NewIngestionService(cfg.IngestionsDBPath); serr != nil {
				log.Printf("ingestion service (sqlite) unavailable: %v", serr)
			} else {
				ingestSvc = svc
				log.Printf("Using SQLite ingestion service at %s", cfg.IngestionsDBPath)
			}

		} else {
			// Pure memory
			mcpStore = scstore.NewMemoryStore(cfg.ClaimTTL)
			actualType = "memory"

			memKeys := auth.NewAPIKeyStore()
			memKeys.SeedEnvironmentVariables()
			apiIssuer, apiValidator = memKeys, memKeys
		}
	}

	// If we fell through from postgres to sqlite above, handle it
	if mcpStore == nil {
		// default sqlite path when postgres was requested but dsn missing
		var err error
		mcpStore, err = scstore.NewSQLiteStore(cfg.MCPDBPath, cfg.ClaimTTL, cfg.SeedFixtures)
		if err != nil {
			mcpStore = scstore.NewMemoryStore(cfg.ClaimTTL)
			actualType = "memory"
		} else {
			actualType = "sqlite"
		}
		// minimal sqlite keys
		if k, err := auth.NewSQLiteAPIKeyStore(cfg.APIKeysDBPath); err == nil {
			k.SeedEnvironmentVariables()
			apiIssuer, apiValidator = k, k
		} else {
			mem := auth.NewAPIKeyStore()
			mem.SeedEnvironmentVariables()
			apiIssuer, apiValidator = mem, mem
		}
	}

	all.SmartContractStore = mcpStore
	all.APIKeyIssuer = apiIssuer
	all.APIKeyValidator = apiValidator
	all.IngestionService = ingestSvc

	log.Printf("AllStores initialized with storage type=%s", actualType)
	return all, nil
}
