package storage

import (
	"os"
	"path/filepath"
	"strconv"
	"time"

	"stargate-backend/storage/datadir"
)

// StorageType identifies the backend persistence mode.
// It is driven primarily by STARGATE_STORAGE and falls back to
// presence of STARGATE_PG_DSN for compatibility.
type StorageType string

const (
	StorageMemory    StorageType = "memory"
	StorageSQLite    StorageType = "sqlite"
	StoragePostgres  StorageType = "postgres"
	StorageFilesystem StorageType = "filesystem" // legacy data layer only
)

// StorageConfig holds all parameters needed to initialize every storage
// backend in a consistent way. It is the single source of truth for
// STARGATE_STORAGE-driven decisions.
//
// Supported modes (Phase 6/7 clarification):
//   - sqlite   : durable embedded single-binary distribution (mcp.db + api_keys.db +
//                blocks.db for inscription metadata). Recommended default.
//   - memory   : ephemeral in-memory (API keys + RAM cache). Intended for ease of
//                debugging business logic and fast unit tests. No durable files.
//   - postgres : shared durable production backend.
//   - filesystem: legacy explicit mode (kept for compatibility).
//
// There is deliberately no "hybrid" mode (filesystem JSON primary + sqlite cache).
// It would duplicate data and waste disk space. When using sqlite, the block
// metadata lives authoritatively in the SQLite block_scans table.
type StorageConfig struct {
	Type StorageType

	// PostgreSQL
	PGDSN string // from STARGATE_PG_DSN or DATABASE_URL

	// Base directories
	DataDir          string // blocks, uploads, etc.
	SQLiteDir        string // where mcp.db / api_keys.db / ingestions.db live

	// Explicit DB paths (override derived paths)
	MCPDBPath        string // STARGATE_MCP_DB
	APIKeysDBPath    string // STARGATE_API_KEYS_DB
	IngestionsDBPath string // STARGATE_INGESTIONS_DB

	// Smart contract / MCP behaviour
	ClaimTTL     time.Duration
	SeedFixtures bool

	// Contract cache (used by middleware + handlers)
	ContractCacheTTL  time.Duration
	ContractCacheSize int

	// Data layer cache
	BlockCacheTimeout time.Duration
}

// LoadStorageConfigFromEnv builds a StorageConfig by reading the canonical
// environment variables. This replaces the scattered logic that used to live
// in stargate_backend.go and container/container.go.
//
// Priority for Type:
//   1. Explicit STARGATE_STORAGE (memory|sqlite|postgres|filesystem)
//   2. If STARGATE_PG_DSN / DATABASE_URL is set → postgres
//   3. Default: sqlite (best for single-binary embedded usage)
func LoadStorageConfigFromEnv() StorageConfig {
	cfg := StorageConfig{}

	// 1. Determine storage type
	storageEnv := os.Getenv("STARGATE_STORAGE")
	switch StorageType(storageEnv) {
	case StorageMemory, StorageSQLite, StoragePostgres, StorageFilesystem:
		cfg.Type = StorageType(storageEnv)
	default:
		if dsn := getPGDSN(); dsn != "" {
			cfg.Type = StoragePostgres
		} else {
			cfg.Type = StorageSQLite // sensible default for single-binary
		}
	}

	// 2. PG DSN (used by postgres type and as fallback signal)
	cfg.PGDSN = getPGDSN()

	// 3. Directories
	cfg.DataDir = os.Getenv("STARGATE_DATA_DIR")
	if cfg.DataDir == "" {
		cfg.DataDir = "data"
	}
	cfg.SQLiteDir = filepath.Join(cfg.DataDir, "sqlite")
	_ = os.MkdirAll(cfg.SQLiteDir, 0755) // best effort

	// 4. Explicit DB file paths (or derive under SQLiteDir)
	cfg.MCPDBPath = os.Getenv("STARGATE_MCP_DB")
	if cfg.MCPDBPath == "" {
		cfg.MCPDBPath = filepath.Join(cfg.SQLiteDir, "mcp.db")
	}
	cfg.APIKeysDBPath = os.Getenv("STARGATE_API_KEYS_DB")
	if cfg.APIKeysDBPath == "" {
		cfg.APIKeysDBPath = filepath.Join(cfg.SQLiteDir, "api_keys.db")
	}
	cfg.IngestionsDBPath = os.Getenv("STARGATE_INGESTIONS_DB")
	if cfg.IngestionsDBPath == "" {
		cfg.IngestionsDBPath = filepath.Join(cfg.SQLiteDir, "ingestions.db")
	}

	// 5. TTLs and behaviour flags
	if h := os.Getenv("STARGATE_DEFAULT_CLAIM_TTL_HOURS"); h != "" {
		if v, err := strconv.Atoi(h); err == nil && v > 0 {
			cfg.ClaimTTL = time.Duration(v) * time.Hour
		}
	}
	if cfg.ClaimTTL == 0 {
		cfg.ClaimTTL = 1 * time.Hour
	}

	if s := os.Getenv("STARGATE_SEED_FIXTURES"); s != "" {
		if v, err := strconv.ParseBool(s); err == nil {
			cfg.SeedFixtures = v
		}
	} else {
		cfg.SeedFixtures = false
	}

	// Contract cache
	cfg.ContractCacheTTL = 2 * time.Minute
	cfg.ContractCacheSize = 1000
	if d := os.Getenv("CONTRACT_CACHE_TTL"); d != "" {
		if dur, err := time.ParseDuration(d); err == nil {
			cfg.ContractCacheTTL = dur
		}
	}
	if sz := os.Getenv("CONTRACT_CACHE_SIZE"); sz != "" {
		if n, err := strconv.Atoi(sz); err == nil && n > 0 {
			cfg.ContractCacheSize = n
		}
	}

	// Data layer cache timeout
	cfg.BlockCacheTimeout = 30 * time.Minute

	return cfg
}

// DefaultDataDir returns the root data directory (delegates to datadir.Default).
func DefaultDataDir() string { return datadir.Default() }

// DefaultPath returns a path under the default data directory.
func DefaultPath(subpath string) string { return datadir.Path(subpath) }

func getPGDSN() string {
	if d := os.Getenv("STARGATE_PG_DSN"); d != "" {
		return d
	}
	return os.Getenv("DATABASE_URL")
}
