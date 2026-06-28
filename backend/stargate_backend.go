package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"stargate-backend/api"
	"stargate-backend/agents"
	"stargate-backend/bitcoin"
	"stargate-backend/container"
	"stargate-backend/core/smart_contract"
	"stargate-backend/handlers"
	"stargate-backend/storage/ipfs"
	"stargate-backend/mcp"
	"stargate-backend/middleware"
	scmiddleware "stargate-backend/app/smart_contract"
	"stargate-backend/services"
	"stargate-backend/starlight"
	"stargate-backend/storage"
	auth "stargate-backend/storage/auth"
	scstore "stargate-backend/storage/smart_contract"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

//go:embed assets/frontend/*
var frontendAssets embed.FS

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

// detectMimeType determines the MIME type based on file content and filename
// This is a simplified version of the inferMime function from api/data_api.go
func detectMimeType(content []byte, filename string) string {
	// Check filename-based detection first for common web assets
	// This is important because content detection often returns text/plain for JS/CSS
	lowerName := strings.ToLower(filename)
	switch {
	case strings.HasSuffix(lowerName, ".jpg"), strings.HasSuffix(lowerName, ".jpeg"):
		return "image/jpeg"
	case strings.HasSuffix(lowerName, ".png"):
		return "image/png"
	case strings.HasSuffix(lowerName, ".gif"):
		return "image/gif"
	case strings.HasSuffix(lowerName, ".webp"):
		return "image/webp"
	case strings.HasSuffix(lowerName, ".svg"):
		return "image/svg+xml"
	case strings.HasSuffix(lowerName, ".bmp"):
		return "image/bmp"
	case strings.HasSuffix(lowerName, ".html"), strings.HasSuffix(lowerName, ".htm"):
		return "text/html"
	case strings.HasSuffix(lowerName, ".json"):
		return "application/json"
	case strings.HasSuffix(lowerName, ".js"):
		return "text/javascript"
	case strings.HasSuffix(lowerName, ".css"):
		return "text/css"
	case strings.HasSuffix(lowerName, ".txt"), strings.HasSuffix(lowerName, ".md"):
		return "text/plain"
	}

	// Try to detect from content for unknown files
	if len(content) > 0 {
		sample := content
		if len(sample) > 512 {
			sample = sample[:512]
		}
		if detected := http.DetectContentType(sample); detected != "" && detected != "application/octet-stream" {
			return detected
		}
	}

	// Final content-based detection for common patterns
	if len(content) > 0 {
		trim := strings.TrimSpace(string(content))
		lower := strings.ToLower(trim)
		if strings.HasPrefix(lower, "<!doctype") || strings.HasPrefix(lower, "<html") {
			return "text/html"
		} else if strings.HasPrefix(lower, "<svg") {
			return "image/svg+xml"
		} else if strings.HasPrefix(lower, "{") || strings.HasPrefix(lower, "[") {
			// Simple JSON detection
			return "application/json"
		} else if isMostlyPrintable(trim) {
			return "text/plain"
		}
	}

	return "application/octet-stream"
}

// isMostlyPrintable checks if content is mostly printable text
func isMostlyPrintable(s string) bool {
	if len(s) == 0 {
		return false
	}
	printable := 0
	for _, r := range s {
		if r >= 32 && r <= 126 || r == '\n' || r == '\r' || r == '\t' {
			printable++
		}
	}
	return float64(printable)/float64(len(s)) > 0.8
}

