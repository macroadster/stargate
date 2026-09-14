# ADR 0007: Wish creator attestation

- **Status:** Accepted
- **Date:** 2026-09-14
- **Deciders:** Stargate maintainers
- **Tags:** authz, stego, replication, security

## Context

After irl.1 / irl.2, submission and proposal approval fail closed unless the node has a `creator_wallet` for the wish. Only the local create path writes that field. Replicas therefore cannot approve: the stego payload is attacker-controlled, `manifest.Issuer` is a node label (`STARGATE_STEGO_ISSUER` / hostname), and OP_RETURN only binds `wish_hash || stego_hash`. Copying `creator_wallet` from the payload, even fill-if-absent, lets a crafted replica overwrite the origin or win first-writer on a fresh peer (`stargate-6ds`).

## Decision

**A wish creator attests authorship with a Bitcoin signed message over the wish hash. The attestation travels as first-class stego fields, not `payload.Metadata`. Replicas write `creator_wallet` only after verification, and never overwrite.**

| Choice | Decision |
| --- | --- |
| Key | The same Bitcoin wallet used for challenge/verify (the API-key binding) |
| Message | `STARLIGHT-WISH-V1\n<visible_pixel_hash>` |
| Carrier | `creator_wallet` + `creator_sig` on `stego.Payload` / `Manifest` |
| When written | Origin: `POST /api/inscriptions/{id}/attest`. Replica: chain-apply `ensureStegoIngestion` after hash check |
| Write rule | `SetCreatorWalletIfAbsent` — verify first, never overwrite |
| Missing sig | Fail closed (same as today). `STARLIGHT_DONATION_ADDRESS` stays this node's settlement wallet, not a network-wide auditor |

Do **not** put these fields in `stegoPayloadMetadataAllowlist`. Do **not** trust `Issuer`. Do **not** relax fail-closed.

## Consequences

**Positive**

- A replica that applies the on-chain `stego_hash` can establish the creator and approve
- Origin `creator_wallet` cannot be stolen via `UpdateFromIngest` incoming-wins
- Unsigned / legacy images remain valid and remain origin-only

**Negative / trade-offs**

- New wishes need a client signature after the hash is known (hash is computed at inscribe time)
- A competing inscription of the same visible hash with the attacker's own valid signature can still become creator on a peer that only sees that inscription (contract id = visible hash). Out of scope for 6ds.

## Related

- `stargate-6ds`, `stargate-irl.1`, `stargate-irl.2`
- [0003](./0003-lifecycle-pixel-hash-identity.md), [0004](./0004-stego-ingestion-bitcoin-boundaries.md)
