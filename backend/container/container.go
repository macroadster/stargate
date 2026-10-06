package container

import (
	"log"
	"os"
	"path/filepath"
	scmiddleware "stargate-backend/app/smart_contract"
	"stargate-backend/handlers"
	"stargate-backend/services"
	"stargate-backend/storage"
	"stargate-backend/storage/smart_contract"
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

// NewContainer wires handlers onto storage that has already been built.
//
// It used to re-read STARGATE_STORAGE, the Postgres DSN, STARGATE_DATA_DIR and
// the cache settings and construct its own ingestion service, data storage and
// contract cache, giving the process two independent data layers over the same
// data (stargate-a49). Taking AllStores means the layer is built once, by
// NewAllStores, and the two can no longer disagree.
func NewContainer(stores *storage.AllStores) *Container {
	// The key stores come from AllStores too, rather than being passed
	// separately, so the handlers cannot be handed a different pair than the one
	// the rest of the process authenticates against.
	apiKeyIssuer := stores.APIKeyIssuer
	apiKeyValidator := stores.APIKeyValidator
	// The cache the rest of the process reads. A second one was allocated here
	// while NewAllStores' went unused.
	contractCache := stores.ContractCache

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

	// The one ingestion service and the one data layer, both from NewAllStores.
	//
	// This built its own of each. The ingestion DSN it derived was not even the
	// same one: it preferred the Postgres DSN whenever the environment carried
	// it, while NewAllStores only uses that DSN when the configured type is
	// postgres. With STARGATE_STORAGE=sqlite and a DATABASE_URL present the two
	// therefore addressed different databases, and initIngestionService returned
	// nil after ~15s of retries if that Postgres was unreachable. The block
	// monitor nil-guards its ingestion handle, so the effect was ingestion
	// reconciliation silently never running rather than a crash.
	ingestionService := stores.IngestionService
	dataStorage := stores.DataStorage

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
		proxyBase = "http://localhost:3001" // default to self in single-binary mode
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

// Close stops background goroutines owned by services in the container
// (e.g. peer cleanup, contract cache TTL cleaner). Safe to call multiple times.
//
// This comment sat here for a while with no method under it, and nothing called
// the Stop methods it describes (stargate-ard). Because it read as though the
// work were done, stargate-gkh gave BlockMonitor and Orchestrator shutdown paths
// and left these two alone.
//
// Each construction starts one cleanup goroutine, so what this buys in a process
// that builds one container and then exits is small — exit reclaims them either
// way. It matters for anything that builds containers repeatedly, tests
// included, and it makes the two Stop methods reachable rather than dead.
func (c *Container) Close() {
	// The cache is shared: it comes from AllStores, and stopping it stops the
	// cleaner for every holder. That is correct at process shutdown, which is
	// this method's only caller, but it is the reason Close is not something to
	// call on one container while another is still serving.
	if c.ContractCache != nil {
		c.ContractCache.Stop()
	}
	if c.PeerService != nil {
		c.PeerService.Stop()
	}
}
