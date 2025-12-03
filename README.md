# Stargate - Bitcoin Ordinals Explorer

A modern, full-stack web application for exploring Bitcoin Ordinals inscriptions, blocks, and smart contracts. Built with React frontend and Go backend.

## 🌟 Features

### Frontend (React)
- **Block Explorer**: Horizontal scrolling block viewer with real-time updates
- **Inscription Gallery**: Grid view of Ordinal inscriptions with metadata
- **Smart Contract Viewer**: Detailed modal views for inscriptions with tabs (Overview, Documentation, Transactions)
- **Inscription Creator**: Step-by-step workflow to create new inscriptions with QR payment
- **Advanced Search**: Search inscriptions by text/ID and blocks by height/hash
- **Dark/Light Mode**: Automatic theme detection with manual toggle
- **Responsive Design**: Mobile-friendly interface with Tailwind CSS

### Backend (Go)
- **REST API**: Fast HTTP server with CORS support
- **Inscription Storage**: Persistent storage of inscription requests and images
- **Blockchain Integration**: Proxy API calls to Hiro Ordinals and Mempool.space
- **Search Engine**: Full-text search across inscriptions and blocks
- **File Management**: Secure image upload and storage

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

## 📖 Usage

### Exploring Blocks
1. View the horizontal block scroller showing recent Bitcoin blocks
2. Click any block to see its inscriptions
3. Use search to find specific blocks by height or hash

### Viewing Inscriptions
1. Browse the inscription gallery in any block
2. Click "View Details" on any inscription
3. Explore tabs: Overview, Documentation, Transactions

### Creating Inscriptions
1. Click "Inscribe" in the header
2. Upload an image and add text
3. Follow the payment workflow with QR code
4. View your pending inscription in the future block

### Search Functionality
- Search for inscription text or IDs
- Find blocks by height (e.g., `870000`) or hash
- Special searches: "block" shows recent blocks

## 🔌 API Endpoints

### Blocks
- `GET /api/blocks` - Recent Bitcoin blocks

### Inscriptions
- `GET /api/inscriptions` - Recent Ordinal inscriptions
- `GET /api/inscription/{id}/content` - Inscription image content

### Inscription Creation
- `POST /api/inscribe` - Create new inscription (multipart form)
- `GET /api/pending-transactions` - View pending inscriptions

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

## 🎨 UI/UX Features

- **Horizontal Block Scroller**: Innovative block browsing
- **Modal-Based Details**: Rich inscription information
- **Payment Integration**: QR codes for Bitcoin payments
- **Theme Support**: Seamless dark/light mode
- **Copy to Clipboard**: Easy ID sharing
- **Drag & Drop**: Intuitive file uploads

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
