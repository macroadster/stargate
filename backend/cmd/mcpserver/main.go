package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"stargate-backend/mcp"
	"stargate-backend/services"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type config struct {
	Port            string
	ClaimTTL        time.Duration
	StoreDriver     string
	PGDSN           string
	Seed            bool
	APIKey          string
	IngestSync      bool
	SyncInterval    time.Duration
	FundingSync     bool
	FundingInterval time.Duration
	FundingProvider string
	FundingAPIBase  string
}

func loadConfig() config {
	port := os.Getenv("MCP_PORT")
	if port == "" {
		port = "3002"
	}

	ttlHours := 72
	if raw := os.Getenv("MCP_DEFAULT_CLAIM_TTL_HOURS"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			ttlHours = v
		}
	}

	syncInterval := 30 * time.Second
	if raw := os.Getenv("MCP_INGEST_SYNC_INTERVAL_SEC"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			syncInterval = time.Duration(v) * time.Second
		}
	}

	fundingInterval := 60 * time.Second
	if raw := os.Getenv("MCP_FUNDING_SYNC_INTERVAL_SEC"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			fundingInterval = time.Duration(v) * time.Second
		}
	}

	storeDriver := os.Getenv("MCP_STORE_DRIVER")
	if storeDriver == "" {
		storeDriver = "memory"
	}

	pgDsn := os.Getenv("MCP_PG_DSN")
	seed := true
	if raw := os.Getenv("MCP_SEED_FIXTURES"); raw != "" {
		if v, err := strconv.ParseBool(raw); err == nil {
			seed = v
		}
	}

	return config{
		Port:            port,
		ClaimTTL:        time.Duration(ttlHours) * time.Hour,
		StoreDriver:     storeDriver,
		PGDSN:           pgDsn,
		Seed:            seed,
		APIKey:          os.Getenv("MCP_API_KEY"),
		IngestSync:      os.Getenv("MCP_ENABLE_INGEST_SYNC") != "false",
		SyncInterval:    syncInterval,
		FundingSync:     os.Getenv("MCP_ENABLE_FUNDING_SYNC") != "false",
		FundingInterval: fundingInterval,
		FundingProvider: envDefault("MCP_FUNDING_PROVIDER", "mock"), // mock | blockstream
		FundingAPIBase:  envDefault("MCP_FUNDING_API_BASE", "https://blockstream.info/api"),
	}
}

func envDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	cfg := loadConfig()

	ctx := context.Background()
	var store mcp.Store
	var err error
	var ingestSvc *services.IngestionService
	switch cfg.StoreDriver {
	case "postgres":
		if cfg.PGDSN == "" {
			log.Fatal("MCP_PG_DSN required when MCP_STORE_DRIVER=postgres")
		}
		store, err = mcp.NewPGStore(ctx, cfg.PGDSN, cfg.ClaimTTL, cfg.Seed)
		if err == nil {
			if svc, serr := services.NewIngestionService(cfg.PGDSN); serr != nil {
				log.Printf("ingestion service unavailable for proposal creation: %v", serr)
			} else {
				ingestSvc = svc
			}
		}
	default:
		store = mcp.NewMemoryStore(cfg.ClaimTTL)
	}
	if err != nil {
		log.Fatalf("failed to init store: %v", err)
	}
	defer store.Close()

	// Start ingestion -> MCP sync when using Postgres.
	if cfg.StoreDriver == "postgres" && cfg.IngestSync {
		if err := mcp.StartIngestionSync(context.Background(), cfg.PGDSN, store, cfg.SyncInterval); err != nil {
			log.Printf("ingestion sync disabled (init error): %v", err)
		} else {
			log.Printf("ingestion sync enabled (interval=%s)", cfg.SyncInterval)
		}
	}

	// Start funding proof refresher (mock provider by default).
	if cfg.StoreDriver == "postgres" && cfg.FundingSync {
		provider := mcp.NewFundingProvider(cfg.FundingProvider, cfg.FundingAPIBase)
		if err := mcp.StartFundingSync(context.Background(), store, provider, cfg.FundingInterval); err != nil {
			log.Printf("funding sync disabled (init error): %v", err)
		} else {
			log.Printf("funding sync enabled (interval=%s)", cfg.FundingInterval)
		}
	}

	server := mcp.NewServer(store, cfg.APIKey, ingestSvc)

	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	mux.Handle("/metrics", promhttp.Handler())

	addr := ":" + cfg.Port
	log.Printf("Stargate MCP server starting on %s (driver=%s)", addr, cfg.StoreDriver)
	log.Printf("Health: http://localhost%s/healthz", addr)
	log.Printf("API:    http://localhost%s/mcp/v1", addr)

	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}
