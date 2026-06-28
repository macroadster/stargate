# Package boundaries (backend)

Status: **canonical** (stargate-3bk.6)  
Related: [MCP_UNIFIED_PLAN.md](./MCP_UNIFIED_PLAN.md), `backend/app/doc.go`

## Layers

```
┌─────────────────────────────────────────────────────────┐
│  Transport                                              │
│  handlers/  mcp/  api/  (HTTP adapters, MCP tools)      │
├─────────────────────────────────────────────────────────┤
│  Application                                            │
│  app/smart_contract/  (+ services/)  agents/            │
│  orchestration, use-cases, background sync              │
├─────────────────────────────────────────────────────────┤
│  Domain                                                 │
│  core/smart_contract/  stego/  bitcoin (builders)       │
│  types, pure rules, protocol helpers                    │
├─────────────────────────────────────────────────────────┤
│  Persistence                                            │
│  storage/smart_contract  storage/auth  storage/*        │
├─────────────────────────────────────────────────────────┤
│  HTTP middleware (cross-cutting only)                   │
│  middleware/  — CORS, recovery, API key, timeouts       │
└─────────────────────────────────────────────────────────┘
```

## Rules

1. **Do not** put business logic in `middleware/` (except `app/` historically lived under `middleware/smart_contract` — **moved to `app/smart_contract`**).
2. **Do not** grow `app/smart_contract/server*.go` with new domain rules — add methods on `app/smart_contract/services/*` or `core/smart_contract`.
3. **Prefer** `storage/smart_contract.Store` over re-exported `app/smart_contract.Store` in new packages.
4. **Transport** (`handlers`, `mcp`, `api`) may call `app` and `storage`; they must not reimplement store workflows.
5. **Agents** use the same store/app surfaces as MCP/REST (no parallel persistence).

## Import path migration

| Old | New |
| --- | --- |
| `stargate-backend/middleware/smart_contract` | `stargate-backend/app/smart_contract` |
| `stargate-backend/middleware/smart_contract/services` | `stargate-backend/app/smart_contract/services` |

HTTP middleware remains `stargate-backend/middleware`.

## Surfaces vs packages

API ownership (REST vs MCP vs data) is documented separately in `GET /api/surfaces` and [MCP_UNIFIED_PLAN.md](./MCP_UNIFIED_PLAN.md). That is **route** ownership; this document is **Go package** ownership.

## Domain entanglement
See [DOMAIN_SEAMS.md](./DOMAIN_SEAMS.md) for stego / ingestion / bitcoin / contract seams and `core/identity`.

## ADRs

Decision records: [../adr/README.md](../adr/README.md).
