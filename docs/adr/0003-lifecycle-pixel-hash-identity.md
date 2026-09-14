# ADR 0003: Proposal / wish / contract lifecycle + pixel-hash identity

- **Status:** Accepted
- **Date:** 2026-06-28
- **Deciders:** Stargate maintainers
- **Tags:** domain, identity, smart-contract

## Context

Starlight maps human wishes (inscriptions / stego images) to AI proposals and on-chain funding. Objects were inconsistently keyed by ingestion IDs, proposal IDs, contract IDs, and hashes, causing UI/agent drift and reconcile bugs.

## Decision

### Identity

**The 64-character hex *visible pixel hash* (VPH) is the stable join key and the stored contract primary key.** Open vs confirmed is `status`, not an ID rename. `wish-<vph>` is accepted on input as a lookup alias only (`core/identity.CandidateIDs`).

| Object | Canonical ID / fields |
| --- | --- |
| Wish / contract | `ContractID = <vph>` via `core/identity.CanonicalContractID` |
| Proposal | `id` (opaque) + required `VisiblePixelHash` / metadata |
| Ingestion | record id and/or `metadata.visible_pixel_hash` |
| Stego manifest | `visible_pixel_hash`, `proposal_id` |
| Task funding proof | `VisiblePixelHash` (wish/commitment), optional `ProductPixelHash` (delivery stego) |

Helpers: package **`stargate-backend/core/identity`** (`CanonicalContractID`, `CandidateIDs`, `Normalize`, `IsPixelHash`). Prefer them over ad-hoc `wish-` string rules. `ToWishID` remains for alias generation.

### Lifecycle (happy path)

1. **Wish ingress** — human image / inscription → ingestion + wish-shaped contract (`/api/open-contracts`, block monitor, or create-wish flows)
2. **Proposal** — agents create proposals tied to VPH (`POST /api/smart_contract/proposals` or MCP `create_proposal`); tasks may derive from markdown
3. **Approve / publish** — creator (or policy) approves → tasks upserted to store; wish may be archived
4. **Fund** — payer builds PSBT (`/contracts/{id}/psbt`); commitment uses wish hash; product stego prepared around PSBT (see ADR 0004)
5. **Work** — claim task → submit deliverables → review
6. **Confirm** — block monitor matches chain data to ingestions/proofs; status moves toward confirmed/completed

Statuses are stringly typed in storage today (`pending`, `approved`, `published`, `active`, `funded`, `confirmed`, …); new code should not invent parallel ID schemes.

## Consequences

**Positive**

- One join key for UI, MCP, reconcile, and stego
- Peers can rehydrate contracts from stego manifests using VPH + proposal_id

**Negative / trade-offs**

- Legacy rows may still use `wish-<vph>`; resolvers must try `identity.CandidateIDs` and pick the live row
- A later publish/confirm that finds only `wish-<vph>` adopts that row onto the bare PK
- Renaming VPH after creation is unsupported (identity immutable)

## Related

- [DOMAIN_SEAMS.md](../arch/DOMAIN_SEAMS.md)
- ADR 0004 — stego/ingestion/bitcoin boundaries
