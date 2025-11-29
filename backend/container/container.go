package container

import (
	"stargate-backend/handlers"
	"stargate-backend/services"
)

// Container holds all application dependencies
type Container struct {
	// Services
	InscriptionService   *services.InscriptionService
	BlockService         *services.BlockService
	SmartContractService *services.SmartContractService
	QRCodeService        *services.QRCodeService
	HealthService        *services.HealthService

	// Handlers
	HealthHandler        *handlers.HealthHandler
	InscriptionHandler   *handlers.InscriptionHandler
	BlockHandler         *handlers.BlockHandler
	SmartContractHandler *handlers.SmartContractHandler
	SearchHandler        *handlers.SearchHandler
	QRCodeHandler        *handlers.QRCodeHandler
	ProxyHandler         *handlers.ProxyHandler
}

// NewContainer creates a new dependency container
func NewContainer() *Container {
	// Initialize services
	inscriptionService := services.NewInscriptionService("inscriptions.json")
	blockService := services.NewBlockService()
	contractService := services.NewSmartContractService("smart_contracts.json")
	qrService := services.NewQRCodeService()
	healthService := services.NewHealthService()

	// Initialize handlers
	healthHandler := handlers.NewHealthHandler(healthService)
	inscriptionHandler := handlers.NewInscriptionHandler(inscriptionService)
	blockHandler := handlers.NewBlockHandler(blockService)
	contractHandler := handlers.NewSmartContractHandler(contractService)
	searchHandler := handlers.NewSearchHandler(inscriptionService, blockService)
	qrHandler := handlers.NewQRCodeHandler(qrService)
	proxyHandler := handlers.NewProxyHandler("http://localhost:8080")

	return &Container{
		// Services
		InscriptionService:   inscriptionService,
		BlockService:         blockService,
		SmartContractService: contractService,
		QRCodeService:        qrService,
		HealthService:        healthService,

		// Handlers
		HealthHandler:        healthHandler,
		InscriptionHandler:   inscriptionHandler,
		BlockHandler:         blockHandler,
		SmartContractHandler: contractHandler,
		SearchHandler:        searchHandler,
		QRCodeHandler:        qrHandler,
		ProxyHandler:         proxyHandler,
	}
}
