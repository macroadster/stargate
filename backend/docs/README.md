# backend/docs

Status: **historical** (markdown) / **generated** (OpenAPI)

Do **not** use the `*.md` files in this directory as the API. They predate
surface retirement (3bk.8) and still mention `/mcp/v1`, `/api/smart-contracts`,
and `/api/contract-stego`.

| Need | Use |
| --- | --- |
| Live route catalog | `GET /api/surfaces` |
| REST vs MCP | `docs/arch/MCP_UNIFIED_PLAN.md`, ADR 0005 |
| Retired aliases | `docs/arch/LEGACY_RETIREMENT.md` |
| Agent tools | `/mcp/SKILL.md`, `/mcp/docs`, `/mcp/openapi.json` |
| Swagger UI | `/api/docs/` (generated `openapi.yaml` / `docs.go` in this folder) |

The markdown summaries (`API_DOCUMENTATION.md`, `SMART_CONTRACT_API.md`,
block-monitor writeups, etc.) stay only so old links do not 404. Treat them as
attic, same as `docs/history/`.
