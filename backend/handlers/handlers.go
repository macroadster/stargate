package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"stargate-backend/models"
	"stargate-backend/services"
)

// BaseHandler provides common functionality for all handlers
type BaseHandler struct{}

// NewBaseHandler creates a new base handler
func NewBaseHandler() *BaseHandler {
	return &BaseHandler{}
}

// sendJSON sends a JSON response
func (h *BaseHandler) sendJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	if data != nil {
		json.NewEncoder(w).Encode(data)
	}
}

// sendError sends an error response
func (h *BaseHandler) sendError(w http.ResponseWriter, statusCode int, message string) {
	errorResp := models.NewErrorResponse(message, statusCode)
	h.sendJSON(w, statusCode, errorResp)
}

// sendSuccess sends a success response
func (h *BaseHandler) sendSuccess(w http.ResponseWriter, data interface{}) {
	successResp := models.NewSuccessResponse(data)
	h.sendJSON(w, http.StatusOK, successResp)
}

// parseJSON parses JSON from request
func (h *BaseHandler) parseJSON(r *http.Request, v interface{}) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

// HealthHandler handles health check requests
type HealthHandler struct {
	*BaseHandler
	healthService *services.HealthService
}

// NewHealthHandler creates a new health handler
func NewHealthHandler(healthService *services.HealthService) *HealthHandler {
	return &HealthHandler{
		BaseHandler:   NewBaseHandler(),
		healthService: healthService,
	}
}

// HandleHealth handles health check requests
func (h *HealthHandler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	health := h.healthService.GetHealthStatus()
	h.sendSuccess(w, health)
}

// InscriptionHandler handles inscription-related requests
type InscriptionHandler struct {
	*BaseHandler
	inscriptionService *services.InscriptionService
}

// NewInscriptionHandler creates a new inscription handler
func NewInscriptionHandler(inscriptionService *services.InscriptionService) *InscriptionHandler {
	return &InscriptionHandler{
		BaseHandler:        NewBaseHandler(),
		inscriptionService: inscriptionService,
	}
}

// HandleGetInscriptions handles getting all inscriptions
func (h *InscriptionHandler) HandleGetInscriptions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	inscriptions, err := h.inscriptionService.GetAllInscriptions()
	if err != nil {
		h.sendError(w, http.StatusInternalServerError, "Failed to get inscriptions")
		return
	}

	response := models.PendingTransactionsResponse{
		Transactions: inscriptions,
		Total:        len(inscriptions),
	}
	h.sendSuccess(w, response)
}

// HandleCreateInscription handles creating a new inscription
func (h *InscriptionHandler) HandleCreateInscription(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Parse multipart form
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		h.sendError(w, http.StatusBadRequest, "Failed to parse form")
		return
	}

	// Get form values
	text := r.FormValue("text")
	price := r.FormValue("price")

	// Get file
	file, header, err := r.FormFile("image")
	if err != nil {
		h.sendError(w, http.StatusBadRequest, "Image file required")
		return
	}
	defer file.Close()

	// Create inscription request
	req := models.InscribeRequest{
		Text:  text,
		Price: price,
	}

	// Create inscription
	inscription, err := h.inscriptionService.CreateInscription(req, file, header.Filename)
	if err != nil {
		h.sendError(w, http.StatusInternalServerError, "Failed to create inscription")
		return
	}

	h.sendSuccess(w, map[string]string{
		"status": "success",
		"id":     inscription.ID,
	})
}

// BlockHandler handles block-related requests
type BlockHandler struct {
	*BaseHandler
	blockService *services.BlockService
}

// NewBlockHandler creates a new block handler
func NewBlockHandler(blockService *services.BlockService) *BlockHandler {
	return &BlockHandler{
		BaseHandler:  NewBaseHandler(),
		blockService: blockService,
	}
}

// HandleGetBlocks handles getting blocks
func (h *BlockHandler) HandleGetBlocks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	blocks, err := h.blockService.GetBlocks()
	if err != nil {
		h.sendError(w, http.StatusInternalServerError, "Failed to fetch blocks")
		return
	}

	h.sendSuccess(w, blocks)
}

// SmartContractHandler handles smart contract requests
type SmartContractHandler struct {
	*BaseHandler
	contractService *services.SmartContractService
}

// NewSmartContractHandler creates a new smart contract handler
func NewSmartContractHandler(contractService *services.SmartContractService) *SmartContractHandler {
	return &SmartContractHandler{
		BaseHandler:     NewBaseHandler(),
		contractService: contractService,
	}
}

// HandleGetContracts handles getting all smart contracts
func (h *SmartContractHandler) HandleGetContracts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	contracts, err := h.contractService.GetAllContracts()
	if err != nil {
		h.sendError(w, http.StatusInternalServerError, "Failed to get contracts")
		return
	}

	response := models.SmartContractsResponse{
		Results: contracts,
		Total:   len(contracts),
	}
	h.sendSuccess(w, response)
}

