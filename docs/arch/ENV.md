# Operator environment variables

Status: **canonical**

Default network is **testnet4**. Mainnet needs `BTCD_ALLOW_MAINNET=true` (ADR 0006).
There is no `STARGATE_BITCOIN_NETWORK` — use `BITCOIN_NETWORK`.

Dump what this process will use: `stargate --config` (or the equivalent CLI
config print).

## Process / storage

| Variable | Default | Notes |
| --- | --- | --- |
| `STARGATE_HTTP_PORT` | `3001` | UI + `/api` + `/mcp` + `/bitcoin/v1` |
| `STARGATE_TUI` | `auto` | Status console when stdout is a terminal. `off` keeps logs. See [CONSOLE.md](./CONSOLE.md) |
| `STARGATE_TUI_VIEW` | `welcome` | `welcome` \| `monitor` \| `logs` |
| `STARGATE_TUI_COLOR` | on | `off` disables color. `NO_COLOR` does too |
| `STARGATE_TUI_ASCII` | off | `1` uses ASCII borders |
| `STARGATE_STORAGE` | `sqlite` | `sqlite` \| `postgres` \| `memory` (ADR 0002) |
| `STARGATE_DATA_DIR` | `data` | Root for sqlite, models, identity |
| `STARGATE_PG_DSN` / `DATABASE_URL` | unset | Required when `STARGATE_STORAGE=postgres` |
| `UPLOADS_DIR` | `$STARGATE_DATA_DIR/uploads` | Hash-named artifacts |
| `BLOCKS_DIR` | `$STARGATE_DATA_DIR/blocks` | Local block cache |
| `STARGATE_MCP_DB` | `$STARGATE_DATA_DIR/sqlite/mcp.db` | |
| `STARGATE_API_KEYS_DB` | `$STARGATE_DATA_DIR/sqlite/api_keys.db` | |
| `STARGATE_INGESTIONS_DB` | `$STARGATE_DATA_DIR/sqlite/ingestions.db` | |
| `STARGATE_DEFAULT_CLAIM_TTL_HOURS` | `1` | Not 72. Override only if you mean it |
| `STARGATE_SEED_FIXTURES` | `false` | Dev/test seed data |
| `STARGATE_METRICS` | `false` | `/metrics` |
| `STARGATE_PPROF` | `false` | |
| `STARGATE_ACTION_RATE_LIMIT` | on | `off` disables claim/submit limiter |
| `STARGATE_ENABLE_FUNDING_SYNC` | unset/false | Optional Merkle refresh; PSBT + block monitor is primary |

API keys are issued by `POST /api/auth/challenge` + `POST /api/auth/verify`.
`STARGATE_API_KEY` is not a login (retired). Do not copy ingest/callback tokens from an old Helm secret into the login path.

## Bitcoin / btcd

| Variable | Default | Notes |
| --- | --- | --- |
| `BITCOIN_NETWORK` | `testnet4` | `testnet4` \| `signet` \| `testnet` \| `mainnet` |
| `BTCD_MODE` | `managed` | `managed` \| `external` \| `off` (Esplora fallback) |
| `BTCD_RPC_HOST` | localhost when managed | Required for `external` |
| `BTCD_DATADIR` | under data dir | Persist across restarts |
| `BTCD_BIN` | `btcd` | Bare name `btcd`: embedded executable, extracted to `$STARGATE_DATA_DIR/bin/btcd` (builds with `-tags embedbtcd`). Otherwise a sibling of the stargate executable, then `PATH`. A path is used as given. |
| `BTCD_ALLOW_MAINNET` | `false` | Must be true to run mainnet |
| `BTCD_CONNECT` / `BTCD_ADDPEER` | unset | Pin trusted peers only |
| `CHAIN_SETTLEMENT_CONFIRMATIONS` | `20` | Proofs stay `provisional` until this depth |
| `STARLIGHT_DONATION_ADDRESS` | unset | Optional extra P2WPKH in the funding PSBT. **Does not gate OP_RETURN.** |
| `BLOCK_MONITOR_CONTRACT_DISCOVER` | on | Walk local blocks for `wish_hash\|\|stego_hash`. `off` leaves history to on-demand scan |
| `BLOCK_MONITOR_CONTRACT_DISCOVER_INTERVAL` | `5s` | Pause between walk batches |
| `BLOCK_MONITOR_CONTRACT_DISCOVER_BATCH` | `100` | Blocks read per batch. Cap 500 |
| `BLOCK_MONITOR_CONTRACT_DISCOVER_STORE_ATTEMPTS` | `5` | After this many store failures, skip that height and keep the pending commitment |
| `BLOCK_MONITOR_CONTRACT_DISCOVER_MISS_GRACE` | `6h` | After the walk is caught up, drop a new commitment that still has no stego file and no contract row. `0s` drops it on the next caught-up pass |

