# Legacy retirement inventory (stargate-3bk.8)

Status: **complete for app/API legacy** (Postgres remains first-class — not legacy)  
Related: ADR 0001, 0002, 0005; storage DRY: **stargate-3bk.3** (keep both dialects)

## Retired

| Item | Action |
| --- | --- |
| `getToolSchemasLegacy` + hardcoded MCP schema table | **Removed** — `GuidanceManifest` only |
| `make backend` / `make frontend` / `*-legacy` images | **Fail fast** → `make docker` / `make single-binary` |
| Routes `/api/smart-contracts`, `/api/contracts-confirmed`, `/api/data/contracts-with-pagination` | **Unregistered** |
| Routes `/api/blocks`, `/api/block-images` | **Unregistered** → `/api/data/*` |
| Routes `/api/contract-stego`, `/api/contract-stego/create` | **Unregistered** — UI uses `/api/smart_contract/contracts/{id}` |
| Inscriptions `?legacy=1` + file-based pending merge | **Removed** — always store + ingestion queue |

## Kept intentionally (not app legacy)

| Item | Reason |
| --- | --- |
| `VerifyLegacySignMessage` (compact wallet signmessage) | Bitcoin Core **wallet protocol** (with BIP-322) |
| Postgres (`pg_store`, `apikey_store_pg`, `PostgresStorage`) | **First-class** dialect (ADR 0002); SQLite is default only |
| Filesystem block path `height_00000000` fallback | On-disk layout compatibility |

## Client migration

| Old | New |
| --- | --- |
| `GET /api/smart-contracts` | `GET /api/open-contracts` or `GET /api/smart_contract/contracts` |
| `GET /api/contracts-confirmed` | `GET /api/open-contracts` |
| `GET /api/data/contracts-with-pagination` | `GET /api/open-contracts` |
| `GET /api/blocks` | `GET /api/data/blocks` |
| `GET /api/block-images` | `GET /api/data/block-images` |
| `GET /api/contract-stego/...` | `GET /api/smart_contract/contracts/{id}` |
| `POST /api/contract-stego/create` | `POST /api/smart_contract/proposals` |
| `GET /api/inscriptions?legacy=1` | `GET /api/inscriptions` (ingestion-only rows included by default) |

Catalog: `GET /api/surfaces` (documents retired paths under `aliases` for discovery).
