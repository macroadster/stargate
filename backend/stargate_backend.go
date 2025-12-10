package main

import (
	"context"
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
	"stargate-backend/mcp"
	"stargate-backend/middleware"
	"stargate-backend/services"
	"stargate-backend/storage"

	"github.com/mark3labs/mcp-go/server"
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

// initializeMCPServer sets up the MCP server with proper store selection
func initializeMCPServer() *mcp.MCPServer {
	// Load MCP configuration
	storeDriver := os.Getenv("MCP_STORE_DRIVER")
	if storeDriver == "" {
		storeDriver = "memory"
	}

	pgDsn := os.Getenv("MCP_PG_DSN")
	apiKey := os.Getenv("MCP_API_KEY")

	// TTL configuration
	ttlHours := 72
	if raw := os.Getenv("MCP_DEFAULT_CLAIM_TTL_HOURS"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			ttlHours = v
		}
	}
	claimTTL := time.Duration(ttlHours) * time.Hour

	// Seed configuration
	seed := true
	if raw := os.Getenv("MCP_SEED_FIXTURES"); raw != "" {
		if v, err := strconv.ParseBool(raw); err == nil {
			seed = v
		}
	}

	// Initialize store based on driver
	var store mcp.Store
	var err error
	var ingestionSvc *services.IngestionService

	switch storeDriver {
	case "postgres":
		if pgDsn == "" {
			log.Printf("MCP_PG_DSN not set, falling back to memory store")
			store = mcp.NewMemoryStore(claimTTL)
		} else {
			store, err = mcp.NewPGStore(context.Background(), pgDsn, claimTTL, seed)
			if err != nil {
				log.Printf("failed to connect to PostgreSQL (%v), falling back to memory store", err)
				store = mcp.NewMemoryStore(claimTTL)
			} else {
				// PostgreSQL connected successfully, try to create ingestion service
				if svc, serr := services.NewIngestionService(pgDsn); serr != nil {
					log.Printf("ingestion service unavailable for proposal creation: %v", serr)
				} else {
					ingestionSvc = svc
				}
			}
		}
	default:
		store = mcp.NewMemoryStore(claimTTL)
	}

	// Log the actual store type being used
	actualStoreType := "memory"
	if err == nil && pgDsn != "" {
		actualStoreType = "postgres"
	}
	log.Printf("MCP server initialized with %s store (requested: %s)", actualStoreType, storeDriver)

	return mcp.NewMCPServer(store, apiKey, ingestionSvc)
}

// startMCPServices starts background services for MCP when using PostgreSQL
func startMCPServices() {
	storeDriver := os.Getenv("MCP_STORE_DRIVER")
	if storeDriver != "postgres" {
		return
	}

	pgDsn := os.Getenv("MCP_PG_DSN")
	if pgDsn == "" {
		log.Printf("MCP_PG_DSN not set, skipping MCP background services")
		return
	}

	// Create a temporary store for sync services
	ttlHours := 72
	if raw := os.Getenv("MCP_DEFAULT_CLAIM_TTL_HOURS"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			ttlHours = v
		}
	}
	claimTTL := time.Duration(ttlHours) * time.Hour

	store, err := mcp.NewPGStore(context.Background(), pgDsn, claimTTL, false)
	if err != nil {
		log.Printf("failed to create store for MCP services (%v), skipping background services", err)
		return
	}

	// Start ingestion -> MCP sync
	if os.Getenv("MCP_ENABLE_INGEST_SYNC") != "false" {
		syncInterval := 30 * time.Second
		if raw := os.Getenv("MCP_INGEST_SYNC_INTERVAL_SEC"); raw != "" {
			if v, err := strconv.Atoi(raw); err == nil && v > 0 {
				syncInterval = time.Duration(v) * time.Second
			}
		}

		if err := mcp.StartIngestionSync(context.Background(), pgDsn, store, syncInterval); err != nil {
			log.Printf("ingestion sync disabled (init error): %v", err)
		} else {
			log.Printf("ingestion sync enabled (interval=%s)", syncInterval)
		}
	}

	// Start funding proof refresher
	if os.Getenv("MCP_ENABLE_FUNDING_SYNC") != "false" {
		fundingInterval := 60 * time.Second
		if raw := os.Getenv("MCP_FUNDING_SYNC_INTERVAL_SEC"); raw != "" {
			if v, err := strconv.Atoi(raw); err == nil && v > 0 {
				fundingInterval = time.Duration(v) * time.Second
			}
		}

		fundingProvider := "mock"
		if env := os.Getenv("MCP_FUNDING_PROVIDER"); env != "" {
			fundingProvider = env
		}
		fundingAPIBase := "https://blockstream.info/api"
		if env := os.Getenv("MCP_FUNDING_API_BASE"); env != "" {
			fundingAPIBase = env
		}

		provider := mcp.NewFundingProvider(fundingProvider, fundingAPIBase)
		if err := mcp.StartFundingSync(context.Background(), store, provider, fundingInterval); err != nil {
			log.Printf("funding sync disabled (init error): %v", err)
		} else {
			log.Printf("funding sync enabled (interval=%s)", fundingInterval)
		}
	}
}

