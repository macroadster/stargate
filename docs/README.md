# Stargate documentation map

Status: **canonical**

This directory is for developers and operators. End-user manuals live in the
embedded UI. Agent workflow lives on the running node.

| Audience | Where | What |
| --- | --- | --- |
| Humans using the UI | `frontend/public/docs/` (served at `/docs`) | Wish, review, pay, deploy a node |
| Agents | `/mcp/SKILL.md`, `/mcp/docs`, `/mcp/tools` on a live node | Claim / submit / PSBT tools |
| Why the system is shaped this way | [`adr/`](./adr/) | Accepted ADRs |
| How it works now | [`arch/`](./arch/) | Seams, packages, contracts, scanner, env |
| Attic | [`history/`](./history/) | Plans, reports, superseded specs |

Do **not** treat `docs/history/` or `backend/docs/*.md` as current API.

## Status stamps

Every living file under `docs/arch/` and `docs/adr/` should open with a status
line:

- **canonical** — source of truth; update when the code changes
- **implemented** — describes live behavior (may still grow)
- **proposal** — not built; do not code from it
- **historical** — attic only; kept for scars

## Start here

1. [adr/README.md](./adr/README.md) — decisions
2. [arch/DOMAIN_SEAMS.md](./arch/DOMAIN_SEAMS.md) — stego / bitcoin / confirm / extract
3. [arch/starlight_contracts.md](./arch/starlight_contracts.md) — **§12 is implemented**; §§1–11 are historical script sketches
4. [arch/ENV.md](./arch/ENV.md) — `STARGATE_*` / `BTCD_*` / `STARLIGHT_*`
5. [arch/MCP_UNIFIED_PLAN.md](./arch/MCP_UNIFIED_PLAN.md) — REST vs MCP surfaces
6. [arch/TRIN_STARLIGHT_SCANNER.md](./arch/TRIN_STARLIGHT_SCANNER.md) — in-process GGUF scan path

Live route catalog: `GET /api/surfaces` on a running node.
Retired HTTP paths: [arch/LEGACY_RETIREMENT.md](./arch/LEGACY_RETIREMENT.md).
