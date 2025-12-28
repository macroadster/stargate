# Stargate — Bitcoin-Native Work, Funding, and Proofs

Stargate is a Bitcoin-native workflow for turning ideas into funded work and verifiable outcomes. It combines on-chain commitments, PSBT funding flows, and oracle reconciliation so AI and humans can propose tasks, raise funds, and prove results—while Bitcoin remains the settlement layer.

Stargate treats Bitcoin as the final ledger, not the product. It encourages people to anchor useful, structured data—proposals, task budgets, funding proofs, and completion evidence—instead of low-value noise.

## How it works

```text
Proposal -> Tasks -> Funding (PSBT)
             |           |
             |           +--> On-chain funding + commitment (P2WSH)
             |                         |
             v                         v
        Submission              Oracle reconcile
             |                         |
             +---------> Confirmed + Sweep commitment
```

## Why it matters

- **Settlement, not replacement**: Bitcoin stays the base protocol; Stargate builds higher-level coordination on top.
- **Economic incentives over spam**: a free, utility-first path for creators to anchor meaningful data instead of low-value noise.
- **Long-lived knowledge**: smart-contract snapshots become training data for better AI alignment and human decision-making over time.

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
- `GET /api/search?q=query` - Search inscriptions and blocks

## 📁 Project Structure

```
starlight/
├── frontend/                 # React application
│   ├── src/
│   │   ├── App.js           # Main application component
│   │   ├── index.js         # React entry point
│   │   └── index.css        # Global styles
│   └── public/
├── backend/                  # Go server
│   ├── main.go              # Server implementation
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
