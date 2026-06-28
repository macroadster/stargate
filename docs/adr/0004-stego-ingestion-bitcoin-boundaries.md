# ADR 0004: Stego / ingestion / bitcoin / contract domain boundaries

- **Status:** Accepted
- **Date:** 2026-06-28
- **Deciders:** Stargate maintainers
- **Tags:** domain, stego, bitcoin, ingestion

## Context

Funding confirmation and product replication required reading four areas at once (block monitor, ingestion service, stego publish/reconcile, contract store), which slowed safe changes and invited circular imports.

## Decision

**Keep clear ownership and collaborate only through defined seams** (see [DOMAIN_SEAMS.md](../arch/DOMAIN_SEAMS.md) and `app/smart_contract/ports.go`).

| Domain | Owns | Must not |
| --- | --- | --- |
| **bitcoin** (`bitcoin/block_monitor_*`, PSBT builders) | Chain scan, script/witness matching, PSBT construction, confirm task proofs | Decode stego manifests; create proposals from payloads |
| **ingestion** (`IngestionService`, ingestion sync) | Pending images + metadata (VPH, funding txids) | Own on-chain parsing |
| **stego** (`stego/*`) | Manifest/payload codec, alpha/LSB helpers, sandbox tarball format | Import `app/smart_contract` |
| **contracts app** (`app/smart_contract`) | Orchestration: publish artifacts around PSBT, reconcile → upsert proposal/contract/tasks | Embed chain parsers |

**Seams**

1. **PSBT → StegoPublishPort** — `PreparePublishArtifacts` before PSBT; `FinalizePublishArtifacts` after (async IPFS/pubsub optional)
2. **Block monitor → StegoReconciler** — on match, `ReconcileStego(cid, expectedHash)` only (injected implementation on `Server`)
3. **Reconcile → ContractFromStegoPort** — `UpsertContractFromStegoPayload(manifest, payload)`
4. **Identity** — all domains resolve IDs via `core/identity` / VPH (ADR 0003)

Dependency direction: `bitcoin` injects reconciler; `app` uses `stego` + `storage`; `stego` / `core` never import `app`.

## Consequences

**Positive**

- Changes to chain matching stay in `bitcoin`
- Codec changes stay in `stego`
- App layer remains the only writer of proposals from product payloads

**Negative / trade-offs**

- Extra interfaces and wiring in `main` / `stargate_backend.go`
- Large legacy functions remain inside packages but must respect seams for *new* coupling

## Related

- ADR 0003 — pixel-hash identity
- `app/smart_contract/ports.go`, `bitcoin.StegoReconciler`
