package container

import (
	"log"
	"os"
	"path/filepath"
	"stargate-backend/handlers"
	scmiddleware "stargate-backend/middleware/smart_contract"
	"stargate-backend/services"
	"stargate-backend/storage"
	"stargate-backend/storage/auth"
	"stargate-backend/storage/smart_contract"
	"strconv"
	"time"
)

// Container holds all application dependencies
type Container struct {
	// Services
	InscriptionService   *services.InscriptionService
	BlockService         *services.BlockService
	SmartContractService *services.SmartContractService
	QRCodeService        *services.QRCodeService
	HealthService        *services.HealthService
	PeerService          *services.PeerService
	DataStorage          storage.ExtendedDataStorage
	IngestionService     *services.IngestionService

	// Caches
	ContractCache *smart_contract.ContractCache

	// Handlers
	HealthHandler        *handlers.HealthHandler
	DiscoveryHandler     *handlers.DiscoveryHandler
	InscriptionHandler   *handlers.InscriptionHandler
	BlockHandler         *handlers.BlockHandler
	SmartContractHandler *handlers.SmartContractHandler
	SearchHandler        *handlers.SearchHandler
	QRCodeHandler        *handlers.QRCodeHandler
	ProxyHandler         *handlers.ProxyHandler
	IngestionHandler     *handlers.IngestionHandler
}

// NewContainer creates a new dependency container
func NewContainer(apiKeyIssuer auth.APIKeyIssuer, apiKeyValidator auth.APIKeyValidator) *Container {
	storageType := os.Getenv("STARGATE_STORAGE")
	pgDSN := os.Getenv("STARGATE_PG_DSN")
	if pgDSN == "" {
		pgDSN = os.Getenv("DATABASE_URL") // fallback env name
	}

	// Initialize cache
	contractCacheTTL := 2 * time.Minute
	contractCacheSize := 1000
	if env := os.Getenv("CONTRACT_CACHE_TTL"); env != "" {
		if duration, err := time.ParseDuration(env); err == nil {
			contractCacheTTL = duration
		}
	}
	if env := os.Getenv("CONTRACT_CACHE_SIZE"); env != "" {
		if size, err := strconv.Atoi(env); err == nil && size > 0 {
			contractCacheSize = size
		}
	}
	contractCache := smart_contract.NewContractCache(contractCacheTTL, contractCacheSize)

	// Initialize services
	dataDir := os.Getenv("BLOCKS_DIR")
	if dataDir == "" {
		dataDir = storage.DefaultPath("blocks")
	}
	inscriptionsFile := os.Getenv("INSCRIPTIONS_FILE")
	if inscriptionsFile == "" {
		inscriptionsFile = filepath.Join(dataDir, "inscriptions.json")
	}
	if err := os.MkdirAll(filepath.Dir(inscriptionsFile), 0755); err != nil {
		log.Printf("failed to ensure data dir: %v", err)
	}
	inscriptionService := services.NewInscriptionService(inscriptionsFile)
	blockService := services.NewBlockService()
	contractsFile := os.Getenv("SMART_CONTRACTS_FILE")
	if contractsFile == "" {
		contractsFile = storage.DefaultPath("smart_contracts.json")
	}
	contractService := services.NewSmartContractService(contractsFile)
	qrService := services.NewQRCodeService()
	healthService := services.NewHealthService()
	peerService := services.NewPeerService()

	var ingestionService *services.IngestionService
	ingestionDSN := pgDSN
	if ingestionDSN == "" {
		// Fall back to SQLite ingestion database
		ingestionDSN = os.Getenv("STARGATE_INGESTIONS_DB")
		if ingestionDSN == "" {
			dataDir := os.Getenv("STARGATE_DATA_DIR")
			if dataDir == "" {
				dataDir = "data"
			}
			ingestionDSN = filepath.Join(dataDir, "sqlite", "ingestions.db")
		}
	}
	ingestionService = initIngestionService(ingestionDSN)

	// Data storage selection
	var dataStorage storage.ExtendedDataStorage
	dataStorage = storage.NewDataStorage(dataDir)
	if storageType == "postgres" && pgDSN != "" {
		if pgStore, err := storage.NewPostgresStorage(pgDSN); err != nil {
			log.Printf("Failed to init Postgres storage, falling back to filesystem: %v", err)
		} else {
			log.Printf("Using Postgres storage backend")
			dataStorage = pgStore
		}
	}

	// Initialize handlers
	healthHandler := handlers.NewHealthHandler(healthService)
	discoveryHandler := handlers.NewDiscoveryHandler(peerService)
	inscriptionHandler := handlers.NewInscriptionHandler(inscriptionService, ingestionService, apiKeyIssuer, apiKeyValidator)
	blockHandler := handlers.NewBlockHandler(blockService)
	// contractHandler will be set later with store
	searchHandler := handlers.NewSearchHandler(inscriptionService, blockService, dataStorage, nil)
	qrHandler := handlers.NewQRCodeHandler(qrService)
	proxyBase := os.Getenv("STARGATE_PROXY_BASE")
	if proxyBase == "" {
		proxyBase = "http://localhost:8080"
	}
	proxyHandler := handlers.NewProxyHandler(proxyBase)
	ingestionHandler := handlers.NewIngestionHandler(ingestionService)

	return &Container{
		// Services
		InscriptionService:   inscriptionService,
		BlockService:         blockService,
		SmartContractService: contractService,
		QRCodeService:        qrService,
		HealthService:        healthService,
		PeerService:          peerService,
		DataStorage:          dataStorage,
		IngestionService:     ingestionService,

		// Caches
		ContractCache: contractCache,

		// Handlers
		HealthHandler:      healthHandler,
		DiscoveryHandler:   discoveryHandler,
		InscriptionHandler: inscriptionHandler,
		BlockHandler:       blockHandler,
		// SmartContractHandler will be set later
		SearchHandler:    searchHandler,
		QRCodeHandler:    qrHandler,
		ProxyHandler:     proxyHandler,
		IngestionHandler: ingestionHandler,
	}
}

// SetSmartContractHandler sets the smart contract handler with the MCP store
func (c *Container) SetSmartContractHandler(store scmiddleware.Store) {
	c.SmartContractHandler = handlers.NewSmartContractHandler(store, c.IngestionService, c.ContractCache)
	// Also set the store on SearchHandler for proposals/contracts search
	if c.SearchHandler != nil {
		c.SearchHandler.SetStore(store)
	}
}

// initIngestionService retries connecting to Postgres a few times to avoid startup races.
func initIngestionService(pgDSN string) *services.IngestionService {
	const maxAttempts = 5
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if svc, err := services.NewIngestionService(pgDSN); err == nil {
			return svc
		} else {
			log.Printf("failed to init ingestion service (attempt %d/%d): %v", attempt, maxAttempts, err)
		}
		time.Sleep(time.Duration(attempt) * time.Second)
	}
	log.Printf("ingestion service disabled after retries")
	return nil
}
