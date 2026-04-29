package storage

import (
	"context"
	"log"
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

	// Ingestion (currently PG-only; will be generalized in later phases)
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
	default:
		// sqlite and memory modes currently keep the proven filesystem data layer
		// (SQLite data layer can be added in a follow-up)
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
				log.Printf("failed to create SQLite MCP store (%v), falling back to memory", err)
				mcpStore = scstore.NewMemoryStore(cfg.ClaimTTL)
				actualType = "memory"
			} else {
				actualType = "sqlite"
				log.Printf("Using embedded SQLite MCP store at %s", cfg.MCPDBPath)
			}

			// SQLite-backed API keys
			sqliteKeys, err := auth.NewSQLiteAPIKeyStore(cfg.APIKeysDBPath)
			if err != nil {
				log.Fatalf("failed to initialize SQLite API key store (%v). Exiting to prevent data loss.", err)
			}
			sqliteKeys.SeedEnvironmentVariables()
			apiIssuer, apiValidator = sqliteKeys, sqliteKeys
			log.Printf("Using SQLite API key store at %s", cfg.APIKeysDBPath)

			// Ingestion via SQLite path (still uses its own DB file for now)
			// For full consistency we would pass the ingestions DB path; keeping
			// existing behaviour in this skeleton.
			// ingestSvc remains nil unless PG is present.

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
