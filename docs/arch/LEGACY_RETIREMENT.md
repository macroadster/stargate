# Legacy retirement inventory (stargate-3bk.8)

Status: **in progress / partially complete**  
Related: ADR 0001, 0002, 0005; related: **stargate-3bk.3** (unify stores, keep both dialects)

## Retired in this change

| Item | Action |
| --- | --- |
| `getToolSchemasLegacy` + hardcoded MCP schema table in `mcp/tools.go` | **Removed** — schemas only from `GuidanceManifest` |
| `make backend` / `make frontend` / `*-legacy` images | **Fail fast** with message to use `make docker` / `make single-binary` |
| Routes `/api/smart-contracts`, `/api/contracts-confirmed`, `/api/data/contracts-with-pagination` | **Unregistered** (unused by current frontend; use `/api/open-contracts`) |
| Routes `/api/blocks`, `/api/block-images` | **Unregistered** (use `/api/data/blocks`, `/api/data/block-images`) |

## Kept intentionally

| Item | Reason | Sunset |
| --- | --- | --- |
| `VerifyLegacySignMessage` (compact wallet signmessage) | Bitcoin Core wallet protocol, not app debt | None — keep alongside BIP-322 |
| `/api/contract-stego` (+ create) | Still used by `StegoAnalysisViewer` | Migrate UI → `/api/smart_contract/*` then remove |
| Postgres store implementations (`pg_store`, `apikey_store_pg`) | **First-class** dialect for multi-writer / shared deploys (ADR 0002); SQLite is default only | **Keep forever** unless a new ADR supersedes; 3bk.3 = reduce duplication, not remove PG |
| Filesystem block path `height_00000000` fallback | On-disk layout compatibility | Keep until data migration tool exists |
| Inscription `?legacy=1` query | Opt-in file-based pending items | Prefer ingestion store; remove after one release without callers |

## Deprecation window (external clients)

If any out-of-repo client used retired routes, switch to:

| Old | New |
| --- | --- |
| `GET /api/smart-contracts` | `GET /api/open-contracts` or `GET /api/smart_contract/contracts` |
| `GET /api/contracts-confirmed` | `GET /api/open-contracts` |
| `GET /api/data/contracts-with-pagination` | `GET /api/open-contracts` |
| `GET /api/blocks` | `GET /api/data/blocks` |
| `GET /api/block-images` | `GET /api/data/block-images` |

Catalog of remaining surfaces: `GET /api/surfaces`.

## Verification checklist

- [x] Frontend uses `/api/open-contracts` (useContracts) and `/api/data/*` for blocks
- [x] MCP tests pass with guidance-only schemas
- [ ] Optional PG↔SQLite migration tooling / shared tests for both dialects — **3bk.3** (do **not** remove Postgres)
- [ ] StegoAnalysisViewer migrated off `/api/contract-stego`
