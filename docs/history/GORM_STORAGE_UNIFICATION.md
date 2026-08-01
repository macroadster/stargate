# GORM storage unification (stargate-e4z)

**Branch:** `dev`  
**Date:** 2026-07-25  
**ADR:** 0002 (keep SQLite + Postgres; reduce drift)

## Goal

Collapse hand-duplicated SQLite / Postgres implementations behind one GORM connection
layer (`storage/gormdb`) and one repository per domain for thin CRUD.

## Done in this change

| Domain | Before | After |
| --- | --- | --- |
| API keys | `apikey_store_sqlite.go` + `apikey_store_pg.go` | `GORMAPIKeyStore` (`apikey_store_gorm.go`); constructors `NewSQLiteAPIKeyStore` / `NewPGAPIKeyStore` preserved |
| Block scans | `sqlite_data_storage.go` + `postgres_storage.go` | `SQLDataStorage` (`sql_data_storage.go`); same constructor names |
| Connection open | ad-hoc `sql.Open` / `pgxpool` | `gormdb.OpenSQLite` / `OpenPostgres` / `OpenMemory` (pure-Go SQLite via glebarez) |

Memory map store (`APIKeyStore`) and filesystem `DataStorage` remain for
`STARGATE_STORAGE=memory` and filesystem mode.

## Not yet migrated (follow-ups)

1. **Smart contract MCP Store** (`sqlite_store` / `pg_store` / `memory_store` ~5.5k LOC)  
   - Extract business rules into `app/smart_contract` first (claim, approve, publish).  
   - Then point durable CRUD at GORM models; memory stays in-process or sqlite memory.
2. **Ingestion service** — already dual-dialect in one file; optional GORM later.
3. **Auth ChallengeStore** — TTL memory only; no SQL needed.

## Operator impact

None intended: env vars (`STARGATE_STORAGE`, DSN paths) and table names
(`api_keys`, `block_scans`) are unchanged. AutoMigrate may add missing columns
idempotently on startup.

## Dependencies

- `gorm.io/gorm`
- `gorm.io/driver/postgres`
- local pure-Go SQLite dialector in `storage/gormdb` over `modernc.org/sqlite`
  (avoids glebarez double-register of driver name `sqlite`)
