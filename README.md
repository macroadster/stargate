# Stargate — Bitcoin-Native Work, Funding, and Proofs

Stargate is a Bitcoin-native coordination layer for turning human wishes and ideas into funded work with verifiable, tamper-evident outcomes. It combines steganographic intent binding (via Starlight), on-chain commitments via hashlocks, PSBT funding flows, and distributed oracle-style reconciliation across independent nodes.

**Core philosophy**: Digital goods are effectively free to copy. The scarce resources are human creative labor and the ongoing effort to keep high-signal artifacts discoverable and alive. Stargate exists to make the *intent* behind creative work hard to revise after the fact, to let buyers pay creators directly for labor, and to give people who run voluntary Starlight nodes a useful personal archive of tools and artifacts — not to extract mandatory platform rents.

Stargate treats Bitcoin as the final settlement layer and historical witness, not as a platform to replace. Independent Starlight instances (connected via IPFS pubsub) act as voluntary, low-energy operators that scan blocks, reconcile steganographic inscriptions with prior ingestions, publish approved artifacts to IPFS, and can compete for tiny optional "lottery" donations via hashlock sweeps. The real value of running a node is the local collection of high-quality AI-generated creative work and the full set of Starlight tools (scanner, stego, Bitcoin coordination) — not the micro-donations.

The system is intentionally tolerant of unswept hashlocks and centralized hosting in the short term. Diversity of home-hosted instances with different local tools is preferred over one canonical powerful server in the long term. Stargate is evolving toward a single-binary distribution model that lets people run useful nodes without a full microservices stack.

## How it works (high level)

```text
Human Wish → Visible Pixel Hash + Ingestion Record (local + IPFS)
             ↓
     AI Agent creates Proposal + executes Tasks
             ↓
     Results stored at UPLOADS_DIR/results/<visible_pixel_hash>/
             ↓
     Build PSBT (PreparePublishArtifacts runs FIRST):
       1. Sandbox tarball: tar results/ → UPLOADS_DIR/<sandbox_hash>
       2. Stego v2 image: embed JSON payload (proposal, tasks, sandbox_hash)
          into wish image alpha channel → UPLOADS_DIR/<stego_hash>
       3. PSBT built with OP_RETURN: wish_hash(32) || stego_hash(32) = 64 bytes
       4. Donation paid directly as P2WPKH (no hashlock, no sweep)
             ↓
     User signs PSBT → broadcasts → confirms on-chain
             ↓
     Block Monitor (on-chain reconciliation):
       • Parses OP_RETURN → extracts wish_hash + stego_hash
       • Matches against known ingestions/proposals/contracts
       • Finds stego image on disk → extracts v2 JSON payload
       • Creates/updates proposal + contract + tasks from payload
       • Reads sandbox_hash from payload → extracts tarball to results/
             ↓
     Peer Replication (fully autonomous):
       • IPFS mirror syncs UPLOADS_DIR/<stego_hash> + <sandbox_hash>
       • Peer's block monitor parses same OP_RETURN from blockchain
       • Same reconciliation flow — contract replicated with sandbox
             ↓
     Sandbox served at /sandbox/<visible_pixel_hash>/
```

## Architecture & Philosophy (detailed)

### Voluntary Operators and the Real Value of a Node

People who run Starlight instances are not primarily chasing the tiny hashlock donations. The real prize is the content and tooling that becomes locally available:

- A personal, searchable archive of high-signal AI-generated creative work (games, tools, writing, art, experiments) that has passed through the steganographic approval process.
- The full local capability to create new wishes, run the Starlight scanner, embed manifests, coordinate funding via PSBTs, publish to IPFS, and reconcile on-chain events.
- Participation in a distributed memory system that makes it expensive to later pretend an original creative request was different from what was actually asked.

Running a node is an act of tool collection and cultural preservation. The small donation sweep is a stochastic micro-reward (a "lottery") that favors the instance closest to the original creation event because it has the artifacts locally before IPFS propagation reaches others. Anyone can win, but the originating node has a natural first-mover advantage. If the donations are never swept, that is philosophically acceptable — it would only indicate that Bitcoin itself had lost all value.

