# Stargate — Bitcoin-Native Work, Funding, and Proofs

Stargate is a Bitcoin-native coordination layer for turning human wishes and ideas into funded work with verifiable, tamper-evident outcomes. It combines steganographic intent binding (via Starlight), on-chain commitments via hashlocks, PSBT funding flows, and distributed oracle-style reconciliation across independent nodes.

**Core philosophy**: Digital goods are effectively free to copy. The scarce resources are human creative labor and the ongoing effort to keep high-signal artifacts discoverable and alive. Stargate exists to make the *intent* behind creative work hard to revise after the fact, to let buyers pay creators directly for labor, and to give people who run voluntary Starlight nodes a useful personal archive of tools and artifacts — not to extract mandatory platform rents.

Stargate treats Bitcoin as the final settlement layer and historical witness, not as a platform to replace. Independent Starlight instances (connected via IPFS pubsub) act as voluntary, low-energy operators that scan blocks, reconcile steganographic inscriptions with prior ingestions, publish approved artifacts to IPFS, and can compete for tiny optional "lottery" donations via hashlock sweeps. The real value of running a node is the local collection of high-quality AI-generated creative work and the full set of Starlight tools (scanner, stego, Bitcoin coordination) — not the micro-donations.

The system is intentionally tolerant of unswept hashlocks and centralized hosting in the short term. Diversity of home-hosted instances with different local tools is preferred over one canonical powerful server in the long term. Stargate is evolving toward a single-binary distribution model that lets people run useful nodes without a full microservices stack.

## How it works (high level)

```text
Human Wish → Stego Image (Starlight) + Visible Pixel Hash
             ↓
     Ingestion Record (local + IPFS)
             ↓
     Funding (PSBT with optional hashlock commitment)
             | 
             +--> On-chain (P2WSH hashlock using wish image or later product image)
             |
             v
     Block Monitor (witness reconciliation)
             • Scans new blocks for steganographic inscriptions
             • Matches visible_pixel_hash / witness data against known ingestions
             • Confirms "this buyer paid creator for this exact intent"
             ↓
     Submission + Product Image (tasks inscribed via steganography)
             ↓
     IPFS Publication + Distributed Witnessing across nodes
             ↓
     Optional hashlock sweep (lottery for node operators)
```

The final delivered artifact (the "product image") can itself become the source of a future hashlock commitment when no separate donation was funded at the start. This creates a chain where completed work carries forward the ability to incentivize the preservation network.

## Architecture & Philosophy (detailed)

### Voluntary Operators and the Real Value of a Node

People who run Starlight instances are not primarily chasing the tiny hashlock donations. The real prize is the content and tooling that becomes locally available:

- A personal, searchable archive of high-signal AI-generated creative work (games, tools, writing, art, experiments) that has passed through the steganographic approval process.
- The full local capability to create new wishes, run the Starlight scanner, embed manifests, coordinate funding via PSBTs, publish to IPFS, and reconcile on-chain events.
- Participation in a distributed memory system that makes it expensive to later pretend an original creative request was different from what was actually asked.

Running a node is an act of tool collection and cultural preservation. The small donation sweep is a stochastic micro-reward (a "lottery") that favors the instance closest to the original creation event because it has the artifacts locally before IPFS propagation reaches others. Anyone can win, but the originating node has a natural first-mover advantage. If the donations are never swept, that is philosophically acceptable — it would only indicate that Bitcoin itself had lost all value.

### Hashlock Commitments and the "Product Image" Rule

At funding time, a small P2WSH hashlock output can be created (`OP_SHA256 <SHA256(preimage)> OP_EQUAL`). The preimage is normally derived from the visible pixel hash of the original wish image (when a donation component was explicitly funded).

When the payer provides **no extra donation** (only payment for labor), the commitment — if still created — should use the **final delivered product image** (the artifact the contractor actually produced, with tasks and context inscribed via steganography) as the basis for the hashlock. This keeps the incentive mechanism aligned with completed work rather than only with the initial request.

See open beads tasks for the implementation work required in the PSBT builder and funding handler.

### Witness Reconciliation (the "Signature" Layer)

When a Bitcoin block is processed:

- The BlockMonitor scans inscriptions for steganographic content.
- It also scans transaction witness data for hashes matching known ingestion records (`matchWitnessHash`).
- A successful match between an on-chain inscription/witness hash and a prior ingestion (via `visible_pixel_hash` + stego manifest) creates a distributed, independently verifiable record that "this buyer paid this creator for this specific intent at this block height."

Any Starlight instance that processes the block can perform this reconciliation. This is how the network stays in sync without a single source of truth.

### IPFS as Primary Distribution, Bitcoin as Slow Expensive Memory

Approved artifacts (wish images and final product images) are published to IPFS. Nodes share them via pubsub. Bitcoin is used for the high-integrity, hard-to-revise commitments and the witness reconciliation — not as the primary hosting or distribution layer. People who want something to survive long-term are expected to pin it on IPFS themselves.