func main() {
	log.Println("=== STARTING STARGATE BACKEND ===")

	// Check what modes to run
	runHTTP := os.Getenv("STARGATE_MODE") != "mcp-only"
	runMCP := os.Getenv("STARGATE_MODE") != "http-only"

	// If neither specified, run both (default behavior)
	if os.Getenv("STARGATE_MODE") == "" {
		runHTTP = true
		runMCP = true
	}

	// Start HTTP server in background if requested
	if runHTTP {
		go runHTTPServer()
	}

	// Start MCP server in foreground if requested
	if runMCP {
		runMCPServer()
		return // MCP server runs in foreground
	}

	// If only HTTP mode, wait indefinitely
	if runHTTP && !runMCP {
		select {} // Block forever
	}
}

func runMCPServer() {
	log.Println("=== STARTING STARGATE MCP SERVER ===")

	// Initialize MCP server
	mcpServer := initializeMCPServer()
	if mcpServer == nil {
		log.Fatal("Failed to initialize MCP server")
	}

	// Start MCP background services if using PostgreSQL AND HTTP server is not also running
	if os.Getenv("STARGATE_MODE") != "http-only" && os.Getenv("STARGATE_MODE") != "both" {
		startMCPServices()
	} else {
		log.Println("MCP background services skipped (handled by HTTP process)")
	}

	log.Println("Stargate MCP server starting on stdio transport")
	log.Printf("Server: Stargate MCP Server v1.0.0")

	// Start MCP server using stdio transport
	if err := server.ServeStdio(mcpServer.GetMCPServer()); err != nil {
		log.Fatalf("MCP server error: %v", err)
	}
}

func runHTTPServer() {
	log.Println("=== STARTING STARGATE HTTP SERVER ===")

	// Initialize dependency container
	container := container.NewContainer()

	// Initialize MCP server (for background services, but not HTTP routes)
	mcpServer := initializeMCPServer()

	// Start MCP background services if using PostgreSQL AND MCP server is not running separately
	if os.Getenv("STARGATE_MODE") != "mcp-only" && os.Getenv("STARGATE_MODE") != "both" {
		startMCPServices()
	} else {
		log.Println("MCP background services skipped (will be handled by separate MCP process)")
	}

	// Set up middleware chain
	mux := http.NewServeMux()

	// Apply middleware to all routes
	handler := middleware.Recovery(
		middleware.Logging(
			middleware.SecurityHeaders(
				middleware.CORS(
					middleware.Timeout(30 * time.Second)(
						setupRoutes(mux, container, mcpServer),
					),
				)),
		),
	)

	// Determine HTTP port (allow override when both modes running)
	httpPort := os.Getenv("STARGATE_HTTP_PORT")
	if httpPort == "" {
		httpPort = "3001"
	}

	log.Printf("Server starting on :%s", httpPort)
	log.Printf("Frontend available at: http://localhost:%s", httpPort)
	log.Printf("Stargate API endpoints at: http://localhost:%s/api/", httpPort)
	log.Printf("Bitcoin steganography API at: http://localhost:%s/bitcoin/v1/", httpPort)
	log.Printf("Smart contract steganography at: http://localhost:%s/api/contract-stego/", httpPort)
	log.Printf("Proxy to steganography API (port 8080) at: http://localhost:%s/stego/", httpPort)
	if os.Getenv("STARGATE_MODE") == "" || os.Getenv("STARGATE_MODE") == "both" {
		log.Println("Note: MCP server running in same process")
	} else {
		log.Println("Note: MCP server available via separate process (STARGATE_MODE=mcp)")
	}

	log.Fatal(http.ListenAndServe(":"+httpPort, handler))
}

func setupRoutes(mux *http.ServeMux, container *container.Container, mcpServer *mcp.MCPServer) http.Handler {
	// Health endpoints
	mux.HandleFunc("/api/health", container.HealthHandler.HandleHealth)

	// API Documentation - includes Swagger UI
	mux.HandleFunc("/api/docs", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/api/docs/", http.StatusFound)
	})
	mux.Handle("/api/docs/", http.StripPrefix("/api/docs/", http.FileServer(http.Dir("./docs"))))

	// Swagger UI redirect
	mux.HandleFunc("/swagger", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/api/docs/swagger.html", http.StatusFound)
	})
	mux.HandleFunc("/swagger/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/api/docs/swagger.html", http.StatusFound)
	})

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
	mux.HandleFunc("/content/", dataAPI.HandleContent)

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

	// Note: New MCP server uses stdio transport, not HTTP routes
	// The MCP server will be available via stdio when run separately
	if mcpServer != nil {
		log.Printf("MCP server initialized (available via stdio transport)")
	}

	log.Printf("All routes registered, returning handler")
	return mux
}
