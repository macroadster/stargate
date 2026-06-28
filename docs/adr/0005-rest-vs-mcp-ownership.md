# ADR 0005: REST vs MCP ownership

- **Status:** Accepted
- **Date:** 2026-06-28
- **Deciders:** Stargate maintainers
- **Tags:** api, mcp, agents

## Context

Stargate exposes overlapping surfaces:

- `/api/smart_contract/*` — lifecycle REST
- `/mcp/*` — HTTP MCP tools / JSON-RPC for agents
- `/api/data/*` — block/inscription browse data
- `/api/open-contracts` and legacy aliases — UI wish list (inscription-shaped)
- `/bitcoin/v1/*` — scan/extract

Duplicate handlers and schema lists (`getToolSchemasLegacy`) caused drift.

## Decision

**REST under `/api/smart_contract/*` is the primary lifecycle API. MCP is a thin tool shim over the same store/application layer, not a second business stack.**

| Audience | Primary surface |
| --- | --- |
| Human UI (lifecycle) | `/api/smart_contract/*` |
| Human UI (wish browse, inscription-shaped) | `/api/open-contracts` (aliases deprecated via headers) |
| Human UI (blocks/images) | `/api/data/*` |
| Agents (tool-calling clients) | `/mcp/tools` + `/mcp/call` (or JSON-RPC `/mcp`) mapping to the same store as REST |
| Scan/extract | `/bitcoin/v1/*` |

Machine-readable catalog: **`GET /api/surfaces`** (`backend/api/surfaces.go`) lists primaries, legacy aliases (`Deprecation` + `Link` headers), and MCP tool → REST backing paths.

MCP tool schemas prefer guidance (`/mcp/SKILL.md`); hardcoded schemas are **fallback only** (`getToolSchemasFallback`).

## Consequences

**Positive**

- One source of truth for proposals/tasks/submissions
- Agents and UI converge on the same data
- Legacy paths remain compatible with explicit deprecation

**Negative / trade-offs**

- MCP may still call the store in-process rather than HTTP-round-tripping REST (acceptable; must not fork logic)
- Removing aliases requires a migration window for UIs

## Related

- [MCP_UNIFIED_PLAN.md](../arch/MCP_UNIFIED_PLAN.md)
- ADR 0001 — same process hosts REST + MCP
- ADR 0003 — shared identity across both surfaces
