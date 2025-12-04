package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"stargate-backend/api"
	"stargate-backend/bitcoin"
	"stargate-backend/container"
	"stargate-backend/middleware"
	"stargate-backend/storage"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func contentTypeForFormat(format string) string {
	switch strings.ToLower(format) {
	case "png":
		return "image/png"
	case "jpg", "jpeg":
		return "image/jpeg"
	case "gif":
		return "image/gif"
	case "webp":
		return "image/webp"
	case "txt", "text":
		return "text/plain"
	default:
		return "application/octet-stream"
	}
}

// findImagePath searches for an image file within the blocks directory using the real block hash folder.
func findImagePath(height string, filename string) (string, bool) {
	baseDir := os.Getenv("BLOCKS_DIR")
	if baseDir == "" {
		baseDir = "blocks"
	}

	// Try explicit directory pattern first (height_*)
	pattern := filepath.Join(baseDir, fmt.Sprintf("%s_*", height), "images", filename)
	matches, err := filepath.Glob(pattern)
	if err == nil && len(matches) > 0 {
		return matches[0], true
	}

	// Fallback to legacy pattern with zero hash suffix
	legacy := filepath.Join(baseDir, fmt.Sprintf("%s_00000000", height), "images", filename)
	if info, err := os.Stat(legacy); err == nil && !info.IsDir() {
		return legacy, true
	}

	return "", false
}

