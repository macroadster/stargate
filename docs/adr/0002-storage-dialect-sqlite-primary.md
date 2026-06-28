# ADR 0002: Storage dialect — SQLite primary

- **Status:** Accepted
- **Date:** 2026-06-28
- **Deciders:** Stargate maintainers
- **Tags:** storage, sqlite, postgres

## Context

Stargate needs durable state for:

- Smart contracts / proposals / tasks / submissions (`storage/smart_contract`)
- API keys / challenges (`storage/auth`)
- Block / inscription caches (`storage` data layer)
- Ingestion records (`services.IngestionService` / SQLite or PG-backed paths)

Postgres remains valuable for multi-writer or shared hosting, but the default developer and single-binary experience should work with **zero external database**.

## Decision

**SQLite is the primary default persistence dialect for the single-binary product.** Postgres remains a supported alternate when `STARGATE_PG_DSN` / `DATABASE_URL` (or explicit `STARGATE_STORAGE=postgres`) is configured.

- Default paths under data dir / `UPLOADS_DIR` / env-driven `*.db` files (`mcp.db`, `api_keys.db`, `ingestions.db`, `blocks.db` as applicable)
- Interfaces live under `storage/*`; implementations may be memory, SQLite, or Postgres
- New features must work on SQLite first; PG-only paths require explicit justification
- In-memory store is for tests and optional seed fixtures (`MCP_SEED_FIXTURES`), not production default

## Consequences

**Positive**

- `curl | bash` / single node works offline
- Simple backups (copy files)
- Aligns with embedded deploy (ADR 0001)

**Negative / trade-offs**

- Limited multi-process writers on one SQLite file
- Some historical code still branches on PG (ingestion sync, funding sync) — must keep feature parity or clearly gate
- Operators needing HA should use Postgres and shared storage for uploads/IPFS

**Follow-ups**

- Prefer `storage` factories over ad-hoc DSN checks in `main`
- Document env matrix in deployment guide

## Related

- `backend/storage/`, `docs/history/STORAGE_REFACTORING_STRATEGY.md`
- ADR 0001 — single binary
