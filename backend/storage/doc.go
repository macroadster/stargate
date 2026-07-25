// Package storage is the persistence layer for Stargate.
//
// Dialects (ADR 0002): SQLite is the default for single-binary installs;
// Postgres is first-class when STARGATE_STORAGE=postgres and a DSN is set.
// Both dialects implement the same interfaces (smart_contract.Store, auth
// API key interfaces, DataStorage). Shared validation and prepare helpers
// live next to implementations to prevent sqlite/pg drift; do not delete
// either dialect.
//
// GORM layer (stargate-e4z): durable SQL CRUD for api_keys and block_scans
// goes through storage/gormdb (pure-Go SQLite + Postgres dialectors) so
// sqlite/pg are one implementation, not two hand-written twins. Smart-contract
// MCP stores still use dialect-specific files while business rules move into
// app/; they can adopt gormdb incrementally.
//
// Prefer NewAllStores / StorageConfig over ad-hoc initialization.
package storage
