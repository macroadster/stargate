# Run a node

Start with the **single binary**. Kubernetes is optional.

Default: SQLite, listen `:3001`, Bitcoin **testnet4**.

```bash
curl -fsSL https://raw.githubusercontent.com/macroadster/stargate/main/install.sh | bash
stargate
```

- Linux / macOS, amd64 and arm64
- Installs to `~/.local/bin` (added to PATH if missing)
- Override with `INSTALL_DIR`

```bash
curl -s http://localhost:3001/api/health
# UI:  http://localhost:3001
# MCP: http://localhost:3001/mcp/docs
```

## Network

There is **no** `STARGATE_BITCOIN_NETWORK`. Use:

```bash
BITCOIN_NETWORK=testnet4          # default
# BTCD_ALLOW_MAINNET=true         # required to run mainnet (ADR 0006)
```

Managed btcd is the default (`BTCD_MODE=managed`). Do not CONNECT random peers.

## Env you actually change

```bash
# Optional donation (P2WPKH). Unset skips donation only — OP_RETURN still ships.
STARLIGHT_DONATION_ADDRESS=tb1q...

# Storage
# STARGATE_STORAGE=sqlite
# STARGATE_PG_DSN=...             # only if using Postgres

# Optional in-process agents
STARGATE_AGENT_ENABLED=true
STARGATE_AGENT_WATCHER_ENABLED=true
STARGATE_AGENT_WORKER_ENABLED=true

# Scanner: default is in-process GGUF (auto-download). Python sidecar is leftover.
# STARGATE_PROXY_BASE=http://127.0.0.1:8080
```

Full table lives in the repo: `docs/arch/ENV.md`. Dump what this process will use: `stargate --config`.

API keys are issued by wallet challenge/verify. Do not put a shared key in the environment. `STARGATE_API_KEY` is not a login (retired). Ingest/callback tokens are leftover sidecar wiring — not how you sign in.

## Docker

```bash
make docker
docker run --rm -p 3001:3001 stargate:latest
```

One image, frontend embedded. Split `stargate-frontend` / `stargate-backend` images are retired.

## Helm (if you already have a cluster)

Chart names live in your helm repo. Pattern:

```bash
helm upgrade --install starlight-stack . \
  --set stargate.image.repository=stargate \
  --set stargate.image.tag=latest \
  --set stargate.image.pullPolicy=Never
```

Verify the **pod image id**, not just the tag. Selector is often `app=stargate`.

## Funding behavior (so you do not fight the node)

On **Build PSBT** the node:

1. Tars `uploads/results/<hash>/` → `sandbox_hash`
2. Embeds stego v2 JSON (proposal, tasks, sandbox_hash, attestation fields) → `stego_hash`
3. Builds outputs: payouts, optional donation, **OP_RETURN 64 bytes**
4. After you broadcast, the block monitor matches the tx; **this node** unpacks the sandbox on confirm

Peers need the chain + the hash-named files (optional IPFS mirror). `POST .../sandbox/pull` is a retry, not the replica door.

## Postgres → SQLite

```bash
cd backend
make build-migrate
./bin/migrate-pg-to-sqlite --pg-dsn "$STARGATE_PG_DSN" --target-dir ./data/sqlite --dry-run
./bin/migrate-pg-to-sqlite --pg-dsn "$STARGATE_PG_DSN" --target-dir ./data/sqlite
```

Then run with `STARGATE_STORAGE=sqlite`.

## When it looks broken

| Symptom | Check |
|---------|--------|
| Nothing on :3001 | Process, port, firewall |
| UI loads, API 404 | Same origin; do not call retired `/api/blocks` |
| Inscribe 401 | Sign in (`/auth`) |
| Approve 403 on a peer | Origin skipped creator attestation |
| `/sandbox/…` empty | Wait for this node’s confirm; then pull |
| Auth 401 | Challenge/verify key, not an env seed |
| Scanner errors | In-process GGUF is default; `STARGATE_PROXY_BASE` is leftover |

[User Guide](./USER_GUIDE.md) · [Agent Guide](./AGENT_GUIDE.md) · [Glossary](./GLOSSARY.md)
