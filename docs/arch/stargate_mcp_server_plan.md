# Stargate MCP Server — Build Plan & MVP Spec

## Why & Alignment
- Implements the coordination layer from `ENGINEERING_ROADMAP.md`: task registry, claim coordination, submission intake, and Merkle-proof surfacing for Starlight evidence flows.
- Serves Model Context Protocol (MCP) clients so AIs can discover, verify, claim, and submit tasks without trusting the server (clients must re-verify on-chain).
- Ships as a standalone service (own binary + Docker image) and is deployed via `starlight-helm`.

## Scope (MVP)
- **Read/query**: list/query tasks & contracts; fetch task details; fetch Merkle/claim/payout proofs.
- **Coordination**: claim task (with expiry), submit work, get status.
- **Verification hints**: surface raw tx, block headers, Merkle paths; never act as final authority.
- **Observability**: health + metrics endpoints.
- **Storage**: start with in-memory seed data; later plug Postgres/Redis and Bitcoin indexer.

## Interfaces (HTTP, JSON)
- Health: `GET /healthz` → `{status:"ok"}`
- Discovery:
  - `GET /mcp/v1/contracts?status=&min_budget_sats=&skills=` → summary list
  - `GET /mcp/v1/tasks?skills=&max_difficulty=&min_budget_sats=&limit=&offset=` → available tasks
  - `GET /mcp/v1/tasks/{task_id}` → full task (includes funding state + merkle stub)
  - `GET /mcp/v1/skills` → list of unique skills: `{ "count": 7, "skills": ["python", "risk", "monitoring"] }`
- Verification:
  - `GET /mcp/v1/tasks/{task_id}/merkle-proof` → payment proof object
  - `GET /mcp/v1/contracts/{contract_id}/funding` → funding proof(s)
- Coordination:
  - `POST /mcp/v1/tasks/{task_id}/claim`
    - Request: `{ "ai_identifier": "string", "estimated_completion": "string" }`
    - Response: `{ "claim_id": "string", "expires_at": "string", "message": "string", "success": true }`
  - `POST /mcp/v1/claims/{claim_id}/submit`
    - Request: `{ "deliverables": { ... }, "completion_proof": { ... } }` (both are JSON objects)
    - Response: Full submission object including `submission_id`, `claim_id`, `status`, and echoed `deliverables` and `completion_proof`.
  - `GET /mcp/v1/tasks/{task_id}/status` → claim/submission state
- Observability:
  - `GET /metrics` (Prometheus), `GET /healthz`

## Data Model (MVP JSON shapes)
- **Contract**: `contract_id`, `title`, `total_budget_sats`, `goals_count`, `available_tasks_count`, `status`.
- **Task**: `task_id`, `contract_id`, `goal_id`, `title`, `description`, `budget_sats`, `skills_required`, `status`, `claimed_by`, `claim_expires_at`, `merkle_proof`.
- **Claim**: `claim_id`, `task_id`, `ai_identifier`, `status`, `expires_at`, `created_at`.
- **MerkleProof**: A detailed object proving existence and funding.
  - `tx_id`: (string) The Bitcoin transaction ID for funding.
  - `block_height`: (int) The block number containing the transaction.
  - `block_header_merkle_root`: (string) The Merkle root from the block header.
  - `proof_path`: (array) An array of objects, e.g., `[{ "hash": "string", "direction": "string" }]`, to reconstruct the Merkle path.
  - `visible_pixel_hash`: (string) The SHA256 hash of the Starlight evidence image.
  - `funded_amount_sats`: (int) The amount of Satoshis funded in the transaction.
  - `confirmation_status`: (string) e.g., "provisional" or "confirmed".
  - `seen_at`: (string) Timestamp when the proof was last seen/updated.
  - `confirmed_at`: (string, optional) Present if confirmed.
- **Submission**: `submission_id`, `claim_id`, `status`, `deliverables`, `completion_proof`, `created_at`.
- **Submission Payload (for `POST /mcp/v1/claims/{claim_id}/submit`)**:
  - `deliverables`: (object) A flexible JSON object containing the work products. Example: `{ "verified_payload_data": { ... }, "original_image_url": "..." }`.
  - `completion_proof`: (object) A flexible JSON object proving completion. Example: `{ "verified_starlight_payload_hash": "...", "tests_passed": 1 }`.

## Current Live Data (example)
- Skills endpoint now returns manual review as well: `["python","risk","monitoring","finance","testing","steganography-decoding","image-analysis","manual-review"]`.
- Task `stargate-task-001` carries:
  - `status: approved`, `claimed_by: gemini-cli-agent`
  - `merkle_proof.visible_pixel_hash: 20d442302752dcd094fe74a527dc5bc46311acef7a2e30ff915743017eff5e5e`
  - `merkle_proof.tx_id: 4a5e1e4baab89f3a32518a88c31bc87f618f76673e2cc77ab2127b7afdeda33b` (genesis stub)
  - `skills_required: ["steganography-decoding","image-analysis","manual-review"]`

## Component Plan
- **API layer**: net/http mux with request validation; JSON responses; deterministic errors.
- **State layer**: start with in-memory store + seed fixtures; interface that can swap to Postgres/Redis.
- **Coordination rules**: single active claim per task; expiry defaults to 72h; idempotent claim if same AI re-claims before expiry.
- **Verification adapter**: placeholder provider that returns canned proofs; interface to swap with Bitcoin indexer.
- **Metrics/health**: Prometheus handler + `/healthz`.

## Delivery Steps
1) **Skeleton service**: `cmd/mcpserver` main with routes above, wiring in-memory store + sample data.
2) **Config**: env-driven (`MCP_PORT`, `MCP_DEFAULT_CLAIM_TTL_HOURS`), sane defaults.
3) **Docker**: multi-stage build `Dockerfile.mcp` producing `stargate-mcp` binary; expose 3002.
4) **Helm**: add Deployment/Service for MCP, values for image/port/resources, ingress path/host wiring.
5) **Docs**: this plan/spec; mention how to build/run locally and via Helm.

## Definition of Done (MVP)
- `go build ./cmd/mcpserver` succeeds.
- Docker image builds via `docker build -f Dockerfile.mcp -t stargate-mcp:local .`.
- Helm chart renders and deploys MCP service reachable at `/mcp` (ingress) or service port.
- API responses match shapes above with placeholder data; safe to evolve behind versioned path `/mcp/v1`.

## Current Implementation Status
- Implemented `/mcp/v1` HTTP API with in-memory store + optional Postgres-backed store (config via `MCP_STORE_DRIVER`, `MCP_PG_DSN`).
- Added Dockerfile (`Dockerfile.mcp`) and Helm deployment with ingress routing and Prometheus scrape annotations.
- Added optional API key protection (`MCP_API_KEY`, header `X-API-Key`) and basic JSON validation on write endpoints.
- Seed fixtures load into memory or Postgres (`MCP_SEED_FIXTURES=true`).
