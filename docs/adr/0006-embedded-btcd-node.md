# ADR 0006: Embedded btcd full node (no mining)

- **Status:** Accepted
- **Date:** 2026-07-19
- **Deciders:** Stargate maintainers
- **Tags:** bitcoin, chain, reliability, deploy

## Context

Stargate monitored the chain by polling public Esplora APIs (`mempool.space` / `blockstream.info`). Rate limits, partial responses, and outages made tip tracking and raw block download unreliable — blocks and inscriptions were missed.

Requirements:

- Complete, local raw block data for the monitor and stego pipeline
- No dependency on public explorer rate limits for the happy path
- Stay within the single-container deploy model (ADR 0001)
- No mining / block generation

## Decision

**Run btcd as a managed full node (or connect to an external one) and source all chain data via JSON-RPC.**

| Mode (`BTCD_MODE`) | Behavior |
| --- | --- |
| `managed` (default) | Stargate spawns bundled `btcd` (no `--generate`), localhost RPC |
| `external` | Connect to operator-provided `BTCD_RPC_HOST` |
| `off` | Esplora HTTP fallback for tests/dev only |

- Enable **`--txindex`** and **`--addrindex`** (no prune) so historical raw txs and address UTXOs work
- Default network remains **testnet4**; mainnet requires `BTCD_ALLOW_MAINNET=true`
- `ChainBackend` interface is the single seam for tip, raw blocks, UTXO, and broadcast
- Keep local `ParseBlock` for inscription/image extraction; only the hex source changes

btcd’s server is `package main`, so “embedded” means **managed child process in the same container**, not an in-process library server — same operational unit as ADR 0001.

## Consequences

**Positive**

- Reliable complete block bytes from a validating full node
- P2P sync independent of third-party APIs
- PSBT/sweep can use local `sendrawtransaction` / addrindex UTXOs

**Negative / trade-offs**

- Chainstate disk (testnet4 multi-GB with indexes; mainnet much larger)
- First IBD latency before tip tracking is useful
- Must ship `btcd` binary and persist `BTCD_DATADIR` across restarts
- Local “synced” can still lag the public network (e.g. testnet future-timestamp rejects); mitigated by external tip lag **and tip-hash** checks (`CHAIN_*` env, see AGENTS.md) and optional managed restart on height lag only
- P2P defaults to localhost listen (no public inbound). Pin trusted peers with `BTCD_CONNECT` / `BTCD_ADDPEER`. Settlement waits `CHAIN_SETTLEMENT_CONFIRMATIONS` and is blocked on explorer hash mismatch so a minority fork cannot confirm contracts. Explorer hashes are never submitted and never used to invalidateblock.

## Related

- ADR 0001 — single-binary / single-container deploy
- ADR 0004 — bitcoin domain boundaries
- `backend/bitcoin/btcd_node.go`, `btcd_backend.go`, `chain_backend.go`, `tip_lag.go`
