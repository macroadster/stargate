# IPFS Stego Oracle Reconciliation (YAML Manifest)

## Goal
When a proposal is approved, embed a compact YAML manifest into a stego image.
The stego image is inscribed on-chain. The `sha256(stego_image_bytes)` becomes
the canonical `contract_id`. The instance that creates the stego image publishes
it to IPFS. Other instances fetch the stego image from IPFS, verify that the
hash matches the on-chain script hash, and ingest the contract metadata into
their local Postgres.

## Design Principles
- Deterministic bytes: the stego image must be reproducible from the manifest.
- Minimal on-chain payload: include only compact metadata and CIDs for full data.
- Idempotent ingestion: contract insert keyed by `contract_id` (hash of stego).
- Verifiable: off-chain payloads are hash-addressed (IPFS CIDs).

## YAML Manifest (Embedded Payload)
Use YAML to keep the manifest compact while still human-readable. The manifest
is embedded as UTF-8 bytes inside the image's stego payload.

### Canonicalization
To keep the stego image deterministic, canonicalize YAML before embedding:
- Use a fixed field order.
- Use LF line endings.
- No trailing spaces.
- No anchors, aliases, or comments.
- No trailing newline at EOF.

### Fields
```yaml
schema_version: 1
contract_id: <sha256-stego-image-hex> # set after stego is produced
proposal_id: <proposal-id>
visible_pixel_hash: <hex>
payload_cid: <ipfs-cid> # full contract + proposal JSON/YAML
tasks_cid: <ipfs-cid|null> # optional, tasks/claims/submissions
created_at: <unix-seconds>
issuer: <instance-id|pubkey>
```

Notes:
- `payload_cid` should include the full contract/proposal metadata.
- `tasks_cid` can be omitted if tasks are not mirrored.
- `contract_id` is derived after the stego image is finalized; the manifest
  payload should be updated once to include it before inscription.

## Pipeline
### Approval → Stego Creation
1. User clicks Approve on proposal.
2. Build manifest with `contract_id` temporarily empty.
3. Canonicalize YAML, embed into image, produce stego image bytes.
4. Compute `contract_id = sha256(stego_image_bytes)`.
5. Update manifest with `contract_id`, re-embed, re-render stego image bytes.
6. Publish stego image to IPFS, capture `stego_cid`.
7. Inscribe stego image (PSBT publish), store `contract_id` on-chain.

### Reconciliation (Other Instances)
1. Fetch stego image by CID (published by the oracle instance).
2. Compute sha256 of stego bytes; ensure it equals on-chain script hash.
3. Extract YAML manifest from stego payload.
4. Fetch `payload_cid` (and `tasks_cid` if enabled).
5. Upsert contract/proposal into local Postgres keyed by `contract_id`.

## Storage & Idempotency
- Primary key: `contract_id`.
- Upsert with conflict resolution:
  - If existing record has same `contract_id`, ignore or update metadata.
  - Record `stego_cid`, `payload_cid`, and `tasks_cid` for later refresh.

## Data Size Strategy
- Keep manifest minimal.
- Store full data off-chain in IPFS:
  - `payload_cid`: contract + proposal metadata
  - `tasks_cid`: tasks/claims/submissions (optional)
- If tasks are too large, store Merkle root instead.

## Verification Requirements
- On-chain script hash matches `sha256(stego_image_bytes)`.
- `visible_pixel_hash` matches contract/proposal metadata.
- `payload_cid` must resolve and parse.

## Open Questions
- Which on-chain script hash is used for verification in the current flow?
- Do we need a multi-sig or oracle signature in the manifest?
- Should `issuer` be a pubkey or instance UUID?
