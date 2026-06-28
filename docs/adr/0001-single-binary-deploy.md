# ADR 0001: Single-binary deploy model

- **Status:** Accepted
- **Date:** 2026-06-28
- **Deciders:** Stargate maintainers
- **Tags:** deploy, packaging, operations

## Context

Stargate historically shipped separate backend and frontend images/processes (`stargate-backend`, `stargate-frontend`). That increased operational surface (two rollouts, CORS/proxy coupling, version skew) and complicated agent-oriented single-node demos.

Requirements:

- One artifact for local, CI, Helm, and GitHub Releases (macOS/Linux)
- Embed the React UI and serve it from the Go process
- Keep optional sidecars (IPFS, scanners) out of the critical path when disabled

## Decision

**Ship Stargate as a single Go binary (and one Docker image `stargate`) that embeds the frontend assets and owns HTTP for UI + API + MCP.**

- Build: `make docker` / release workflows produce `stargate` with embedded `assets/frontend`
- Runtime: one listen port (`STARGATE_HTTP_PORT`, default `3001`) serves `/`, `/api/*`, `/mcp/*`, `/bitcoin/v1/*`, static uploads
- Split images (`make backend` / `make frontend` / `*-legacy`) are **retired** (fail with message to use `make docker` / `make single-binary`; see stargate-3bk.8)
- Helm / k8s: one container per pod for the stargate stack (see Agents.md deployment workflow)

## Consequences

**Positive**

- Atomic versioning: UI and API always match
- Simpler install (`curl | bash` / single binary download)
- Fewer network hops and CORS issues in local/dev

**Negative / trade-offs**

- Frontend rebuild is tied into Go embed / Docker build
- Process restart required for UI changes in production images
- Very large binary vs pure API server

**Follow-ups**

- Prefer feature flags/env for optional agents and IPFS rather than extra containers unless isolation is required

## Related

- Agents.md — Deployment Workflow
- ADR 0005 — REST vs MCP on the same process
