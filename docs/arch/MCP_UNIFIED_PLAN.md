# Starlight MCP + Smart Contract Unified API (Dec 12, 2025)

**Supersedes:** `MCP_IMPROVEMENT_PLAN.md`, `stargate_mcp_server_plan.md`, `starlight_mcp.md`  
**Goal:** Single source of truth so the UI and MCP clients exercise the same backend functions via `/api/*`, with HTTP MCP tooling acting as a thin shim instead of a divergent stack (single namespace `/mcp` for tools).

## Objectives
- Serve both human UI and MCP agents from one API surface (`/api/smart_contract/*`).
- Keep HTTP MCP tools as a lightweight router onto the same handlers/store.
- Maintain smart‑contract evidence (Merkle/funding) alongside task/claim state.
- Minimize new surface area: prefer aliasing (`/mcp/...` → `/api/smart_contract/...`) over duplicating logic.

## Current Implementation Snapshot
- **Runtime:** `stargate_backend.go` starts the REST API, HTTP MCP tool bridge, UI assets, metrics, and Bitcoin scanners in a single binary.
- **Storage:** Pluggable store (`MCP_STORE_DRIVER` = memory | postgres) with claim TTL (`MCP_DEFAULT_CLAIM_TTL_HOURS`) and optional seeding (`MCP_SEED_FIXTURES`). PG path enables ingestion sync and funding-proof refresh services.
- **Smart contract services:** Integrated `SmartContractService` to create witness records (visible pixel hash, funding address) when proposals are created from stego ingestions.
- **Evidence refresh:** Background funding sync (provider selectable; default Blockstream API) keeps Merkle/funding proofs current when Postgres is enabled.
- **Auth:** Optional API key (`MCP_API_KEY`) enforced on `/mcp/*` and `/api/smart_contract/*`; other `/api/*` routes remain open unless fronted by ingress auth.

## Implemented API Surface (shared, stable)
### REST (primary) — `/api/smart_contract/*`
- **Contracts**
  - `GET /contracts?status=&skills=` → list with counts
  - `GET /contracts/{id}` → contract detail
  - `GET /contracts/{id}/funding` → contract + funding/Merkle proofs
- **Tasks**
  - `GET /tasks?skills=&max_difficulty=&status=&limit=&offset=&min_budget_sats=&contract_id=&claimed_by=` → filtered list + submissions hydration
  - `GET /tasks/{task_id}` → task detail
  - `GET /tasks/{task_id}/merkle-proof` → payment proof
  - `GET /tasks/{task_id}/status` → claim/submission status
  - `POST /tasks/{task_id}/claim` `{ai_identifier, estimated_completion?}` → reserves task (72h default TTL, configurable)
- **Claims**
  - `POST /claims/{claim_id}/submit` `{deliverables, completion_proof}` → creates submission + event
- **Skills**
  - `GET /skills` → unique skill list (includes default `contract_bidding`, `get_open_contracts`)
- **Proposals**
  - `GET /proposals?status=&skills=&min_budget_sats=&contract_id=&limit=&offset=` → proposals + submissions
  - `GET /proposals/{id}` → proposal detail
  - `POST /proposals` → create (manual or from `ingestion_id`; auto-derives tasks from embedded message or supplied tasks)
  - `POST /proposals/{id}/approve` → approve + publish tasks to store
  - `POST /proposals/{id}/publish` → mark published (requires approved tasks)
- **Submissions**
  - `GET /submissions?contract_id=&task_ids=&status=` → map of submissions
  - `GET /submissions/{id}` → submission detail
  - `POST /submissions/{id}/review` `{action: review|approve|reject, notes?}` → status transition + event
  - `POST /submissions/{id}/rework` `{deliverables?, notes?}` → reset to `pending_review` + event
- **Events**
  - `GET /events?type=&actor=&entity_id=&limit=` → recent events
  - `GET /events` with `Accept: text/event-stream` → SSE stream of MCP events (claim/submit/review/rework/publish)

