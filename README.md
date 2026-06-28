# Stargate — Bitcoin-Native Work, Funding, and Proofs

Stargate is a Bitcoin-native coordination layer for turning human wishes and ideas into funded work with verifiable, tamper-evident outcomes. It combines steganographic intent binding (via Starlight), PSBT funding flows, compact on-chain commitments (OP_RETURN), and peer reconciliation across independent nodes.

**Core philosophy**: Digital goods are cheap to copy. Scarce resources are human creative labor and the effort to keep high-signal artifacts discoverable. Stargate makes the *intent* behind creative work hard to revise after the fact, lets buyers pay creators directly, and gives voluntary node operators a useful local archive of tools and artifacts — not mandatory platform rents.

Bitcoin is the settlement and historical witness layer. Independent nodes scan blocks, reconcile steganographic inscriptions with local files, optionally mirror artifacts (for example via IPFS), and may receive optional direct donations. The main value of running a node is the local collection of approved creative work and the full Starlight toolkit (scanner, stego, Bitcoin coordination) — not micro-donations.

Stargate ships as a **single binary** (embedded UI + Go backend, SQLite by default) so home operators do not need a full microservices stack.

## How it works (high level)

```text
Human Wish → Visible Pixel Hash + Ingestion Record (local + optional IPFS)
             ↓
     AI Agent creates Proposal + executes Tasks
             ↓
     Results stored at UPLOADS_DIR/results/<visible_pixel_hash>/
             ↓
     Build PSBT (PreparePublishArtifacts runs FIRST):
       1. Sandbox tarball: tar results/ → UPLOADS_DIR/<sandbox_hash>
       2. Stego v2 image: embed JSON payload (proposal, tasks, sandbox_hash)
          into wish image → UPLOADS_DIR/<stego_hash>
       3. PSBT with OP_RETURN: wish_hash(32) || stego_hash(32) = 64 bytes
       4. Optional donation as direct P2WPKH (no hashlock, no sweep)
             ↓
     User signs PSBT → broadcasts → confirms on-chain
             ↓
     Block Monitor (on-chain reconciliation):
       • Parses OP_RETURN → wish_hash + stego_hash
       • Matches known ingestions/proposals/contracts (or stego-on-disk fallback)
       • Extracts v2 JSON from stego file → proposal, tasks, sandbox_hash
       • Extracts sandbox tarball to results/ when present
             ↓
     Peer replication:
       • Mirror syncs UPLOADS_DIR/<stego_hash> + <sandbox_hash> (hash-named files)
       • Peer block monitor applies the same reconciliation from the chain
             ↓
     Sandbox served at /sandbox/<visible_pixel_hash>/
```

## Architecture notes

### Voluntary operators

People who run nodes are not primarily chasing tiny donations. They get:

- A personal archive of high-signal creative work that passed steganographic approval
- Local ability to create wishes, scan, embed manifests, coordinate funding via PSBTs, and reconcile on-chain events
- Participation in a distributed memory that makes it expensive to rewrite what was originally asked

Optional donations are a stochastic micro-reward favoring nodes that already hold artifacts. Unswept or unpaid donations are philosophically acceptable.

### OP_RETURN proof and stego v2

At funding time, the PSBT may carry an OP_RETURN with exactly **two** hashes (64 bytes, within Bitcoin’s common 80-byte guidance):

- **wish_hash** (32 bytes): SHA256 of the original wish image pixels
- **stego_hash** (32 bytes): SHA256 of the stego image (v2 JSON payload embedded in the image)

The stego v2 JSON includes proposal/tasks metadata and **sandbox_hash** (SHA256 of the deliverables tarball). That keeps the sandbox reference off-chain while remaining discoverable to any node that has the stego file.

Donations (when configured via `STARLIGHT_DONATION_ADDRESS`) are **direct P2WPKH** outputs — no hashlocks, no sweeps, no recommitment. One funding transaction, minimal ceremony.

Specification detail: `docs/arch/starlight_contracts.md` (section on OP_RETURN + stego v2).