// customUploadsHandler serves uploaded files with proper MIME type detection
// instead of relying on file extensions (hash-based filenames have no extensions)
func customUploadsHandler(uploadsDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Extract relative path from URL (after /uploads/)
		relPath := strings.TrimPrefix(r.URL.Path, "/uploads/")
		if relPath == "" || relPath == "." {
			// Serve directory index if needed, otherwise not found
			http.FileServer(http.Dir(uploadsDir)).ServeHTTP(w, r)
			return
		}

		// Construct full file path and clean it
		filePath := filepath.Join(uploadsDir, relPath)

		// Security check: ensure the cleaned path is still within uploads directory
		if !strings.HasPrefix(filepath.Clean(filePath), uploadsDir) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		// Check if file exists
		fileInfo, err := os.Stat(filePath)
		if os.IsNotExist(err) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		// If it's a directory, use standard FileServer for indexing
		if fileInfo.IsDir() {
			http.StripPrefix("/uploads/", http.FileServer(http.Dir(uploadsDir))).ServeHTTP(w, r)
			return
		}

		// Open file for streaming (avoids loading entire file into memory)
		file, err := os.Open(filePath)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		defer file.Close()

		// Read only first 512 bytes for MIME detection
		sample := make([]byte, 512)
		n, err := file.Read(sample)
		if err != nil && err != io.EOF {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		sample = sample[:n]

		// Detect MIME type using content-based detection (Stealth Design)
		mimeType := detectMimeType(sample, filepath.Base(relPath))
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}

		// Seek back to beginning for streaming
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		// Set headers
		w.Header().Set("Content-Type", mimeType)
		w.Header().Set("Cache-Control", "public, max-age=31536000")
		w.Header().Set("Content-Length", strconv.FormatInt(fileInfo.Size(), 10))

		// Stream file directly to response
		_, _ = io.Copy(w, file)
	}
}