### OP_RETURN Proof and Stego v2 Replication

At funding time, the PSBT carries an OP_RETURN output with exactly 2 hashes (64 bytes total, within Bitcoin's 80-byte recommendation):

- **wish_hash** (32 bytes): SHA256 of the original wish image pixels
- **stego_hash** (32 bytes): SHA256 of the stego image (wish image with embedded v2 JSON payload)

The stego v2 JSON payload — embedded in the wish image's alpha channel — contains the full proposal, tasks, metadata, and the **sandbox_hash** (SHA256 of the deliverables tarball). This keeps the sandbox hash off-chain while making it discoverable by any node that has the stego image.

Donations are paid directly as standard P2WPKH outputs to `STARLIGHT_DONATION_ADDRESS` — no hashlocks, no sweeps, no recommitment. One transaction, zero ceremony.

See `docs/arch/starlight_contracts.md` Section 12 for the full specification.

### Block Monitor Reconciliation

When a Bitcoin block is processed, the block monitor has three matching paths (in priority order):

1. **funding_txid match**: Known funding txid from ingestion metadata → confirm contract, then scan OP_RETURN for stego hash → reconcile stego + sandbox
2. **OP_RETURN candidate match**: Parse OP_RETURN → match wish_hash against known ingestions/proposals/contracts → reconcile stego + sandbox
3. **OP_RETURN no-candidate fallback**: No database match, but stego image exists on disk → reconcile from stego v2 payload (creates the contract from scratch)

In all paths, `reconcileOnChainArtifacts` finds the stego image at `UPLOADS_DIR/<stego_hash>`, extracts the v2 JSON payload, upserts the contract/proposal/tasks, and triggers sandbox extraction.

Any Starlight instance that processes the block + has the files via IPFS mirror can perform this reconciliation independently. The blockchain is the announcement channel — no pubsub flags or special configuration needed.

### IPFS Mirror as File Distribution, Bitcoin as Settlement

Files in `UPLOADS_DIR` are named by their SHA256 hash (P2P-layer neutral — no IPFS CID leakage into Starlight). The IPFS mirror syncs root-level files between peers using these hash-based filenames. Bitcoin is the settlement and proof layer — OP_RETURN hashes let any node verify and reconstruct contracts from the on-chain record.

### Single-Binary Direction and Future Provenance Ideas

Stargate is moving toward a single-binary distribution model (downloadable, runnable binary without requiring the full microservices stack of Go backend + Python Starlight + Postgres + IPFS node). This lowers the barrier for home operators.

**Migrating from Postgres to SQLite**: If you have an existing deployment using `STARGATE_PG_DSN`, use the dedicated migration utility:

    cd backend
    make build-migrate
    ./bin/migrate-pg-to-sqlite --pg-dsn "$STARGATE_PG_DSN" --target-dir ./data/sqlite --dry-run   # preview
    ./bin/migrate-pg-to-sqlite --pg-dsn "$STARGATE_PG_DSN" --target-dir ./data/sqlite             # execute

Then set `STARGATE_STORAGE=sqlite` (or simply remove the PG DSN env var) and restart. The tool correctly converts JSONB→TEXT, TEXT[] skills→comma strings, TIMESTAMPTZ→RFC3339 text, etc. See `bin/migrate-pg-to-sqlite --help`.

This is now implemented: at PSBT build time, `PreparePublishArtifacts` tars the sandbox artifacts (`results/<visible_pixel_hash>/`), computes the SHA256, and embeds it as `sandbox_hash` in the stego v2 JSON payload. Peers extract the tarball by hash after replication, giving AI-generated deliverables a bit-level verifiable provenance chain.

### Relationship with Starlight (the ML scanner)

Starlight (the Python AI steganalysis system) is the approval oracle. Multiple detection methods (alpha, LSB, palette, EXIF, EOI, raw) are used in an ensemble. The goal is not perfect secrecy but making it expensive to alter the embedded wish or task context without visibly destroying the image. The Go backend (Stargate) consumes Starlight as a service for scanning blocks and individual images during ingestion and reconciliation.

## Current Status & Direction

- The system is fully functional for solo or small-group use.
- Multiple independent instances replicate contracts autonomously via OP_RETURN hashes + IPFS mirror file sync.
- The donation mechanism is intentionally voluntary and non-rent-seeking — direct P2WPKH, no hashlocks or sweeps.
- OP_RETURN uses 64 bytes (2 hashes), within Bitcoin's 80-byte standard recommendation.
- Sandbox (AI deliverables) hash is carried inside the stego v2 JSON payload, not on-chain.
- Single-binary distribution: `make docker` produces a unified `stargate:latest` image with embedded frontend.

## Built-in Autonomous Agents

Stargate ships with a complete Go-native agent orchestration system (the functionality previously lived in the separate Python `starlight.agents` module).

When enabled, the orchestrator runs inside the same process as the rest of the backend and can:

- Discover open wishes/contracts and create proposals
- Audit pending proposals (recursive detection, budget sanity, scope, quality heuristics)
- Audit submissions and approve/rework/reject them
- Claim tasks, execute work using a detected AI coding CLI, and submit results
- Handle rework requests, continuation work, and persistent memory (`memory.md`)
- Write artifacts + a navigation `index.html` into the task sandbox under `UPLOADS_DIR/results/<hash>/`

### Supported Execution Tools (auto-detected from $PATH)

The `AutoDetectExecutor` looks for these binaries (in priority order):

- `opencode` (strongly recommended)
- `claude`
- `grok`
- `agy`
- `codex`

If none are found (or you force it), a safe **stub executor** is used that still produces reports and `index.html`.

### Key Environment Variables

```bash
# Master switch (default: true)
STARGATE_AGENT_ENABLED=true

# Enable the two roles independently
STARGATE_AGENT_WATCHER_ENABLED=true   # proposal + submission audits
STARGATE_AGENT_WORKER_ENABLED=true    # wish → proposals + task execution

STARGATE_AGENT_AI_IDENTIFIER="my-stargate-agent"
STARGATE_AGENT_POLL_INTERVAL=60

# Force a specific tool (or "stub")
STARGATE_AGENT_EXECUTOR=opencode
# or
STARGATE_AGENT_EXECUTOR=stub

# Model override (injected as --model / -m where supported)
STARGATE_AGENT_EXECUTOR_MODEL=claude-3-5-sonnet-20241022

# Optional: provide a custom base system prompt
STARGATE_AGENT_SYSTEM_PROMPT=/etc/stargate/agent-prompt.txt
```

See `backend/agents/config.go` and `backend/agents/executor.go` for the full set of supported variables and how arguments are constructed for each tool.

### How to Run with a Real Tool

1. Install one of the supported CLIs (e.g. `opencode`).
2. Make sure it is in `$PATH` and can reach your Stargate MCP endpoint (via its own config, e.g. `opencode.json`).
3. Set the variables above and (re)start Stargate.

The agents will automatically use the tool to do real work inside each task's isolated results directory.

### Safety Notes

- Real execution tools can run arbitrary commands in the sandbox directory. Use with care.
- For tests/CI you can force `STARGATE_AGENT_EXECUTOR=stub`.
- The orchestrator has built-in rate limits, hoarding prevention (one active task at a time), rejection caching, and resource awareness.

See the source in `backend/agents/` for the full implementation (Watcher, Worker, Orchestrator, and the declarative tool invocation specs).

## 🌟 Features

### Frontend (React)
- **Smart Contract Viewer**: Detailed modal views for proposals, tasks, and funding proofs
- **Task Discovery**: Portal for AI and humans to claim, submit, and verify work
- **Funding & PSBTs**: Build payout or raise-fund PSBTs with multi-payer support
- **Inscription Viewer**: Grid view of inscriptions with metadata
- **Block Explorer**: Horizontal scrolling block viewer with real-time updates
- **Search**: Find contracts, inscriptions, or blocks by ID/height/hash
- **Dark/Light Mode**: Automatic theme detection with manual toggle
- **Responsive Design**: Mobile-friendly interface with QuantumCSS

### Backend (Go)
- **REST API**: Fast HTTP server with CORS support
- **Funding Builder**: PSBT construction for payouts and raise-fund workflows
- **Oracle Reconcile**: Match on-chain funding/commitment outputs to contracts
- **Commitment Sweeps**: Build and broadcast P2WSH sweep transactions
- **Inscription Storage**: Persistent storage of inscription requests and images
- **Built-in Autonomous Agents**: Native Go implementation of watcher + worker orchestration (migrated from the original Python `starlight.agents`). Auto-detects and drives external AI coding CLIs for creating proposals, executing tasks, auditing, and handling rework. See "Built-in Autonomous Agents" section below.

## 🛠 Tech Stack

### Frontend
- **React 19** - Modern React with hooks and JSX
- **@howssatoshi/quantumcss** - Utility-first CSS (QuantumCSS)
- **Lucide React** - Beautiful icons
- **QRCode Canvas** - QR code generation
- **Vite** - Fast build tooling (not CRA)

### Backend
- **Go 1.25.7** - High-performance backend (single-binary model)
- **net/http** - HTTP server + http.ServeMux
- **encoding/json** - JSON handling
- **modernc.org/sqlite** (pure-Go) + pgx - Storage (CGO disabled)
- **File I/O + IPFS** - Persistent + distributed storage

## 🚀 Installation

### One-Liner Binary Install (Recommended)

```bash
curl -fsSL https://github.com/macroadster/stargate/releases/latest/download/stargate-$(uname -s | tr '[:upper:]' '[:lower:]')-$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/').tar.gz | tar xz && sudo mv stargate-* /usr/local/bin/stargate
```

Then run:

```bash
stargate
```

Server starts on `http://localhost:3001` with SQLite storage. No Docker, Kubernetes, or Helm required. Available for Linux (amd64, arm64) and macOS (amd64, arm64).

### Prerequisites (Development)
- Node.js 18+ and npm
- Go 1.25+
- Git
- Docker (for unified image builds via `make docker`)

### Local Backend (no containers)
```bash
cd backend
go mod tidy
./run_dev.sh  # exports sensible defaults then runs `go run .`
```
Server starts on `http://localhost:3001`
- Metrics: `http://localhost:3001/metrics`

### Local Frontend (no containers)
```bash
cd frontend
npm install
npm start
```
App runs on `http://localhost:3000`

### API Docs
- Backend OpenAPI: `http://localhost:3001/api/docs/openapi.yaml` (load in Swagger UI/Insomnia); Swagger UI at `/api/docs/` and metrics at `/metrics`
- Starlight FastAPI (when running locally): `http://localhost:8080/docs` (private, used by Stargate)

## ⚙️ Optional Stego + IPFS Approval Flow

To embed a YAML manifest into the approved proposal image and publish it to IPFS,
enable the approval pipeline:

```bash
STARGATE_STEGO_APPROVAL_ENABLED=true
STARGATE_PROXY_BASE=http://localhost:8080  # starlight fastapi
STARGATE_API_KEY=...                       # if starlight requires auth
STARGATE_STEGO_METHOD=lsb                  # optional; defaults to lsb
STARGATE_STEGO_ISSUER=stargate-default     # optional; defaults to hostname
STARGATE_STEGO_INGEST_TIMEOUT_SEC=30
STARGATE_STEGO_INGEST_POLL_SEC=2
IPFS_API_URL=http://127.0.0.1:5001
```

## 📖 Usage

### Exploring Blocks
1. View the horizontal block scroller showing recent Bitcoin blocks
2. Click any block to see its inscriptions
3. Use search to find specific blocks by height or hash

### Viewing Inscriptions
1. Browse the inscription gallery in any block
2. Click "View Details" on any inscription
3. Explore tabs: Overview, Documentation, Transactions

### Funding Work
1. Open a smart contract and review tasks
2. Build a payout or raise-fund PSBT
3. Sign in your wallet and broadcast
4. Watch confirmations and commitment sweeps

### Search Functionality
- Search for inscription text or IDs
- Find blocks by height (e.g., `870000`) or hash
- Find contracts by ID

## 🔌 API Endpoints (selected; backend has 30+ routes including full MCP at /mcp/* and smart contract at /api/smart_contract/*)

### Core
- `GET /api/health` - Health check
- `POST /api/inscribe` - Create inscription/wish (JSON: message, image_base64, etc.)
- `GET /api/open-contracts` - Pending/open contracts

### Bitcoin / Data
- `GET /api/blocks` - Recent blocks
- `GET /bitcoin/v1/scan/transaction` - Scan tx for stego
- `GET /bitcoin/v1/info` - Scanner info

### MCP / Smart Contracts (main coordination)
- `GET /mcp/tools` , `POST /mcp/call` (tool-level auth for writes)
- `GET /mcp/docs` , `/mcp/openapi.json` (public)
- `GET /mcp/chat/stream` etc.

### Search
- `GET /api/search?q=...` - Search across inscriptions, txs, blocks, contracts, proposals

See `backend/docs/API_DOCUMENTATION.md` and `/api/docs` for full OpenAPI.

## 📁 Project Structure

```
stargate/
├── frontend/                # React + Vite (JSX)
│   ├── src/                 # .jsx sources
│   │   ├── App.jsx
│   │   ├── components/      # Reusable UI components
│   │   ├── pages/           # Route pages
│   │   ├── context/         # React context providers
│   │   ├── hooks/           # Custom React hooks
│   │   └── utils/           # Utilities
│   ├── public/
│   ├── vite.config.js
│   └── index.html
├── backend/                 # Go (single-binary, CGO=0 + pure sqlite)
│   ├── stargate_backend.go    # Main entry point
│   ├── agents/                # Built-in autonomous agent orchestration (Watcher/Worker + AutoDetectExecutor)
│   ├── api/                   # REST API handlers
│   ├── bitcoin/               # Bitcoin data + scanner clients
│   ├── cmd/                   # CLI utilities (migration, etc.)
│   ├── container/             # Dependency injection container
│   ├── core/                  # Core domain logic
│   ├── docs/                  # API documentation (OpenAPI)
│   ├── handlers/              # HTTP request handlers
│   ├── mcp/                   # MCP protocol server (tool-based auth)
│   ├── middleware/            # HTTP middleware (auth, CORS, logging)
│   ├── models/                # Data models
│   ├── scripts/               # Build/development scripts
│   ├── security/              # Auth, API keys, crypto
│   ├── services/              # Business logic services
│   ├── starlight/             # Starlight ML scanner client
│   ├── stego/                 # Steganography encoding/decoding
│   ├── storage/               # Storage abstraction (SQLite, PG)
│   ├── go.mod
│   └── ...
├── Dockerfile (unified)
├── Makefile
└── README.md
```

## 🔒 Security

- Input validation and sanitization
- Secure file upload handling
- CORS protection
- No sensitive data storage

## 🤝 Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests if applicable
5. Submit a pull request

## 📄 License

MIT License - see LICENSE file for details

## 🙏 Acknowledgments

- Bitcoin raw block data via local parser + mempool client (no longer Hiro)
- [Mempool.space](https://mempool.space) inspiration for explorer UX
- [QuantumCSS](https://github.com/howssatoshi/quantumcss) for styling
- [Lucide](https://lucide.dev) for icons
- [modernc.org/sqlite](https://modernc.org/sqlite) for pure-Go embedded DB

---

**Stargate** - Bitcoin-native coordination for wishes, work, and verifiable proofs ✨</content>
<parameter name="filePath">README.md