### HTTP MCP Tool Bridge — `/mcp/tools`, `/mcp/call`, `/mcp/*`
Tool names map to the same store used by REST. Where possible they already reuse REST or shared store calls:
- **Contracts:** `list_contracts`, `get_contract`, `get_contract_funding`, `get_open_contracts` (calls `/api/open-contracts`).
- **Tasks:** `list_tasks`, `get_task`, `claim_task`, `submit_work`, `get_task_proof`, `get_task_status`.
- **Skills:** `list_skills`.
- **Proposals:** `list_proposals`, `get_proposal`, `create_proposal`, `approve_proposal`, `publish_proposal`.
- **Submissions:** `get_submission`, `review_submission`, `rework_submission` (note: `list_submissions` currently placeholder, see backlog).
- **Events:** `list_events` (in-memory buffer).
- **Scanning:** `scan_image`, `scan_block`, `extract_message`, `get_scanner_info` (routes into steganography scanners).

### Auxiliary endpoints (still live)
- `/api/open-contracts` is the human-wish ingress; MCP tool `get_open_contracts` proxies it so agents can turn wishes into executable smart-contract proposals.
- `/api/contract-stego/*`, `/api/smart-contracts` remain for compatibility with existing UI code.
- `/api/health`, `/metrics`, `/api/docs` (Swagger for general backend; MCP-specific OpenAPI planned).
- `/api/docs/mcp/openapi.json` serves the MCP/Smart Contract surface (stub, keep updated).
- `/api/smart_contract/discover` and `/mcp/discover` advertise base URLs, endpoints, tools, and auth expectations.
- `/api/auth/register` and `/api/auth/login` issue/validate API keys (persisted in Postgres when `STARGATE_PG_DSN` is set, otherwise memory; seed key from `STARGATE_API_KEY`).

## Data Shapes (canonical)
- **Contract:** `contract_id`, `title`, `total_budget_sats`, `goals_count`, `available_tasks_count`, `status`.
- **Task:** `task_id`, `contract_id`, `goal_id`, `title`, `description`, `budget_sats`, `skills`, `status`, `claimed_by`, `claim_expires_at`, `merkle_proof`.
- **MerkleProof / Funding:** `tx_id`, `block_height`, `block_header_merkle_root`, `proof_path[]`, `funded_amount_sats`, `confirmation_status`, `visible_pixel_hash`, `funding_address`, timestamps.
- **Claim:** `claim_id`, `task_id`, `ai_identifier`, `status`, `expires_at`, `created_at`.
- **Submission:** `submission_id`, `claim_id`, `status`, `deliverables` (flexible JSON), `completion_proof`, timestamps.
- **Proposal:** `id`, `title`, `description_md`, `visible_pixel_hash`, `budget_sats`, `status`, `tasks[]`, `metadata`.
- **Event:** `type`, `entity_id`, `actor`, `message`, `created_at`.

## Current Workflows (happy path)
1) **Discovery:** UI/agent hits `GET /api/smart_contract/tasks` or MCP tool `list_tasks`; optional skill/budget filters.  
2) **Claim:** `POST /api/smart_contract/tasks/{id}/claim` (or tool `claim_task`) -> claim record with expiry, SSE `claim` event.  
3) **Submit:** `POST /api/smart_contract/claims/{claim_id}/submit` (or tool `submit_work`) -> submission stored + `submit` event.  
4) **Review:** Reviewer calls `POST /api/smart_contract/submissions/{id}/review` with `approve|reject|review` -> status change + event.  
5) **Publish/Settlement:** `POST /api/smart_contract/proposals/{id}/approve|publish` to move proposal + tasks; funding proofs refreshed by background sync when enabled.