func main() {
	// Initialize dependency container
	container := container.NewContainer()

	// Set up middleware chain
	mux := http.NewServeMux()

	// Apply middleware to all routes
	handler := middleware.Recovery(
		middleware.Logging(
			middleware.SecurityHeaders(
				middleware.CORS(
					middleware.Timeout(30 * time.Second)(
						setupRoutes(mux, container),
					),
				)),
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
	mux.HandleFunc("/api/docs", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/api/docs/", http.StatusFound)
	})
	mux.Handle("/api/docs/", http.StripPrefix("/api/docs/", http.FileServer(http.Dir("./docs"))))
	mux.Handle("/metrics", promhttp.Handler())

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

	// Ingestion endpoints
	mux.HandleFunc("/api/ingest-inscription", container.IngestionHandler.HandleIngest)
	mux.HandleFunc("/api/ingest-inscription/", container.IngestionHandler.HandleGetIngestion)

	// Search endpoints
	mux.HandleFunc("/api/search", container.SearchHandler.HandleSearch)

	// QR code endpoints
	mux.HandleFunc("/api/qrcode", container.QRCodeHandler.HandleGenerateQRCode)

	// Proxy endpoints
	mux.HandleFunc("/stego/", container.ProxyHandler.HandleProxy)
	mux.HandleFunc("/analyze/", container.ProxyHandler.HandleProxy)
	mux.HandleFunc("/generate/", container.ProxyHandler.HandleProxy)

	// Serve uploaded files
	uploadsDir := os.Getenv("UPLOADS_DIR")
	if uploadsDir == "" {
		uploadsDir = "/data/uploads"
	}
	_ = os.MkdirAll(uploadsDir, 0755)
	mux.Handle("/uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir(uploadsDir))))

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

	// Bitcoin API for scanning
	bitcoinAPI := bitcoin.NewBitcoinAPI()

	// Enhanced data API endpoints (keep existing functionality)
	dataStorage := container.DataStorage
	blockMonitor := bitcoin.NewBlockMonitorWithStorageAndAPI(
		bitcoin.NewBitcoinNodeClient("https://blockstream.info/api"),
		dataStorage,
		bitcoinAPI,
	)

	// Pre-cache historical blocks (with rate limiting)
	go func() {
		log.Println("Pre-caching historical Bitcoin blocks...")
		// Start with most important blocks first
		priorityBlocks := []int64{0, 174923, 481824}
		otherBlocks := []int64{1, 210000, 420000, 630000, 709632}

		// Cache priority blocks immediately
		for _, height := range priorityBlocks {
			log.Printf("Caching priority historical block %d...", height)
			if err := blockMonitor.ProcessBlock(height); err != nil {
				log.Printf("Failed to cache block %d: %v", height, err)
			} else {
				log.Printf("Successfully cached block %d", height)
			}
			time.Sleep(2 * time.Second) // Longer delay for priority blocks
		}

		// Cache other blocks with longer delays
		for _, height := range otherBlocks {
			log.Printf("Caching historical block %d...", height)
			if err := blockMonitor.ProcessBlock(height); err != nil {
				log.Printf("Failed to cache block %d: %v", height, err)
			} else {
				log.Printf("Successfully cached block %d", height)
			}
			time.Sleep(5 * time.Second) // Much longer delay to avoid rate limits
		}

		log.Println("Historical blocks caching completed")
	}()

	// Start the block monitor in background
	go func() {
		if err := blockMonitor.Start(); err != nil {
			log.Printf("Failed to start block monitor: %v", err)
		} else {
			log.Println("Block monitor started successfully")
		}
	}()

	dataAPI := api.NewDataAPI(
		dataStorage,
		blockMonitor,
		bitcoinAPI,
	)

	mux.HandleFunc("/api/data/block/", dataAPI.HandleGetBlockData)
	mux.HandleFunc("/api/data/blocks", dataAPI.HandleGetRecentBlocks)
	mux.HandleFunc("/api/data/block-summaries", dataAPI.HandleGetBlockSummaries)
	mux.HandleFunc("/api/data/block-inscriptions/", dataAPI.HandleGetBlockInscriptionsPaginated)
	mux.HandleFunc("/api/data/stats", dataAPI.HandleGetSteganographyStats)
	mux.HandleFunc("/api/data/updates", dataAPI.HandleRealtimeUpdates)
	mux.HandleFunc("/api/data/scan", dataAPI.HandleScanBlockOnDemand)
	mux.HandleFunc("/api/data/block-images", dataAPI.HandleGetBlockImages)
	mux.HandleFunc("/api/block-images", dataAPI.HandleGetBlockImages)
	mux.HandleFunc("/api/stego/callback", dataAPI.HandleStegoCallback)

	// Serve block images
	mux.HandleFunc("/api/block-image/", func(w http.ResponseWriter, r *http.Request) {
		// Extract height and filename from URL path
		pathParts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/block-image/"), "/")
		if len(pathParts) < 2 {
			http.Error(w, "Invalid URL format", http.StatusBadRequest)
			return
		}

		height := pathParts[0]
		filename := pathParts[1]

		// Try to locate the image on disk (blocks/<height>_<hash>/images/<filename>)
		if fsPath, ok := findImagePath(height, filename); ok {
			log.Printf("Serving image from filesystem: %s", fsPath)
			http.ServeFile(w, r, fsPath)
			return
		}

		// Fallback to Postgres storage: delegate to data API to build an in-memory response
		h, _ := strconv.ParseInt(height, 10, 64)
		if cache, err := dataStorage.GetBlockData(h); err == nil {
			if block, ok := cache.(*storage.BlockDataCache); ok {
				for _, img := range block.Images {
					if img.FileName == filename {
						if len(img.Data) == 0 {
							// Inline bytes not present (e.g., DB-only storage)
							http.NotFound(w, r)
							return
						}
						w.Header().Set("Content-Type", contentTypeForFormat(img.Format))
						_, _ = w.Write(img.Data)
						return
					}
				}
			}
		}

		http.NotFound(w, r)
	})

	// Bitcoin steganography scanning endpoints
	mux.HandleFunc("/bitcoin/v1/health", bitcoinAPI.HandleHealth)
	mux.HandleFunc("/bitcoin/v1/info", bitcoinAPI.HandleInfo)
	mux.HandleFunc("/bitcoin/v1/scan/transaction", bitcoinAPI.HandleScanTransaction)
	mux.HandleFunc("/bitcoin/v1/scan/image", bitcoinAPI.HandleScanImage)
	mux.HandleFunc("/bitcoin/v1/scan/block", bitcoinAPI.HandleBlockScan)
	mux.HandleFunc("/bitcoin/v1/extract", bitcoinAPI.HandleExtract)
	mux.HandleFunc("/bitcoin/v1/transaction/", bitcoinAPI.HandleGetTransaction)

	return mux
}