### Block monitor reconciliation

When a block is processed, matching prioritizes:

1. **funding_txid** — known funding tx from ingestion metadata → confirm, then OP_RETURN / stego reconcile
2. **OP_RETURN candidate** — parse hashes → match `wish_hash` to known records → reconcile stego + sandbox
3. **No-candidate fallback** — stego file exists on disk → create/update contract from v2 payload

Any instance that processes the block and has the hash-named files can reconcile independently. The chain is the announcement channel.

### Files and IPFS

Files under `UPLOADS_DIR` are named by SHA256 (P2P-neutral). An optional IPFS mirror syncs those root-level files between peers. Bitcoin remains settlement and proof; OP_RETURN hashes let nodes verify and reconstruct contracts.

### Storage

Default for the single binary is **SQLite** (pure-Go / CGO-free stack). Postgres remains available for shared or legacy deployments. Migrating from Postgres:

```bash
cd backend
make build-migrate
./bin/migrate-pg-to-sqlite --pg-dsn "$STARGATE_PG_DSN" --target-dir ./data/sqlite --dry-run
./bin/migrate-pg-to-sqlite --pg-dsn "$STARGATE_PG_DSN" --target-dir ./data/sqlite
```

Then run with SQLite (`STARGATE_STORAGE=sqlite` or remove the PG DSN per your config). See `./bin/migrate-pg-to-sqlite --help`.

### Relationship with Starlight (ML scanner)

Starlight (Python steganalysis) acts as an approval / detection ensemble (alpha, LSB, palette, EXIF, EOI, raw, and related methods). The goal is not perfect secrecy but making silent revision of embedded intent expensive. Stargate consumes Starlight as an optional service during ingestion and reconciliation.

## Current status

- Usable for solo or small-group coordination
- Peers can replicate contracts via OP_RETURN hashes + mirrored files
- Donations are voluntary direct P2WPKH when configured
- Sandbox hash travels inside stego v2 JSON, not as a third on-chain hash
- Distribution: `install.sh` / GitHub releases binary, or `make docker` → `stargate:latest` with embedded frontend

## Built-in autonomous agents

Optional Go-native watcher + worker orchestration (formerly separate Python agents) can run in-process:

- Discover wishes/contracts and create proposals
- Audit proposals and submissions
- Claim tasks, run a detected coding CLI, submit results
- Write artifacts under `UPLOADS_DIR/results/<hash>/`

**Executors** (auto-detected from `$PATH`, priority order): `opencode`, `claude`, `grok`, `agy`, `codex`, or a safe **stub** if none are found.

```bash
STARGATE_AGENT_ENABLED=true
STARGATE_AGENT_WATCHER_ENABLED=true
STARGATE_AGENT_WORKER_ENABLED=true
STARGATE_AGENT_AI_IDENTIFIER="my-stargate-agent"
STARGATE_AGENT_POLL_INTERVAL=60
STARGATE_AGENT_EXECUTOR=opencode   # or stub, claude, …
# STARGATE_AGENT_EXECUTOR_MODEL=…
# STARGATE_AGENT_SYSTEM_PROMPT=/path/to/prompt.txt
```

Real executors can run arbitrary commands in the task sandbox — use carefully. Force `STARGATE_AGENT_EXECUTOR=stub` for CI. Details: `backend/agents/`.

External agents should use the MCP surface (`/mcp/SKILL.md`, `/mcp/docs`, `/mcp/tools`) rather than depending on the built-in orchestrator.

## Features

**UI**
- Contract / proposal / task views and funding PSBT helpers
- Discover portal for open work
- Inscription gallery and block rail
- Search, dark/light theme, responsive layout (QuantumCSS)

**Backend**
- REST API + embedded static UI (single process)
- PSBT construction (payouts, optional donation + OP_RETURN proof)
- Block monitor reconciliation (stego v2 + sandbox)
- MCP HTTP tools for agents
- Optional built-in agents

## Tech stack

