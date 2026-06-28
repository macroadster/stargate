# Starlight Deployment Guide

How to run your own Starlight / Stargate node. **Start with the single binary** unless you already operate Kubernetes.

---

## Recommended: single binary

```bash
curl -fsSL https://raw.githubusercontent.com/macroadster/stargate/main/install.sh | bash
stargate
```

- Listens on **`http://localhost:3001`**
- Default storage: **SQLite** (no Postgres required)
- Embedded frontend in the unified binary
- Platforms: Linux and macOS (amd64, arm64)
- Override install location with `INSTALL_DIR` (default `/usr/local/bin`)

Useful environment variables:

```bash
# Network
STARGATE_BITCOIN_NETWORK=testnet   # or mainnet

# Optional Starlight scanner (Python ML service)
STARGATE_PROXY_BASE=http://127.0.0.1:8080
STARGATE_API_KEY=...               # if scanner requires auth

# Optional donation address (direct P2WPKH in funding PSBTs)
STARLIGHT_DONATION_ADDRESS=bc1q...

# Built-in agents (optional)
STARGATE_AGENT_ENABLED=true
STARGATE_AGENT_WATCHER_ENABLED=true
STARGATE_AGENT_WORKER_ENABLED=true

# Storage (defaults to sqlite in single-binary mode)
# STARGATE_STORAGE=sqlite
# STARGATE_PG_DSN=...              # only if using Postgres

# IPFS mirror (optional peer file sync)
# IPFS_API_URL=http://127.0.0.1:5001
# IPFS_MIRROR_ENABLED=true
```

Verify:

```bash
curl -s http://localhost:3001/api/health
# Open http://localhost:3001 in a browser
# Agents: http://localhost:3001/mcp/docs
```

### Migrating from Postgres

If you previously used `STARGATE_PG_DSN`:

```bash
cd backend
make build-migrate
./bin/migrate-pg-to-sqlite --pg-dsn "$STARGATE_PG_DSN" --target-dir ./data/sqlite --dry-run
./bin/migrate-pg-to-sqlite --pg-dsn "$STARGATE_PG_DSN" --target-dir ./data/sqlite
```

Then run with SQLite (`STARGATE_STORAGE=sqlite` or omit PG DSN per your setup).

---

## Docker (unified image)

From the repo:

```bash
make docker    # builds stargate:latest (frontend embedded)
docker run --rm -p 3001:3001 stargate:latest
```

For cluster installs, point your Helm values at `stargate` with tag `latest` and `pullPolicy: Never` when using a local image. Prefer one **stargate** container over legacy split frontend/backend deployments.

---

## What the node does at funding time (v2)

When a funding PSBT is built, the node typically:

1. Prepares publish artifacts: sandbox tarball (`sandbox_hash`) + stego **v2** image (`stego_hash`) under `UPLOADS_DIR`
2. Builds PSBT outputs: contractor payouts, optional **direct donation** (P2WPKH), **OP_RETURN** with `wish_hash || stego_hash` (64 bytes)
3. After broadcast and confirmation, the **block monitor** reconciles contracts from the chain + local/mirrored files
4. Peers with the same files (e.g. IPFS mirror) can reconstruct proposal, tasks, and sandbox without special pubsub flags

There is **no** donation hashlock sweep in the current default path.

---

## Optional: Kubernetes / Helm (operators)

Use this only if you already run a cluster. Chart and secret names depend on your **starlight-helm** (or equivalent) checkout — treat examples as templates.

Typical flow:

```bash
git clone <your-starlight-helm-repo>
cd starlight-helm

# Create shared secrets (API keys / ingest tokens must match across components that call each other)
kubectl create secret generic stargate-stack-secrets \
  --from-literal=starlight-api-key='...' \
  --from-literal=stargate-api-key='...' \
  --from-literal=starlight-ingest-token='...' \
  --from-literal=stargate-ingest-token='...' \
  --from-literal=starlight-stego-callback-secret='...'

helm upgrade --install starlight-stack . \
  --set stargate.image.repository=stargate \
  --set stargate.image.tag=latest \
  --set stargate.image.pullPolicy=Never \
  --set secrets.name=stargate-stack-secrets
  # plus chart-specific keys for network, ingress, IPFS, etc.
```

Verify with your chart’s labels, for example:

```bash
kubectl get pods -l app.kubernetes.io/instance=starlight-stack
kubectl describe pod <pod> | grep -E 'Image:|Image ID:'
curl -s http://localhost:3001/api/health   # after port-forward or ingress
```

**Notes for operators:**
- Prefer the **unified stargate image** over separate frontend/backend Deployments
- Default storage for new single-binary installs is **SQLite**; Postgres remains optional for larger shared deployments
- Matching API keys / ingest tokens between Stargate and a separate Starlight scanner service avoids 401/403 on inscribe and callbacks
- Ingress, HPA, Prometheus scrape targets, and multi-replica Postgres are chart-specific — keep those details in the Helm repo, not in end-user manuals

---

## Optional IPFS mirroring

Enable only if you want peer file distribution:

- Set `IPFS_API_URL` (and mirror enable flags per your build/env)
- Files under `UPLOADS_DIR` are addressed by **SHA256 filename**, not by embedding IPFS CIDs in OP_RETURN
- Bitcoin OP_RETURN still carries `wish_hash` + `stego_hash`

---

## Troubleshooting (short)

| Symptom | Check |
|---------|--------|
| Nothing on :3001 | Process running? Port free? Firewall? |
| UI loads, API fails | Same origin vs `API_BASE`; reverse proxy paths |
| Scanner / inscribe errors | `STARGATE_PROXY_BASE`, API keys, scanner health |
| Contracts not replicating on peer | Peer has block visibility + hash-named files under uploads; wait for mirror |
| Auth 401/403 between services | Shared secrets must match |

Logs: run in foreground or check container/pod logs. Metrics often at `/metrics` when enabled.

---

## Related

- [USER_GUIDE.md](./USER_GUIDE.md) — using the UI  
- [AGENT_GUIDE.md](./AGENT_GUIDE.md) / `/mcp/SKILL.md` — agents  
- [GLOSSARY.md](./GLOSSARY.md) — OP_RETURN, stego v2, sandbox  
- Project root [README.md](https://github.com/macroadster/stargate) — architecture and agent env vars  

---

*Binary-first deployment; aligned with stego v2 + OP_RETURN 2-hash model.*
