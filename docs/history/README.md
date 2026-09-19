# Historical docs (attic)

Status: **historical**

Plans, E2E reports, pentest notes, and superseded specs. Do not implement from
these files. Living docs: [../README.md](../README.md).

Notable scars parked here:

- `zero_cost_funding_plan.md` — argued for skipping the OP_RETURN commitment. **Rejected.** Live PSBTs always emit `wish_hash || stego_hash` (`commitment_sats` default 1000).
- `ENGINEERING_ROADMAP.md` — 2025 GPS/delivery marketplace sketch, not the product.
- `UX_IMPROVEMENT_SPEC.md` — Postgres-first rail/pagination plan; SQLite is the default (ADR 0002).
- `DOCKER_OPTIMIZATION_SUMMARY.md` — split `stargate-frontend` / `stargate-backend` images. Retired (ADR 0001); use `make docker`.
- `PHASE1_IMPLEMENTATION_SUMMARY.md`, `CONTENT_ENDPOINT_SPEC.md` — `/mcp/v1` and proposed `/content` routes; not the live surface.
- `MCP_IMPROVEMENT_PLAN.md`, `stargate_mcp_server_plan.md`, `starlight_mcp.md` — superseded by `docs/arch/MCP_UNIFIED_PLAN.md`.
- `*_e2e*.sh` / `mcp_*.sh` — old cluster probes. Require env-supplied keys; do not run against a live node.