// findImagePath searches for an image file within the blocks directory using the real block hash folder.
func findImagePath(height string, filename string) (string, bool) {
	baseDir := os.Getenv("BLOCKS_DIR")
	if baseDir == "" {
		baseDir = storage.DefaultPath("blocks")
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

// initializeMCPComponents is now a thin compatibility wrapper around the
// unified storage factory (Phase 7 cleanup).
//
// All storage decisions (MCP store, API keys, Ingestion, Data layer, caches)
// are made in one place: storage.LoadStorageConfigFromEnv() + NewAllStores().
// This removes ~100 lines of duplicated env-parsing and backend-selection logic.
//
// memory mode: fast ephemeral (in-memory keys + RAM cache) — excellent for
// debugging business logic and unit tests.
// sqlite mode: durable embedded single-binary (recommended default).
// No hybrid (filesystem JSON + sqlite) is supported — it would duplicate data.
func initializeMCPComponents() (scmiddleware.Store, auth.APIKeyIssuer, auth.APIKeyValidator, *services.IngestionService, *auth.ChallengeStore) {
	cfg := storage.LoadStorageConfigFromEnv()

	allStores, err := storage.NewAllStores(cfg)
	if err != nil {
		log.Fatalf("failed to initialize unified storage (STARGATE_STORAGE=%s): %v", cfg.Type, err)
	}

	// The MCP/SmartContract Store is the one passed around as scmiddleware.Store
	mcpStore := allStores.SmartContractStore

	if _, ok := mcpStore.(*scstore.MemoryStore); ok {
		log.Printf("Components initialized with memory store")
	}

	// For backward compatibility with the old return signature we still return
	// the pieces that the rest of main expects.
	// DataStorage and ContractCache are also available on allStores if needed.
	return mcpStore,
		allStores.APIKeyIssuer,
		allStores.APIKeyValidator,
		allStores.IngestionService,
		allStores.ChallengeStore
}

// startMCPServices starts background services for sync (works with PostgreSQL or embedded SQLite).
func startMCPServices(escort *smart_contract.EscortService, store scmiddleware.Store) {
	pgDsn := os.Getenv("STARGATE_PG_DSN")

	if store == nil {
		log.Printf("no valid store available, skipping background services")
		return
	}

	// Build ingestion DSN for sync
	var ingestDsn string
	if pgDsn != "" {
		ingestDsn = pgDsn
	} else {
		// Use embedded SQLite for ingestion
		ingestDsn = os.Getenv("STARGATE_INGESTIONS_DB")
		if ingestDsn == "" {
			dataDir := os.Getenv("STARGATE_DATA_DIR")
			if dataDir == "" {
				dataDir = "data"
			}
			ingestDsn = filepath.Join(dataDir, "ingestions.db")
		}
	}

	// Start ingestion -> MCP sync
	if os.Getenv("STARGATE_ENABLE_INGEST_SYNC") != "false" {
		syncInterval := 30 * time.Second
		if raw := os.Getenv("STARGATE_INGEST_SYNC_INTERVAL_SEC"); raw != "" {
			if v, err := strconv.Atoi(raw); err == nil && v > 0 {
				syncInterval = time.Duration(v) * time.Second
			}
		}

		if err := scmiddleware.StartIngestionSync(context.Background(), ingestDsn, store, syncInterval); err != nil {
			log.Printf("ingestion sync disabled (init error): %v", err)
		} else {
			log.Printf("ingestion sync enabled (interval=%s)", syncInterval)
		}
	}

	// Start funding proof refresher (opt-in only; disabled by default since
	// the direct PSBT payment flow + block monitor makes external proof
	// refresh unnecessary for most deployments).
	if os.Getenv("STARGATE_ENABLE_FUNDING_SYNC") == "true" {
		fundingInterval := 60 * time.Second
		if raw := os.Getenv("STARGATE_FUNDING_SYNC_INTERVAL_SEC"); raw != "" {
			if v, err := strconv.Atoi(raw); err == nil && v > 0 {
				fundingInterval = time.Duration(v) * time.Second
			}
		}

		fundingProvider := "mock"
		if env := os.Getenv("STARGATE_FUNDING_PROVIDER"); env != "" {
			fundingProvider = env
		}
		fundingAPIBase := bitcoin.GetNetworkConfig(bitcoin.GetCurrentNetwork()).BaseURL
		if env := os.Getenv("STARGATE_FUNDING_API_BASE"); env != "" {
			fundingAPIBase = env
		}

		provider := scmiddleware.NewFundingProvider(fundingProvider, fundingAPIBase)
		if err := scmiddleware.StartFundingSync(context.Background(), store, provider, escort, fundingInterval); err != nil {
			log.Printf("funding sync disabled (init error): %v", err)
		} else {
			log.Printf("funding sync enabled (interval=%s, provider=%s)", fundingInterval, fundingProvider)
		}
	} else {
		log.Printf("funding sync disabled (default; set STARGATE_ENABLE_FUNDING_SYNC=true to enable)")
	}
}

// consolidateEnvironmentPaths ensures all data-related environment variables are consistent.
func consolidateEnvironmentPaths() {
	// Root data directory
	dataDir := os.Getenv("STARGATE_DATA_DIR")
	if dataDir == "" {
		dataDir = "data"
	}

	// Consolidate standard paths if not explicitly set
	paths := map[string]string{
		"BLOCKS_DIR":             "blocks",
		"UPLOADS_DIR":            "uploads",
		"IPFS_STORAGE_DIR":       "ipfs_objects",
		"IPFS_EMBEDDED_REPO":     "ipfs_repo",
		"SMART_CONTRACTS_FILE":   "smart_contracts.json",
		"STARGATE_MCP_DB":        "sqlite/mcp.db",
		"STARGATE_API_KEYS_DB":   "sqlite/api_keys.db",
		"STARGATE_INGESTIONS_DB": "sqlite/ingestions.db",
	}

	// Ensure sqlite directory exists if we are using it
	sqliteDir := filepath.Join(dataDir, "sqlite")
	if err := os.MkdirAll(sqliteDir, 0755); err != nil {
		log.Printf("Warning: failed to create sqlite directory %s: %v", sqliteDir, err)
	}

	for envVar, defaultSubdir := range paths {
		if os.Getenv(envVar) == "" {
			resolved := filepath.Join(dataDir, defaultSubdir)
			os.Setenv(envVar, resolved)
			log.Printf("Consolidated %s to %s", envVar, resolved)
		}
	}
}

func main() {
	log.Println("=== STARTING STARGATE BACKEND ===")

	// Ensure consistent data paths
	consolidateEnvironmentPaths()

	// Initialize MCP components (needed for both server and background)
	store, apiKeyIssuer, apiKeyValidator, ingestionSvc, challengeStore := initializeMCPComponents()

	// Initialize IPFS client (includes embedded node if enabled)
	ipfsClient := ipfs.NewClientFromEnv()
	defer func() {
		if ipfsClient != nil {
			log.Println("Shutting down IPFS client...")
			_ = ipfsClient.Close()
		}
	}()

	// Start HTTP server (includes MCP endpoints)
	go runHTTPServer(store, apiKeyIssuer, apiKeyValidator, ingestionSvc, challengeStore, ipfsClient)

	// Wait indefinitely
	select {} // Block forever
}

func runHTTPServer(store scmiddleware.Store, apiKeyIssuer auth.APIKeyIssuer, apiKeyValidator auth.APIKeyValidator, ingestionSvc *services.IngestionService, challengeStore *auth.ChallengeStore, ipfsClient *ipfs.Client) {
	log.Println("=== STARTING STARGATE HTTP SERVER ===")

	// Initialize IPFS native storage
	ipfsStorageDir := os.Getenv("IPFS_STORAGE_DIR")
	if ipfsStorageDir == "" {
		ipfsStorageDir = "ipfs_objects"
	}
	if err := os.MkdirAll(ipfsStorageDir, 0755); err != nil {
		log.Printf("Warning: failed to create IPFS storage directory %s: %v", ipfsStorageDir, err)
	} else {
		log.Printf("IPFS native storage initialized at: %s", ipfsStorageDir)
	}

	var mirror mirrorState
	ipfsCfg := ipfs.LoadMirrorConfig()
	// When the mirror downloads a new file, trigger ingestion immediately
	// instead of relying on a separate pubsub re-fetch cycle.
	ipfsCfg.OnFileDownloaded = func(ctx context.Context, ev ipfs.FileDownloadedEvent) {
		scmiddleware.IngestDownloadedFile(ctx, ev.FilePath, ev.CID, ingestionSvc, store)
	}
	if ipfsCfg.Enabled {
		go mirror.startWithRetry(context.Background(), ipfsCfg)
	}

	// Initialize dependency container
	container := container.NewContainer(apiKeyIssuer, apiKeyValidator)

	// Initialize EscortService
	rpcURL := bitcoin.GetNetworkConfig(bitcoin.GetCurrentNetwork()).BaseURL
	if env := os.Getenv("STARGATE_FUNDING_API_BASE"); env != "" {
		rpcURL = env
	}
	verifier := smart_contract.NewMerkleProofVerifier(rpcURL)
	interpreter := smart_contract.NewScriptInterpreter()
	escort := smart_contract.NewEscortService(verifier, interpreter)

	// Initialize HTTP MCP server (always enabled)
	scannerManager := starlight.GetScannerManager()
	httpMCPServer := mcp.NewHTTPMCPServer(store, apiKeyValidator, apiKeyIssuer, ingestionSvc, scannerManager, container.SmartContractService, challengeStore)

	// Set the smart contract handler with the store
	container.SetSmartContractHandler(store)
	// Also allow inscription handler to mirror into MCP store
	container.InscriptionHandler.SetStore(store)

	// Start MCP background services if using PostgreSQL AND MCP server is not running separately
	if os.Getenv("STARGATE_MODE") != "mcp-only" && os.Getenv("STARGATE_MODE") != "both" {
		startMCPServices(escort, store)
	} else {
		log.Println("MCP background services skipped (will be handled by separate MCP process)")
	}

	// Start built-in agent orchestrator (opt-in via STARGATE_AGENT_ENABLED).
	// This brings the former Python starlight.agents orchestration logic into stargate.
	agentCfg := agents.LoadConfig()
	if agentCfg.Enabled {
		// Propagate executor config from LoadConfig into env so NewAutoDetectExecutor picks it up
		if agentCfg.ExecutorTool != "" {
			os.Setenv("STARGATE_AGENT_EXECUTOR", agentCfg.ExecutorTool)
		}
		if agentCfg.ExecutorModel != "" {
			os.Setenv("STARGATE_AGENT_EXECUTOR_MODEL", agentCfg.ExecutorModel)
		}

		agentOrch := agents.NewOrchestrator(agentCfg, store, nil)
		agentOrch.Start(context.Background())
		// Note: orchestrator runs until process exit for now (matches other background services).
		// If a full shutdown path is added later, call agentOrch.Stop() on termination.
		log.Printf("Built-in agents enabled (watcher=%v, worker=%v, tool=%s, model=%s)",
			agentCfg.WatcherEnabled, agentCfg.WorkerEnabled, agentCfg.ExecutorTool, agentCfg.ExecutorModel)
	} else {
		log.Println("Built-in agents disabled (set STARGATE_AGENT_ENABLED=true to enable)")
	}

	// Set up middleware chain
	mux := http.NewServeMux()

	// Register HTTP MCP routes
	httpMCPServer.RegisterRoutes(mux)

	// Apply middleware to all routes
	routes, mcpRestServer := setupRoutes(mux, container, store, apiKeyIssuer, apiKeyValidator, challengeStore, ingestionSvc, &mirror, escort)

	// Set smart_contract server reference on MCP server (must be done after mcpRestServer is created)
	httpMCPServer.SetServer(mcpRestServer)

	handler := middleware.Recovery(
		middleware.Logging(
			middleware.SecurityHeaders(
				middleware.CORS(
					middleware.Timeout(30 * time.Second)(routes),
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
	log.Printf("Smart contract API at: http://localhost:%s/api/smart_contract/", httpPort)
	log.Printf("MCP HTTP tools at: http://localhost:%s/mcp/tools", httpPort)
	log.Printf("MCP HTTP calls at: http://localhost:%s/mcp/call", httpPort)
	log.Printf("Proxy to steganography API (port 8080) at: http://localhost:%s/stego/", httpPort)

	log.Fatal(http.ListenAndServe(":"+httpPort, handler))
}

func setupRoutes(mux *http.ServeMux, container *container.Container, store scmiddleware.Store, apiKeyIssuer auth.APIKeyIssuer, apiKeyValidator auth.APIKeyValidator, challengeStore *auth.ChallengeStore, ingestionSvc *services.IngestionService, mirror *mirrorState, escort *smart_contract.EscortService) (http.Handler, *scmiddleware.Server) {
	// Initialize MCP REST server for HTTP routes
	mcpRestServer := scmiddleware.NewServer(store, apiKeyValidator, ingestionSvc)
	if escort != nil {
		mcpRestServer.SetEscortService(escort)
	}
	mcpRestServer.RegisterRoutes(mux)
	if err := scmiddleware.StartStegoPubsubSync(context.Background(), mcpRestServer); err != nil {
		log.Printf("stego pubsub sync disabled: %v", err)
	}
	if err := scmiddleware.StartSyncPubsubSync(context.Background(), mcpRestServer); err != nil {
		log.Printf("mcp event sync disabled: %v", err)
	}
	// Health endpoints
	mux.HandleFunc("/api/health", container.HealthHandler.HandleHealth)

	// Peer Discovery endpoints
	mux.HandleFunc("/api/peers/register", container.DiscoveryHandler.HandleRegisterPeer)
	mux.HandleFunc("/api/peers/unregister", container.DiscoveryHandler.HandleUnregisterPeer)
	mux.HandleFunc("/api/peers", container.DiscoveryHandler.HandleListPeers)

	mux.HandleFunc("/api/ipfs-mirror/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		status := mirror.Status()
		if err := json.NewEncoder(w).Encode(status); err != nil {
			http.Error(w, "failed to encode status", http.StatusInternalServerError)
			return
		}
	})

	// Auth endpoints
	keyHandler := handlers.NewAPIKeyHandler(apiKeyIssuer, apiKeyValidator, challengeStore)
	// mux.HandleFunc("/api/auth/register", keyHandler.HandleRegister) // DISABLED for security
	mux.HandleFunc("/api/auth/login", keyHandler.HandleLogin)
	mux.HandleFunc("/api/auth/logout", keyHandler.HandleLogout)
	mux.HandleFunc("/api/auth/challenge", keyHandler.HandleChallenge)
	mux.HandleFunc("/api/auth/verify", keyHandler.HandleVerify)

	// Helper function to wrap handlers with auth
	wrapWithAuth := func(h http.HandlerFunc) http.Handler {
		return middleware.APIAuth(apiKeyValidator)(h)
	}

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
	mux.Handle("/api/inscriptions/", wrapWithAuth(container.InscriptionHandler.HandleDeleteInscription))
	mux.Handle("/api/inscribe", wrapWithAuth(container.InscriptionHandler.HandleCreateInscription))

	// Surface ownership catalog (primary vs legacy aliases) — see api/surfaces.go and docs/arch/MCP_UNIFIED_PLAN.md
	mux.HandleFunc("/api/surfaces", api.HandleSurfaces)

	// Block endpoints — primary browse APIs live under /api/data/*; /api/blocks is a legacy alias.
	// /api/blocks retired (3bk.8); use /api/data/blocks
	_ = container.BlockHandler // keep handler in container for data API parity if needed

	// UI contract list (inscription-shaped for the frontend). Prefer /api/open-contracts.
	// Lifecycle CRUD for agents stays on /api/smart_contract/*; MCP tools shim the same store.
	// Primary UI wish/contract browse (inscription-shaped).
	// Retired (3bk.8): /api/smart-contracts, /api/contracts-confirmed,
	// /api/data/contracts-with-pagination, /api/contract-stego(+ /create).
	mux.HandleFunc("/api/open-contracts", container.SmartContractHandler.HandleGetContracts)

	// Ingestion endpoints
	mux.Handle("/api/ingest-inscription", wrapWithAuth(container.IngestionHandler.HandleIngest))
	mux.HandleFunc("/api/ingest-inscription/", container.IngestionHandler.HandleGetIngestion)
	mux.HandleFunc("/api/ingest-hash", container.IngestionHandler.HandleHashImage)

	// Search endpoints
	mux.HandleFunc("/api/search", container.SearchHandler.HandleSearch)

	// QR code endpoints
	mux.HandleFunc("/api/qrcode", container.QRCodeHandler.HandleGenerateQRCode)

	// Proxy endpoints
	mux.Handle("/stego/", wrapWithAuth(container.ProxyHandler.HandleProxy))
	mux.Handle("/analyze/", wrapWithAuth(container.ProxyHandler.HandleProxy))
	mux.Handle("/generate/", wrapWithAuth(container.ProxyHandler.HandleProxy))

	// Serve uploaded files with proper MIME type detection
	uploadsDir := os.Getenv("UPLOADS_DIR")
	_ = os.MkdirAll(uploadsDir, 0755)
	mux.HandleFunc("/uploads/", customUploadsHandler(uploadsDir))

	// Serve sandbox files (alias for /uploads/results)
	resultsDir := filepath.Join(uploadsDir, "results")
	_ = os.MkdirAll(resultsDir, 0755)
	mux.Handle("/sandbox/", http.StripPrefix("/sandbox/", http.FileServer(http.Dir(resultsDir))))

	// Serve frontend files from embedded FS
	frontendFS, _ := fs.Sub(frontendAssets, "assets/frontend")
	fileServer := http.FileServer(http.FS(frontendFS))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Clean the path to prevent directory traversal
		path := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"))
		if path == "." || path == "" {
			path = "index.html"
		}

		// Check if the file exists in the embedded FS
		_, err := frontendFS.Open(path)
		if err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}

		// Fallback to index.html for SPA routing
		// Only fallback if it doesn't look like an API or content request
		if !strings.HasPrefix(r.URL.Path, "/api/") &&
			!strings.HasPrefix(r.URL.Path, "/bitcoin/") &&
			!strings.HasPrefix(r.URL.Path, "/mcp/") &&
			!strings.HasPrefix(r.URL.Path, "/content/") &&
			!strings.HasPrefix(r.URL.Path, "/uploads/") &&
			!strings.HasPrefix(r.URL.Path, "/sandbox/") &&
			!strings.HasPrefix(r.URL.Path, "/stego/") &&
			!strings.HasPrefix(r.URL.Path, "/analyze/") &&
			!strings.HasPrefix(r.URL.Path, "/generate/") &&
			!strings.HasPrefix(r.URL.Path, "/metrics") &&
			!strings.HasPrefix(r.URL.Path, "/swagger") {

			// If the request has an extension and we reached here, the file was not found.
			// Return 404 instead of index.html to avoid "raw HTML" in SPA fetches.
			ext := filepath.Ext(path)
			if ext != "" && ext != ".html" {
				http.NotFound(w, r)
				return
			}

			indexContent, err := fs.ReadFile(frontendFS, "index.html")
			if err == nil {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.Write(indexContent)
				return
			}
		}

		http.NotFound(w, r)
	})

	// Bitcoin API for scanning
	bitcoinAPI := bitcoin.NewBitcoinAPI()

	// Enhanced data API endpoints (keep existing functionality)
	dataStorage := container.DataStorage
	bitcoinNetwork := bitcoin.GetCurrentNetwork()
	bitcoinClient := bitcoin.NewBitcoinNodeClientForNetwork(bitcoinNetwork)
	blockMonitor := bitcoin.NewBlockMonitorWithStorageAndAPI(
		bitcoinClient,
		dataStorage,
		bitcoinAPI,
	)
	blockMonitor.SetIngestionService(container.IngestionService)
	blockMonitor.SetStegoReconciler(bitcoin.StegoReconcilerFunc(func(ctx context.Context, stegoCID, expectedHash string) error {
		return mcpRestServer.ReconcileStego(ctx, stegoCID, expectedHash)
	}))
	blockMonitor.SetIPFSUnpin(func(ctx context.Context, path string) error {
		return mirror.UnpinPath(ctx, path)
	})
	// All Store implementations (Memory, SQLite, PG) now satisfy bitcoin.SweepTaskStore
	// because the required methods are part of the core Store interface (Phase 5).
	blockMonitor.SetSweepDependencies(store, bitcoin.NewMempoolClient())
	// OP_RETURN-based matching: block monitor discovers contracts during normal
	// block processing — no event-driven reconciliation needed.
	if err := scmiddleware.StartIPFSIngestionSync(context.Background(), ingestionSvc, store, func(ctx context.Context, recent int) error {
		return blockMonitor.ReconcileRecentBlocks(ctx, recent)
	}); err != nil {
		log.Printf("ipfs ingestion sync disabled: %v", err)
	}

	// Pre-cache historical blocks (with rate limiting)
	go func() {
		if bitcoinNetwork != "mainnet" {
			log.Printf("Skipping historical block pre-cache on %s", bitcoinNetwork)
			return
		}
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

	// Keep the content tx index in sync as new blocks arrive.
	if blockMonitor != nil {
		blockMonitor.OnBlockProcessed(dataAPI.IndexBlock)
	}

	mux.HandleFunc("/api/data/block/", dataAPI.HandleGetBlockData)
	mux.HandleFunc("/api/data/blocks", dataAPI.HandleGetRecentBlocks)
	mux.HandleFunc("/api/data/block-summaries", dataAPI.HandleGetBlockSummaries)
	mux.HandleFunc("/api/data/block-inscriptions/", dataAPI.HandleGetBlockInscriptionsPaginated)
	mux.HandleFunc("/api/data/stats", dataAPI.HandleGetSteganographyStats)
	mux.HandleFunc("/api/data/updates", dataAPI.HandleRealtimeUpdates)
	mux.HandleFunc("/api/data/scan", dataAPI.HandleScanBlockOnDemand)
	mux.HandleFunc("/api/data/block-images", dataAPI.HandleGetBlockImages)
	// /api/block-images retired (3bk.8); use /api/data/block-images
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
			if filepath.Ext(fsPath) == "" {
				if file, err := os.Open(fsPath); err == nil {
					defer file.Close()
					buf := make([]byte, 512)
					n, _ := file.Read(buf)
					if n > 0 {
						w.Header().Set("Content-Type", http.DetectContentType(buf[:n]))
					}
				}
			}
			http.ServeFile(w, r, fsPath)
			return
		}

		// Fallback: check UPLOADS_DIR (images received via IPFS or local creation)
		if uDir := os.Getenv("UPLOADS_DIR"); uDir != "" {
			uploadPath := filepath.Join(uDir, filename)
			if !strings.HasPrefix(filepath.Clean(uploadPath), filepath.Clean(uDir)) {
				http.Error(w, "Invalid filename", http.StatusBadRequest)
				return
			}
			if info, err := os.Stat(uploadPath); err == nil && !info.IsDir() {
				log.Printf("Serving image from uploads fallback: %s", uploadPath)
				http.ServeFile(w, r, uploadPath)
				return
			}
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
						w.Header().Set("Content-Length", fmt.Sprintf("%d", len(img.Data)))
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

	// MCP tools are available via HTTP endpoints at /mcp/

	log.Printf("All routes registered, returning handler")
	return mux, mcpRestServer
}

type mirrorState struct {
	mu     sync.RWMutex
	mirror *ipfs.Mirror
}

func (m *mirrorState) Status() ipfs.MirrorStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m == nil || m.mirror == nil {
		return ipfs.MirrorStatus{Enabled: false}
	}
	return m.mirror.Status()
}

func (m *mirrorState) UnpinPath(ctx context.Context, path string) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m == nil || m.mirror == nil {
		return nil
	}
	return m.mirror.UnpinPath(ctx, path)
}

func (m *mirrorState) set(mirror *ipfs.Mirror) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mirror = mirror
}

func (m *mirrorState) startWithRetry(ctx context.Context, cfg ipfs.MirrorConfig) {
	backoff := 5 * time.Second
	maxBackoff := 1 * time.Minute

	for {
		started, err := ipfs.StartMirror(ctx, cfg)
		if err == nil && started != nil {
			m.set(started)
			log.Printf("IPFS mirror started after retry")
			return
		}
		if err != nil {
			log.Printf("IPFS mirror startup failed (retrying in %s): %v", backoff, err)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}

		if backoff < maxBackoff {
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}