### Single-Binary Direction and Future Provenance Ideas

Stargate is moving toward a single-binary distribution model (downloadable, runnable binary without requiring the full microservices stack of Go backend + Python Starlight + Postgres + IPFS node). This lowers the barrier for home operators.

**Migrating from Postgres to SQLite**: If you have an existing deployment using `STARGATE_PG_DSN`, use the dedicated migration utility:

    cd backend
    make build-migrate
    ./bin/migrate-pg-to-sqlite --pg-dsn "$STARGATE_PG_DSN" --target-dir ./data/sqlite --dry-run   # preview
    ./bin/migrate-pg-to-sqlite --pg-dsn "$STARGATE_PG_DSN" --target-dir ./data/sqlite             # execute

Then set `STARGATE_STORAGE=sqlite` (or simply remove the PG DSN env var) and restart. The tool correctly converts JSONB→TEXT, TEXT[] skills→comma strings, TIMESTAMPTZ→RFC3339 text, etc. See `bin/migrate-pg-to-sqlite --help`.

A related future direction under discussion: at submission time, tar the relevant sandbox/artifact state and embed the SHA256 of that tarball into the product image via steganography. This would give AI-generated binaries and complex deliverables a strong, bit-level verifiable provenance chain ("this exact bundle was produced for this specific task").

### Relationship with Starlight (the ML scanner)

Starlight (the Python AI steganalysis system) is the approval oracle. Multiple detection methods (alpha, LSB, palette, EXIF, EOI, raw) are used in an ensemble. The goal is not perfect secrecy but making it expensive to alter the embedded wish or task context without visibly destroying the image. The Go backend (Stargate) consumes Starlight as a service for scanning blocks and individual images during ingestion and reconciliation.

## Current Status & Direction

- The system is fully functional for solo or small-group use.
- Multiple independent instances can coexist and eventually see the same IPFS content.
- The donation mechanism is intentionally voluntary and non-rent-seeking.
- Work is ongoing to better align the hashlock preimage source with the "product image" rule and to improve single-binary packaging.

## 🌟 Features

### Frontend (React)
- **Smart Contract Viewer**: Detailed modal views for proposals, tasks, and funding proofs
- **Task Discovery**: Portal for AI and humans to claim, submit, and verify work
- **Funding & PSBTs**: Build payout or raise-fund PSBTs with multi-payer support
- **Inscription Viewer**: Grid view of inscriptions with metadata
- **Block Explorer**: Horizontal scrolling block viewer with real-time updates
- **Search**: Find contracts, inscriptions, or blocks by ID/height/hash
- **Dark/Light Mode**: Automatic theme detection with manual toggle
- **Responsive Design**: Mobile-friendly interface with Tailwind CSS

### Backend (Go)
- **REST API**: Fast HTTP server with CORS support
- **Funding Builder**: PSBT construction for payouts and raise-fund workflows
- **Oracle Reconcile**: Match on-chain funding/commitment outputs to contracts
- **Commitment Sweeps**: Build and broadcast P2WSH sweep transactions
- **Inscription Storage**: Persistent storage of inscription requests and images

## 🛠 Tech Stack

### Frontend
- **React 19** - Modern React with hooks
- **Tailwind CSS** - Utility-first CSS framework
- **Lucide React** - Beautiful icons
- **QRCode Canvas** - QR code generation
- **Create React App** - Build tooling

### Backend
- **Go 1.21** - High-performance backend
- **net/http** - HTTP server
- **encoding/json** - JSON handling
- **File I/O** - Persistent storage

## 🚀 Installation

### Prerequisites
- Node.js 18+ and npm
- Go 1.21+
- Git

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

## 🔌 API Endpoints

### Blocks
- `GET /api/blocks` - Recent Bitcoin blocks

### Inscriptions
- `GET /api/inscriptions` - Recent Ordinal inscriptions
- `GET /api/inscription/{id}/content` - Inscription image content

### Inscription Creation
- `POST /api/inscribe` - Create new inscription (multipart form)
- `GET /api/open-contracts` - View open contracts

### Search
- `GET /api/search?q=query` - Search inscriptions, transactions, blocks, contracts, and proposals

## 📁 Project Structure

```
starlight/
├── frontend/                # React application
│   ├── src/
│   │   ├── App.js           # Main application component
│   │   ├── index.js         # React entry point
│   │   └── index.css        # Global styles
│   └── public/
├── backend/                 # Go server
│   ├── stargate_backend.go  # Server implementation
│   ├── go.mod               # Go modules
│   └── uploads/             # Stored inscription images
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

- [Hiro Systems](https://hiro.so) for Ordinals API
- [Mempool.space](https://mempool.space) for Bitcoin data
- [Tailwind CSS](https://tailwindcss.com) for styling
- [Lucide](https://lucide.dev) for icons

---

**Stargate** - Illuminating the world of Bitcoin Ordinals ✨</content>
<parameter name="filePath">README.md
