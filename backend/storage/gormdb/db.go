// Package gormdb is the shared GORM connection layer for Stargate SQL dialects.
//
// ADR 0002 requires both SQLite (default single-binary) and Postgres (shared HA).
// Callers open a *gorm.DB once via OpenSQLite / OpenPostgres / OpenMemory and
// pass it to domain repositories so sqlite/pg CRUD is not hand-duplicated.
//
// SQLite uses modernc.org/sqlite (pure-Go) via a local dialector so the process
// never double-registers the "sqlite" driver name (glebarez conflicts with modernc).
package gormdb

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Dialect identifies the SQL backend behind a *gorm.DB.
type Dialect string

const (
	DialectSQLite   Dialect = "sqlite"
	DialectPostgres Dialect = "postgres"
	DialectMemory   Dialect = "memory"
)

// Config tunes connection pooling for a GORM open.
type Config struct {
	// MaxOpenConns defaults: SQLite 10, Postgres 20.
	MaxOpenConns int
	// MaxIdleConns defaults: SQLite 5, Postgres 10.
	MaxIdleConns int
	// ConnMaxLifetime defaults to 1h.
	ConnMaxLifetime time.Duration
	// ConnMaxIdleTime defaults to 30m (Postgres) / 0 (SQLite).
	ConnMaxIdleTime time.Duration
	// LogLevel defaults to Silent (application logs own messages).
	LogLevel logger.LogLevel
	// SkipDefaultTransaction can reduce SQLite write overhead for simple ops.
	SkipDefaultTransaction bool
}

func defaultGormConfig(cfg Config) *gorm.Config {
	level := cfg.LogLevel
	if level == 0 {
		level = logger.Silent
	}
	return &gorm.Config{
		Logger:                 logger.Default.LogMode(level),
		SkipDefaultTransaction: cfg.SkipDefaultTransaction,
		DisableForeignKeyConstraintWhenMigrating: false,
	}
}

// OpenSQLite opens (or creates) a file-backed SQLite database via pure-Go modernc.
// path may be absolute or relative; parent dirs are created when needed.
func OpenSQLite(path string, cfg ...Config) (*gorm.DB, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("gormdb: empty sqlite path")
	}
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("gormdb: mkdir %s: %w", dir, err)
		}
	}
	// foreign_keys + WAL for durability under concurrent readers (block monitor + API).
	// DSN query form matches modernc.org/sqlite (same as MCP / ingestion stores).
	dsn := path + "?_foreign_keys=on&_journal_mode=WAL"
	c := Config{}
	if len(cfg) > 0 {
		c = cfg[0]
	}
	if c.MaxOpenConns == 0 {
		c.MaxOpenConns = 10
	}
	if c.MaxIdleConns == 0 {
		c.MaxIdleConns = 5
	}
	if c.ConnMaxLifetime == 0 {
		c.ConnMaxLifetime = time.Hour
	}
	c.SkipDefaultTransaction = true

	db, err := gorm.Open(moderncSQLiteDialector{DSN: dsn}, defaultGormConfig(c))
	if err != nil {
		return nil, fmt.Errorf("gormdb: open sqlite %s: %w", path, err)
	}
	if err := applyPool(db, c); err != nil {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			_ = sqlDB.Close()
		}
		return nil, err
	}
	if err := db.Use(&dialectPlugin{d: DialectSQLite}); err != nil {
		return nil, err
	}
	return db, nil
}

// OpenMemory opens a shared in-memory SQLite database (tests / ephemeral SQL).
func OpenMemory(cfg ...Config) (*gorm.DB, error) {
	c := Config{}
	if len(cfg) > 0 {
		c = cfg[0]
	}
	if c.MaxOpenConns == 0 {
		c.MaxOpenConns = 5
	}
	if c.MaxIdleConns == 0 {
		c.MaxIdleConns = 5
	}
	c.SkipDefaultTransaction = true

	// cache=shared keeps schema visible across connections from the pool.
	dsn := "file::memory:?cache=shared&_foreign_keys=on"
	db, err := gorm.Open(moderncSQLiteDialector{DSN: dsn}, defaultGormConfig(c))
	if err != nil {
		return nil, fmt.Errorf("gormdb: open memory: %w", err)
	}
	if err := applyPool(db, c); err != nil {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			_ = sqlDB.Close()
		}
		return nil, err
	}
	if err := db.Use(&dialectPlugin{d: DialectMemory}); err != nil {
		return nil, err
	}
	return db, nil
}

// OpenPostgres opens a Postgres database via the official GORM postgres driver (pgx).
func OpenPostgres(dsn string, cfg ...Config) (*gorm.DB, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, fmt.Errorf("gormdb: empty postgres DSN")
	}
	c := Config{}
	if len(cfg) > 0 {
		c = cfg[0]
	}
	if c.MaxOpenConns == 0 {
		c.MaxOpenConns = 20
	}
	if c.MaxIdleConns == 0 {
		c.MaxIdleConns = 10
	}
	if c.ConnMaxLifetime == 0 {
		c.ConnMaxLifetime = time.Hour
	}
	if c.ConnMaxIdleTime == 0 {
		c.ConnMaxIdleTime = 30 * time.Minute
	}

	db, err := gorm.Open(postgres.Open(dsn), defaultGormConfig(c))
	if err != nil {
		return nil, fmt.Errorf("gormdb: open postgres: %w", err)
	}
	if err := applyPool(db, c); err != nil {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			_ = sqlDB.Close()
		}
		return nil, err
	}
	if err := db.Use(&dialectPlugin{d: DialectPostgres}); err != nil {
		return nil, err
	}
	return db, nil
}

// DialectOf returns the dialect registered when the DB was opened via this package.
// Unknown/third-party DBs return "".
func DialectOf(db *gorm.DB) Dialect {
	if db == nil {
		return ""
	}
	if p, ok := db.Config.Plugins[dialectPluginName].(*dialectPlugin); ok && p != nil {
		return p.d
	}
	name := strings.ToLower(db.Dialector.Name())
	switch {
	case name == "sqlite":
		return DialectSQLite
	case name == "postgres" || name == "postgresql":
		return DialectPostgres
	default:
		return Dialect(name)
	}
}

// Close closes the underlying sql.DB.
func Close(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func applyPool(db *gorm.DB, c Config) error {
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("gormdb: sql.DB: %w", err)
	}
	sqlDB.SetMaxOpenConns(c.MaxOpenConns)
	sqlDB.SetMaxIdleConns(c.MaxIdleConns)
	if c.ConnMaxLifetime > 0 {
		sqlDB.SetConnMaxLifetime(c.ConnMaxLifetime)
	}
	if c.ConnMaxIdleTime > 0 {
		sqlDB.SetConnMaxIdleTime(c.ConnMaxIdleTime)
	}
	return nil
}

const dialectPluginName = "stargate:dialect"

// dialectPlugin tags a *gorm.DB with the Stargate dialect for DialectOf.
type dialectPlugin struct {
	d Dialect
}

func (p *dialectPlugin) Name() string { return dialectPluginName }

func (p *dialectPlugin) Initialize(db *gorm.DB) error { return nil }
