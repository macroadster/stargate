# ADR 0008: Extract sandbox on this node's on-chain confirm

- **Status:** Accepted
- **Date:** 2026-09-15
- **Deciders:** Stargate maintainers
- **Tags:** domain, sandbox, bitcoin, replication
- **Amends:** [0004](./0004-stego-ingestion-bitcoin-boundaries.md)

## Context

osv.4 left `/sandbox/<hash>` empty until an explicit `POST .../sandbox/pull`.
Peers that only watched the chain never unpacked the tarball. Gossip and first-pass
stego reconcile were the wrong doors: they run before (or without) this node's
`ConfirmContract`, and they invited extract on unconfirmed or replica-gossiped
metadata.

The living seam list in [DOMAIN_SEAMS.md](../arch/DOMAIN_SEAMS.md) already named
`SandboxExtractor`. ADR 0004 still stopped at `StegoReconciler`.

## Decision

**This node's on-chain `ConfirmContract` unpacks the sandbox tarball.**

| Path | May extract? |
| --- | --- |
| `maybeConfirmContract` / `promoteFundedContracts` / `markIngestionConfirmed` / `ensureMatchedContract` when `scanMayConfirm` | **Yes** — after `ConfirmContract` succeeds, call `SandboxExtractor.ExtractSandbox` → `DownloadSandboxArtifacts` |
| Stego reconcile (`ReconcileStego`) | **No** — metadata only (proposal/tasks/`sandbox_hash`) |
| Gossip / `processEvent` / IPFS pubsub | **No** |
| Unconfirmed / catch-up that does not confirm | **No** |
| `POST .../sandbox/pull` | Manual retry only if confirm-time extract missed |

`bitcoin` injects `SandboxExtractor` the same way it injects `StegoReconciler`.
Implementation stays on `app/smart_contract.Server`. `bitcoin` must not unpack
tarballs itself.

## Consequences

**Positive**

- A node that confirms the funding tx serves `/sandbox/<vph>/` without a second human/API step
- Gossip and stego cannot race extract ahead of confirm
- Pull remains a recovery hatch, not the replica door

**Negative / trade-offs**

- Confirm now depends on the tarball being on disk (IPFS mirror / origin write). Missing file is retried on later confirm/pull, not treated as a confirm failure of the chain match
- ADR 0004 seam list must stay in lockstep with DOMAIN_SEAMS

## Related

- `stargate-osv.5`, `stargate-osv.1`–`osv.4`
- [0004](./0004-stego-ingestion-bitcoin-boundaries.md), [DOMAIN_SEAMS.md](../arch/DOMAIN_SEAMS.md)
- `bitcoin.SandboxExtractor`, `app/smart_contract/ports.go`
