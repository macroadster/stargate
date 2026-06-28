# Architecture Decision Records (ADRs)

Status: **canonical** (stargate-3bk.5)  
Agents and humans should treat this directory as the source of truth for *why* the system is shaped as it is. Implementation detail lives in `docs/arch/` and code.

## Index

| ADR | Title | Status |
| --- | --- | --- |
| [0001](./0001-single-binary-deploy.md) | Single-binary deploy model | Accepted |
| [0002](./0002-storage-dialect-sqlite-primary.md) | Storage dialect (SQLite primary) | Accepted |
| [0003](./0003-lifecycle-pixel-hash-identity.md) | Proposal / wish / contract lifecycle + pixel-hash identity | Accepted |
| [0004](./0004-stego-ingestion-bitcoin-boundaries.md) | Stego / ingestion / bitcoin / contract boundaries | Accepted |
| [0005](./0005-rest-vs-mcp-ownership.md) | REST vs MCP ownership | Accepted |

## Format

Each ADR uses a short MADR-inspired template: **Context → Decision → Consequences → Related**. Superseding an ADR means a new numbered file and an update to this index.

## Related living docs

- Package layers: [../arch/PACKAGE_BOUNDARIES.md](../arch/PACKAGE_BOUNDARIES.md)
- Domain seams: [../arch/DOMAIN_SEAMS.md](../arch/DOMAIN_SEAMS.md)
- API surfaces: [../arch/MCP_UNIFIED_PLAN.md](../arch/MCP_UNIFIED_PLAN.md), `GET /api/surfaces`
