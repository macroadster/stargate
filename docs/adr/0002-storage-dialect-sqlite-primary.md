# ADR 0002: Storage dialects — SQLite default, Postgres supported

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

Operators need **both**:

1. **Zero-dependency single-binary / laptop / edge** installs (no external DB).
2. **Shared multi-writer / HA-friendly** deployments (Postgres).

Claiming “SQLite only” would be inaccurate and break production shared hosting.

## Decision

**Support both SQLite and Postgres as first-class dialects.**  
**SQLite is the default** when no Postgres DSN / storage override is set (best for ADR 0001 single-binary).  
**Postgres is fully supported** when configured—not a temporary legacy path and **must not be removed** without an explicit superseding ADR.

Selection (see `storage/factory.go`, `container/container.go`):

| Mode | Typical env |
| --- | --- |
| SQLite (default) | unset, or `STARGATE_STORAGE=sqlite` |
| Postgres | `STARGATE_STORAGE=postgres` and `STARGATE_PG_DSN` (or `DATABASE_URL` for data layer) |
| Memory | tests / ephemeral (`STARGATE_STORAGE=memory`) |

Rules:

- Interfaces live under `storage/*`; keep **both** `sqlite_*` and `pg_*` implementations in sync at the interface boundary.
- New features must work on **both** dialects unless explicitly documented as dialect-specific.
- `cmd/migrate-pg-to-sqlite` is an **optional migration tool** for operators moving *to* SQLite—not a mandate to abandon Postgres.
- Unification work (stargate-3bk.3) means **reduce duplication** (shared logic, one interface, less drift)—**not** delete Postgres support.

## Consequences

**Positive**

- Single-binary / offline works out of the box (SQLite)
- Shared clusters can use Postgres without a fork
- Clear env matrix for deploy docs

**Negative / trade-offs**

- Two implementations to maintain (mitigate via shared helpers / interface tests)
- Historical near-duplicates between sqlite_store and pg_store need ongoing DRY, not deletion of PG

**Non-goals**

- Requiring Postgres for all installs
- Requiring SQLite for all installs
- Removing either dialect in favor of the other

## Related

- `backend/storage/`, `backend/storage/factory.go`
- ADR 0001 — single binary (SQLite default fits embedded deploy)
- stargate-3bk.3 — unify implementations **while keeping both dialects**
