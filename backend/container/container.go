package container

import (
	"log"
	"os"
	"path/filepath"
	"stargate-backend/handlers"
	scmiddleware "stargate-backend/middleware/smart_contract"
	"stargate-backend/services"
	"stargate-backend/storage"
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
	DataStorage          storage.ExtendedDataStorage
	IngestionService     *services.IngestionService

	// Handlers
	HealthHandler        *handlers.HealthHandler
	InscriptionHandler   *handlers.InscriptionHandler
	BlockHandler         *handlers.BlockHandler
	SmartContractHandler *handlers.SmartContractHandler
	SearchHandler        *handlers.SearchHandler
	QRCodeHandler        *handlers.QRCodeHandler
	ProxyHandler         *handlers.ProxyHandler
	IngestionHandler     *handlers.IngestionHandler
}

// NewContainer creates a new dependency container
func NewContainer() *Container {
	storageType := os.Getenv("STARGATE_STORAGE")
	pgDSN := os.Getenv("STARGATE_PG_DSN")
	if pgDSN == "" {
		pgDSN = os.Getenv("DATABASE_URL") // fallback env name
	}

	// Initialize services
	dataDir := "blocks"
	if env := os.Getenv("BLOCKS_DIR"); env != "" {
		dataDir = env
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
	contractService := services.NewSmartContractService("smart_contracts.json")
	qrService := services.NewQRCodeService()
	healthService := services.NewHealthService()
	var ingestionService *services.IngestionService
	if pgDSN != "" {
		ingestionService = initIngestionService(pgDSN)
	} else {
		log.Printf("ingestion service disabled: STARGATE_PG_DSN not set")
	}

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
	inscriptionHandler := handlers.NewInscriptionHandler(inscriptionService, ingestionService)
	blockHandler := handlers.NewBlockHandler(blockService)
	// contractHandler will be set later with store
	searchHandler := handlers.NewSearchHandler(inscriptionService, blockService, dataStorage)
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
		DataStorage:          dataStorage,
		IngestionService:     ingestionService,

		// Handlers
		HealthHandler:      healthHandler,
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
	c.SmartContractHandler = handlers.NewSmartContractHandler(c.SmartContractService, store)
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