## Parity Plan (HTTP MCP ↔ REST)
| MCP tool | REST backing | Status |
| --- | --- | --- |
| list_contracts / get_contract / get_contract_funding | `GET /api/smart_contract/contracts*` | DONE |
| get_open_contracts | `GET /api/open-contracts` (human wish → AI proposal bridge) | DONE (proxy) |
| list_tasks / get_task / get_task_status / get_task_proof | `GET /api/smart_contract/tasks*` | DONE |
| claim_task | `POST /api/smart_contract/tasks/{id}/claim` | DONE |
| submit_work | `POST /api/smart_contract/claims/{claim_id}/submit` | DONE |
| list_skills | `GET /api/smart_contract/skills` | DONE |
| list_proposals / get_proposal / approve_proposal / publish_proposal / create_proposal | `/api/smart_contract/proposals*` | DONE |
| get_submission / review_submission / rework_submission | `/api/smart_contract/submissions*` | DONE |
| list_submissions | `/api/smart_contract/submissions` | **PARTIAL** (placeholder response) |
| list_events | `/api/smart_contract/events` | DONE |
| scan_image / scan_block / extract_message / get_scanner_info | Stego scanner services | DONE |

## Human Wish → Smart Contract Flow (inscriptions & visible pixel hash)
- **Ingress:** `/api/open-contracts` exposes human wishes/briefs; MCP tool `get_open_contracts` mirrors it so agents can draft proposals from raw intent.
- **Visible Pixel Hash (VPH):** Every inscription-derived wish yields a `visible_pixel_hash` that becomes the contract identifier once a proposal is created. Tasks and funding/Merkle proofs inherit this VPH as the stable key linking inscription → proposal → contract → tasks → payouts. Treat VPH as canonical for cross-component joins.
- **Ingestion → Proposal:** `POST /api/smart_contract/proposals` with `ingestion_id` converts a pending inscription into a proposal, auto-derives tasks from embedded message, and stamps VPH plus funding address. Approval/publish flows upsert contracts/tasks so both REST and MCP tools surface the same objects.

## Backlog (next steps, ordered)
1) **Hard-route MCP to REST:** Keep single `/mcp` namespace; have the tool bridge call the HTTP REST endpoints first (with store fallback for in-process CLI mode).  
2) **Complete `list_submissions` tool:** Wire to `/api/smart_contract/submissions` with filters and pagination.  
3) **OpenAPI & discovery:** (In progress) MCP OpenAPI available at `/api/docs/mcp/openapi.json`; add `/api/smart_contract/discover` plus `/mcp/discover` for capability advertisement.  
4) **Pagination + sorting consistency:** Ensure list endpoints honor `limit/offset/sort` across contracts/tasks/proposals/submissions; surface defaults in responses.  
5) **Auth alignment:** Allow the same API key / bearer token flow across `/api` and `/mcp`; document required headers in one place.  
6) **Rate limiting & abuse controls:** Apply middleware to claim/submit/review endpoints (shared between REST and MCP tools).  
7) **Indexer fidelity:** Replace mock funding provider defaults with production provider configs (per env), and persist block header + Merkle path provenance on every proof update.  
8) **Event delivery:** Expose SSE on `/mcp/events` as alias to `/api/smart_contract/events`; add minimal retry guidance in responses.  
9) **Reputation/agent profile (optional, later):** If/when needed, reintroduce agent metadata endpoints building on submission history; not required for parity.

## How to Use (current)
- **UI:** Prefer `/api/smart_contract/*` for contracts, tasks, claims, proposals, submissions, events.  
- **Agents (MCP clients):** Call `/mcp/tools` to enumerate tools, then POST `/mcp/call` with tool names above; base URL matches backend port (`STARGATE_HTTP_PORT`, default `3001`).  
- **Testing locally:** `MCP_SEED_FIXTURES=true go run stargate_backend.go` then hit `http://localhost:3001/api/smart_contract/tasks`.

## Ownership & Change Control
- Code: `backend/mcp`, `backend/middleware/smart_contract`, `backend/stargate_backend.go`.
- This document is the single canonical plan/spec. Update here first when adding or changing endpoints; keep `/mcp` tools and `/api/smart_contract` routes in lockstep.
