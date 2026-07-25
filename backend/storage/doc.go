// Package storage is the persistence layer for Stargate.
//
// Dialects (ADR 0002): SQLite is the default for single-binary installs;
// Postgres is first-class when STARGATE_STORAGE=postgres and a DSN is set.
// Both dialects implement the same interfaces (smart_contract.Store, auth
// API key interfaces, DataStorage). Shared validation and prepare helpers
// live next to implementations to prevent sqlite/pg drift; do not delete
// either dialect.
//
// GORM layer (stargate-e4z / stargate-jh0): durable SQL for api_keys,
// block_scans, and MCP smart-contract (SQLStore) goes through storage/gormdb
// (pure-Go SQLite + Postgres). Policy for claims/proposals lives in
// storage/smart_contract/policy_*.go; MemoryStore remains for pure RAM tests.
//
// Prefer NewAllStores / StorageConfig over ad-hoc initialization.
package storage
