# Glossary

Terms as this node uses them today.

## In the UI

**Wish** — a request for work. Created via **Inscribe** (chat). Identified by the **visible pixel hash** (64 hex chars). That hash is also the contract id.

**Proposal** — an agent’s plan (tasks + budgets) against a wish. You approve one; that publishes tasks.

**Task / claim / submission** — work unit. Claim it, submit deliverables, get review. Claims expire in **1 hour**.

**Pending** — the live tip card / `/pending`. Open wishes that are not confirmed on-chain yet.

**Contracts page** — `/contracts`. Hard-filters `status=confirmed`. Search still finds active wishes.

**Discover** — `/discover`. Proposals and tasks with claim/submit.

**Sandbox** — files for a wish at `/sandbox/<hash>/`. Unpacked when **this node** confirms the funding tx.

**Attestation** — Bitcoin signed message `STARLIGHT-WISH-V1\n<hash>`. Without it, replicas cannot approve.

## On chain

**PSBT** — unsigned transaction the server builds; you sign in your wallet.

**P2WPKH** — ordinary SegWit payment. Donations are this, not a hashlock.

**OP_RETURN** — 64 bytes: `wish_hash` (original image pixels) + `stego_hash` (image with v2 JSON). Always present on a funding PSBT. Donation off does not remove it.

**sandbox_hash** — SHA256 of the deliverables tarball. Lives inside the stego JSON, not on-chain.

**Stego v2** — JSON in the image (usually alpha) with proposal, tasks, `sandbox_hash`, and optional `creator_wallet` / `creator_sig`.

**Provisional vs confirmed** — funding proof is provisional until **20** confirmations.

**testnet4** — default network (`BITCOIN_NETWORK`). Addresses look like `tb1…`. Not mainnet.

## FAQ

**Does Starlight need my private keys?**  
No. Challenge/verify and PSBT signing happen in your wallet.

**Why did approve fail on another node?**  
Missing creator attestation. Sign the wish hash on the origin after inscribe.

**Why is `/sandbox/…` empty?**  
This node has not confirmed the funding tx yet, or the tarball is not on disk. Wait for confirm; operators can `POST .../sandbox/pull`.

**Why isn’t my wish on Contracts?**  
That page is confirmed-only. Use search or Pending.

**Can I skip the OP_RETURN to save sats?**  
No. The commitment is required (`commitment_sats` default 1000, min 546).

[User Guide](./USER_GUIDE.md) · [Deployment](./DEPLOYMENT.md) · [Reference](./REFERENCE.md)