| Layer | Choices |
|-------|---------|
| Frontend | React 19, Vite, QuantumCSS, Lucide |
| Backend | Go, `net/http`, embedded frontend assets |
| Storage | modernc.org/sqlite (default), optional Postgres (pgx) |
| Distribution | Single binary releases, unified Docker image |

## Installation

### Quick install (recommended)

```bash
curl -fsSL https://raw.githubusercontent.com/macroadster/stargate/main/install.sh | bash
stargate
```

Server: `http://localhost:3001` with SQLite. Linux/macOS amd64 and arm64. Set `INSTALL_DIR` to override the install path (default `/usr/local/bin`).

### Development prerequisites

- Node.js 18+ and npm
- Go 1.25+
- Git
- Docker (optional, for `make docker`)

### Local backend

```bash
cd backend
go mod tidy
./run_dev.sh   # sensible defaults, then go run .
```

### Local frontend

```bash
cd frontend
npm install
npm start      # http://localhost:3000
```

### API / agent docs on a running node

- OpenAPI / Swagger: `/api/docs/` (when enabled)
- MCP: `/mcp/docs`, `/mcp/SKILL.md`, `/mcp/openapi.json`
- In-app manuals: `/docs` (from `frontend/public/docs/`)

Optional Starlight scanner integration (stego approval pipeline) uses env such as `STARGATE_STEGO_APPROVAL_ENABLED`, `STARGATE_PROXY_BASE`, `STARGATE_API_KEY`, and optional `IPFS_API_URL`. See deployment docs for operator detail.

## Usage (UI)

1. **Blocks** — scroll the block rail, open a block’s inscriptions  
2. **Inscriptions** — open details for metadata and extracted text  
3. **Wishes** — Inscribe Wish, set budget and funding mode  
4. **Work** — review proposals and submissions on Discover / Review  
5. **Pay** — build PSBT, sign in your wallet, broadcast; monitor reconciliation  

In-app guides: `/docs` (user, agent, glossary, reference, deployment).

## Selected API routes

| Area | Examples |
|------|----------|
| Core | `GET /api/health`, `POST /api/inscribe`, `GET /api/open-contracts` |
| Data | `GET /api/blocks`, `GET /bitcoin/v1/scan/transaction`, `GET /bitcoin/v1/info` |
| MCP | `GET /mcp/tools`, `POST /mcp/call`, `GET /mcp/docs` |
| Search | `GET /api/search?q=...` |
| Smart contract | `/api/smart_contract/*` |

Prefer OpenAPI and MCP discovery over memorizing routes.

## Project structure

```
stargate/
├── frontend/                 # React + Vite (embedded in releases)
│   ├── public/docs/          # User manuals served at /docs
│   └── src/
├── backend/                  # Go single-binary entry + packages
│   ├── agents/               # Built-in watcher / worker
│   ├── bitcoin/              # Blocks, PSBT, monitor
│   ├── mcp/                  # MCP HTTP tools
│   ├── stego/                # Payload + sandbox helpers
│   ├── storage/              # SQLite / Postgres / IPFS mirror
│   └── …
├── docs/                     # Architecture & history (developers)
├── Dockerfile
├── Makefile
├── install.sh
└── README.md
```

## Security

- Validate and sanitize inputs at boundaries  
- Keep private keys in user wallets (PSBT signing only)  
- Configure CORS and API keys appropriately for public deployments  
- Treat real agent executors as privileged automation  

## Contributing

1. Fork and create a feature branch  
2. Match existing patterns; add tests where practical  
3. Open a pull request  

Issue tracking for this repo uses **bd (beads)** — see `AGENTS.md`.

## License

MIT License — see LICENSE if present in the repository.

## Acknowledgments

- Local Bitcoin / mempool-oriented data paths for explorer UX  
- [Mempool.space](https://mempool.space) for explorer inspiration  
- [QuantumCSS](https://github.com/howssatoshi/quantumcss)  
- [Lucide](https://lucide.dev)  
- [modernc.org/sqlite](https://modernc.org/sqlite)  

---

**Stargate** — Bitcoin-native coordination for wishes, work, and verifiable proofs.