// HandleCreateContract handles creating a new smart contract
func (h *SmartContractHandler) HandleCreateContract(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req models.CreateContractRequest
	if err := h.parseJSON(r, &req); err != nil {
		h.sendError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}

	contract, err := h.contractService.CreateContract(req)
	if err != nil {
		h.sendError(w, http.StatusInternalServerError, "Failed to create contract")
		return
	}

	h.sendSuccess(w, contract)
}

// HandleGetContract handles getting a contract by ID
func (h *SmartContractHandler) HandleGetContract(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Extract contract ID from URL path
	path := strings.TrimPrefix(r.URL.Path, "/api/contract-stego/")
	parts := strings.Split(path, "/")
	if len(parts) < 1 {
		h.sendError(w, http.StatusBadRequest, "Invalid contract ID")
		return
	}

	contractID := parts[0]
	contract, err := h.contractService.GetContractByID(contractID)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "Contract not found")
		return
	}

	h.sendSuccess(w, contract)
}

// SearchHandler handles search requests
type SearchHandler struct {
	*BaseHandler
	inscriptionService *services.InscriptionService
	blockService       *services.BlockService
}

// NewSearchHandler creates a new search handler
func NewSearchHandler(inscriptionService *services.InscriptionService, blockService *services.BlockService) *SearchHandler {
	return &SearchHandler{
		BaseHandler:        NewBaseHandler(),
		inscriptionService: inscriptionService,
		blockService:       blockService,
	}
}

// HandleSearch handles search requests
func (h *SearchHandler) HandleSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	query := r.URL.Query().Get("q")
	if query == "" || strings.ToLower(query) == "block" || strings.ToLower(query) == "blocks" {
		// Return recent blocks
		blocks, err := h.blockService.GetBlocks()
		if err != nil {
			h.sendError(w, http.StatusInternalServerError, "Failed to fetch blocks")
			return
		}

		// Limit to 5 blocks
		if len(blocks) > 5 {
			blocks = blocks[:5]
		}

		inscriptions, _ := h.inscriptionService.GetAllInscriptions()
		response := models.SearchResult{
			Inscriptions: inscriptions,
			Blocks:       blocks,
		}
		h.sendSuccess(w, response)
		return
	}

	// Search inscriptions and blocks
	inscriptionResults, err := h.inscriptionService.SearchInscriptions(query)
	if err != nil {
		h.sendError(w, http.StatusInternalServerError, "Failed to search inscriptions")
		return
	}

	blockResults, err := h.blockService.SearchBlocks(query)
	if err != nil {
		h.sendError(w, http.StatusInternalServerError, "Failed to search blocks")
		return
	}

	response := models.SearchResult{
		Inscriptions: inscriptionResults,
		Blocks:       blockResults,
	}
	h.sendSuccess(w, response)
}

// QRCodeHandler handles QR code generation requests
type QRCodeHandler struct {
	*BaseHandler
	qrService *services.QRCodeService
}

// NewQRCodeHandler creates a new QR code handler
func NewQRCodeHandler(qrService *services.QRCodeService) *QRCodeHandler {
	return &QRCodeHandler{
		BaseHandler: NewBaseHandler(),
		qrService:   qrService,
	}
}

// HandleGenerateQRCode handles QR code generation
func (h *QRCodeHandler) HandleGenerateQRCode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	address := r.URL.Query().Get("address")
	amount := r.URL.Query().Get("amount")

	if address == "" {
		h.sendError(w, http.StatusBadRequest, "Address parameter required")
		return
	}

	qrData, err := h.qrService.GenerateQRCode(address, amount)
	if err != nil {
		h.sendError(w, http.StatusInternalServerError, "Failed to generate QR code")
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Write(qrData)
}

// ProxyHandler handles proxy requests to external services
type ProxyHandler struct {
	*BaseHandler
	targetURL string
}

// NewProxyHandler creates a new proxy handler
func NewProxyHandler(targetURL string) *ProxyHandler {
	return &ProxyHandler{
		BaseHandler: NewBaseHandler(),
		targetURL:   targetURL,
	}
}

// HandleProxy handles proxying requests to the target service
func (h *ProxyHandler) HandleProxy(w http.ResponseWriter, r *http.Request) {
	// Construct the target URL
	target := h.targetURL + r.URL.Path
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}

	// Create new request
	req, err := http.NewRequest(r.Method, target, r.Body)
	if err != nil {
		h.sendError(w, http.StatusInternalServerError, "Failed to create request")
		return
	}

	// Copy headers
	for name, values := range r.Header {
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}

	// Make the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		h.sendError(w, http.StatusBadGateway, "Failed to proxy request")
		return
	}
	defer resp.Body.Close()

	// Copy response headers
	for name, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(name, value)
		}
	}

	// Copy response status and body
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}