Funding PSBTs always include the 64-byte OP_RETURN (`wish_hash \|\| stego_hash`)
with `commitment_sats` default **1000** (min 546). Explicit `0` is a validation
error. Donation address only adds/omits the donation output.

## Scanner (Trin / GGUF)

| Variable | Default | Notes |
| --- | --- | --- |
| `STARLIGHT_LLM_GGUF` | unset | Opus Qwen2.5-0.5B GGUF. When this file exists it wins, and image scans run that model inside Trin |
| `STARLIGHT_GGUF` | unset | Pin a local GGUF if the file exists. A `qwen2` file is the language-model scanner; anything else is the CNN |
| `STARLIGHT_TRIN_MODEL` | unset | Alias if `STARLIGHT_GGUF` is missing |
| `STARLIGHT_HF_REPO` | `macroadster/starlight-prod` | Auto-download target |
| `STARLIGHT_HF_FILE` | `starlight.gguf` | |
| `STARLIGHT_HF_REVISION` | `main` | |
| `STARLIGHT_GGUF_FORCE_DOWNLOAD` | unset | `1`/`true` re-fetches into the data dir |
| `STARGATE_PROXY_BASE` | unset | Legacy Python sidecar only |
| `STARGATE_STEGO_APPROVAL_ENABLED` | unset | Optional sidecar approval; not required for replication |

Selection: Qwen2 language model when the resolved GGUF says `general.architecture=qwen2`, otherwise Trin CNN → Alpha → Mock. Missing GGUF / download failure falls through. The Qwen2 path is the Opus 0.5B model running inside Trin. It is much slower than the CNN, so leave `STARLIGHT_LLM_GGUF` unset for block scanning.
Detail: [TRIN_STARLIGHT_SCANNER.md](./TRIN_STARLIGHT_SCANNER.md).

## Built-in agents (optional)

| Variable | Default | Notes |
| --- | --- | --- |
| `STARGATE_AGENT_ENABLED` | `false` | Master switch |
| `STARGATE_AGENT_WATCHER_ENABLED` | — | Discover / propose |
| `STARGATE_AGENT_WORKER_ENABLED` | — | Claim / execute / submit |
| `STARGATE_AGENT_AI_IDENTIFIER` | — | |
| `STARGATE_AGENT_POLL_INTERVAL` | `60` | Seconds |
| `STARGATE_AGENT_EXECUTOR` | auto | `opencode`, `claude`, `grok`, `agy`, `codex`, or `stub` |
| `STARGATE_AGENT_EXECUTOR_MODEL` | unset | |
| `STARGATE_AGENT_SYSTEM_PROMPT` | unset | Path to prompt file |

Real executors can run arbitrary commands in the task sandbox. Force `stub` in CI.

## IPFS mirror (optional)

| Variable | Default | Notes |
| --- | --- | --- |
| `IPFS_ENABLED` | `true` | Embedded libp2p mirror |
| `IPFS_API_URL` | unset | External Kubo if you have one |
| `IPFS_MIRROR_ENABLED` | — | Durable uploads topic |
| `IPFS_MIRROR_TOPIC` | `stargate-uploads` | PSBT-built artifacts |
| `IPFS_WISH_TOPIC` | `stargate-wishes` | Inscribed wishes without a PSBT |
| `IPFS_WISH_TTL` | `168h` | Unengaged wishes unpinned after 7 days |
| `IPFS_EMBEDDED_BOOTSTRAP` | `starlight-ai.freemyip.com` | `none` = mDNS only; `public` = Protocol Labs DHT (CPU-heavy; GO-2024-3218 still unfixed upstream) |
| `IPFS_DHT_MODE` | `client` | `client` (default) does not serve DHT queries. `server` / `auto` only if this node should. |
| `IPFS_IDENTITY_FILE` | `$STARGATE_DATA_DIR/ipfs_identity.key` | Stable Peer ID |

`GET /api/ipfs-mirror/status` reports both topics. Bitcoin remains settlement;
filenames are SHA256, not CIDs in OP_RETURN.

## Related

- ADR 0001 single binary, 0002 storage, 0006 btcd, 0007 attestation
- [DOMAIN_SEAMS.md](./DOMAIN_SEAMS.md), [starlight_contracts.md](./starlight_contracts.md) §12
