package main

import (
	"log"
	"net/http"
	"time"

	"stargate-backend/api"
	"stargate-backend/bitcoin"
	"stargate-backend/container"
	"stargate-backend/middleware"
	"stargate-backend/storage"
)

func main() {
	// Initialize dependency container
	container := container.NewContainer()

	// Set up middleware chain
	mux := http.NewServeMux()

	// Apply middleware to all routes
	handler := middleware.Recovery(
		middleware.Logging(
			middleware.CORS(
				middleware.Timeout(30 * time.Second)(
					setupRoutes(mux, container),
				),
			),
		),
	)

	log.Println("Server starting on :3001")
	log.Println("Frontend available at: http://localhost:3001")
	log.Println("Stargate API endpoints at: http://localhost:3001/api/")
	log.Println("Bitcoin steganography API at: http://localhost:3001/bitcoin/v1/")
	log.Println("Smart contract steganography at: http://localhost:3001/api/contract-stego/")
	log.Println("Proxy to steganography API (port 8080) at: http://localhost:3001/stego/")

	log.Fatal(http.ListenAndServe(":3001", handler))
}

func setupRoutes(mux *http.ServeMux, container *container.Container) http.Handler {
	// Health endpoints
	mux.HandleFunc("/api/health", container.HealthHandler.HandleHealth)

	// Inscription endpoints
	mux.HandleFunc("/api/inscriptions", container.InscriptionHandler.HandleGetInscriptions)
	mux.HandleFunc("/api/inscribe", container.InscriptionHandler.HandleCreateInscription)
	mux.HandleFunc("/api/pending-transactions", container.InscriptionHandler.HandleGetInscriptions)

	// Block endpoints
	mux.HandleFunc("/api/blocks", container.BlockHandler.HandleGetBlocks)

	// Smart contract endpoints
	mux.HandleFunc("/api/smart-contracts", container.SmartContractHandler.HandleGetContracts)
	mux.HandleFunc("/api/contract-stego", container.SmartContractHandler.HandleGetContract)
	mux.HandleFunc("/api/contract-stego/create", container.SmartContractHandler.HandleCreateContract)

	// Search endpoints
	mux.HandleFunc("/api/search", container.SearchHandler.HandleSearch)

	// QR code endpoints
	mux.HandleFunc("/api/qrcode", container.QRCodeHandler.HandleGenerateQRCode)

	// Proxy endpoints
	mux.HandleFunc("/stego/", container.ProxyHandler.HandleProxy)
	mux.HandleFunc("/analyze/", container.ProxyHandler.HandleProxy)
	mux.HandleFunc("/generate/", container.ProxyHandler.HandleProxy)

	// Serve uploaded files
	mux.Handle("/uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir("uploads/"))))

	// Serve frontend files
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.ServeFile(w, r, "../index.html")
			return
		}
		if r.URL.Path == "/app.js" {
			http.ServeFile(w, r, "../app.js")
			return
		}
		http.NotFound(w, r)
	})

	// Enhanced data API endpoints (keep existing functionality)
	dataAPI := api.NewDataAPI(
		storage.NewDataStorage("data"),
		bitcoin.NewBlockMonitorWithStorage(
			bitcoin.NewBitcoinNodeClient("https://blockstream.info/api"),
			storage.NewDataStorage("data"),
		),
		bitcoin.NewBitcoinAPI(),
	)

	mux.HandleFunc("/api/data/block/", dataAPI.HandleGetBlockData)
	mux.HandleFunc("/api/data/blocks", dataAPI.HandleGetRecentBlocks)
	mux.HandleFunc("/api/data/stats", dataAPI.HandleGetSteganographyStats)
	mux.HandleFunc("/api/data/updates", dataAPI.HandleRealtimeUpdates)
	mux.HandleFunc("/api/data/scan", dataAPI.HandleScanBlockOnDemand)
	mux.HandleFunc("/api/data/block-images", dataAPI.HandleGetBlockImages)

	// Bitcoin steganography scanning endpoints
	bitcoinAPI := bitcoin.NewBitcoinAPI()
	mux.HandleFunc("/bitcoin/v1/health", bitcoinAPI.HandleHealth)
	mux.HandleFunc("/bitcoin/v1/info", bitcoinAPI.HandleInfo)
	mux.HandleFunc("/bitcoin/v1/scan/transaction", bitcoinAPI.HandleScanTransaction)
	mux.HandleFunc("/bitcoin/v1/scan/image", bitcoinAPI.HandleScanImage)
	mux.HandleFunc("/bitcoin/v1/scan/block", bitcoinAPI.HandleBlockScan)
	mux.HandleFunc("/bitcoin/v1/extract", bitcoinAPI.HandleExtract)
	mux.HandleFunc("/bitcoin/v1/transaction/", bitcoinAPI.HandleGetTransaction)

	return mux
}
