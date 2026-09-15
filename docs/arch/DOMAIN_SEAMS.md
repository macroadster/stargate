# Domain seams: stego · ingestion · bitcoin · contracts

Status: **canonical** (stargate-3bk.7)  
Related: [PACKAGE_BOUNDARIES.md](./PACKAGE_BOUNDARIES.md)

## Problem

Funding confirmation and wish→proposal→product flows historically required reading four areas at once:

| Domain | Typical code | Responsibility |
| --- | --- | --- |
| **Bitcoin / blocks** | `bitcoin/block_monitor_*` | Scan chain, match outputs/witnesses to ingestions, confirm tasks |
| **Ingestion** | `services.IngestionService`, `app/.../ingestion_sync.go` | Pending wish images + metadata (pixel hash, funding txids) |
| **Stego** | `stego/*`, `app/.../stego_publish.go`, `stego_reconcile.go` | Encode/decode manifests & payloads; IPFS/pubsub optional |
| **Contracts** | `storage/smart_contract`, `app/.../services` | Proposals, tasks, merkle proofs, PSBT linkage |

## Identity join key

**Visible pixel hash** (64-char hex) is the stable join key.

| Object | How it stores the key |
| --- | --- |
| Wish / open contract | `ContractID = <hash>` (`core/identity.CanonicalContractID`; `wish-<hash>` is a lookup alias) |
| Proposal | `VisiblePixelHash` + `metadata.visible_pixel_hash` |
| Ingestion | record ID and/or `metadata.visible_pixel_hash` |
| Stego manifest | `visible_pixel_hash`, `proposal_id` |
| Task proof | `VisiblePixelHash` (wish), `ProductPixelHash` (stego/product) |

Helpers: **`stargate-backend/core/identity`** (`CanonicalContractID`, `CandidateIDs`, `ToWishID`, `IsPixelHash`). Prefer these over ad-hoc `wish-` string rules.

## Seams (interfaces)

Defined in `app/smart_contract/ports.go` and `bitcoin.StegoReconciler` / `bitcoin.SandboxExtractor`:

1. **PSBT → Stego publish** (`StegoPublishPort`): `PreparePublishArtifacts` before build; `FinalizePublishArtifacts` after (async IPFS/pubsub). Block monitor never calls publish.
2. **Block monitor → Stego reconcile** (`bitcoin.StegoReconciler` / `StegoReconcilePort`): on funding match, `ReconcileStego(cid, expectedHash)` only — no direct store writes for product payload in bitcoin package. Metadata only; does not unpack the sandbox.
3. **Block monitor → Sandbox extract** (`bitcoin.SandboxExtractor` / `SandboxExtractPort`): after this node's on-chain `ConfirmContract` succeeds, `ExtractSandbox(contractID)` → `DownloadSandboxArtifacts`. Gossip, `processEvent`, and stego reconcile must not extract. `POST .../sandbox/pull` stays as a manual retry.
4. **Stego reconcile → Store** (`ContractFromStegoPort`): `UpsertContractFromStegoPayload` maps manifest+payload → proposal/contract/tasks.
5. **Block monitor → Ingestion**: match tx scripts/witnesses to ingestion candidates (`ingestionCandidateBuckets`); update proofs / ensure contract row via `sweepStore` / confirm APIs.
6. **Ingestion sync → Proposals**: pending ingestions may create proposals (`ingestion_sync`); uses `identity.CandidateIDs` for contract existence checks.

## Dependency direction

```
bitcoin ──injects──► StegoReconciler (implemented by app/smart_contract.Server)
bitcoin ──injects──► SandboxExtractor (implemented by app/smart_contract.Server)
bitcoin ──uses────► IngestionService (read/match)
app/smart_contract ──uses──► stego (encode/decode), storage, identity
handlers/mcp ──uses──► app/smart_contract, storage
```

Do **not** import `app/smart_contract` from `stego` or `core`. Do **not** implement stego decode inside `bitcoin`.

## Change checklist

When touching confirmation or publish flows:

- [ ] Resolve IDs via `core/identity`
- [ ] Keep chain matching in `bitcoin/*`
- [ ] Keep manifest/payload codec in `stego/*`
- [ ] Keep proposal/task upsert in `app/smart_contract` (ports)
- [ ] Wire new collaborations through interfaces in `ports.go` / `StegoReconciler` / `SandboxExtractor`

## ADRs

Decision records: [../adr/README.md](../adr/README.md) (especially ADR 0003–0004).
