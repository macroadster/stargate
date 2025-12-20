package handlers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	sc "stargate-backend/core/smart_contract"
	scmiddleware "stargate-backend/middleware/smart_contract"
	"stargate-backend/models"
	"stargate-backend/services"
	"stargate-backend/storage"
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
	ingestionService   *services.IngestionService
	proxyBase          string
	store              scmiddleware.Store
}

// NewInscriptionHandler creates a new inscription handler
func NewInscriptionHandler(inscriptionService *services.InscriptionService, ingestionService *services.IngestionService) *InscriptionHandler {
	return &InscriptionHandler{
		BaseHandler:        NewBaseHandler(),
		inscriptionService: inscriptionService,
		ingestionService:   ingestionService,
		proxyBase:          os.Getenv("STARGATE_PROXY_BASE"),
	}
}

// SetStore injects the MCP store so inscriptions can be mirrored into open contracts.
func (h *InscriptionHandler) SetStore(store scmiddleware.Store) {
	h.store = store
}

func placeholderPNG() []byte {
	b64 := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8/5+hHgAHggJ/Ptq4YQAAAABJRU5ErkJggg=="
	data, _ := io.ReadAll(base64.NewDecoder(base64.StdEncoding, strings.NewReader(b64)))
	return data
}

// HandleGetInscriptions handles getting all inscriptions
// @Summary Get all pending inscriptions (smart contracts)
// @Description Get all pending inscriptions (smart contracts)
// @Tags Inscriptions
// @Produce  json
// @Success 200 {object} models.PendingTransactionsResponse
// @Router /api/inscriptions [get]
// @Router /api/pending-transactions [get]
func (h *InscriptionHandler) HandleGetInscriptions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var inscriptions []models.InscriptionRequest
	dedupe := make(map[string]int) // id -> index in inscriptions
	includeLegacyOnly := r.URL.Query().Get("legacy") == "true" || r.URL.Query().Get("legacy") == "1"

	// Prefer open-contracts (MCP store) to keep UI + AI in sync.
	if h.store != nil {
		if contracts, err := h.store.ListContracts("", nil); err == nil {
			for _, c := range contracts {
				if isPendingContractStatus(c.Status) {
					continue
				}
				if h.ingestionService != nil && c.ContractID != "" {
					if rec, err := h.ingestionService.Get(c.ContractID); err == nil {
						if strings.EqualFold(strings.TrimSpace(rec.Status), "confirmed") {
							continue
						}
					}
				}
				item := h.fromContract(c)
				if _, ok := dedupe[item.ID]; !ok {
					dedupe[item.ID] = len(inscriptions)
					inscriptions = append(inscriptions, item)
				}
			}
		} else {
			fmt.Printf("Failed to list contracts for pending view: %v\n", err)
		}
	}

	// Always include ingestion queue to attach images/text; merge into existing items when IDs match.
	if h.ingestionService != nil {
		if recs, err := h.ingestionService.ListRecent("", 200); err == nil {
			for _, rec := range recs {
				item := h.fromIngestion(rec)
				if idx, ok := dedupe[item.ID]; ok {
					// Enrich existing entry with image/text if missing.
					if inscriptions[idx].ImageData == "" && item.ImageData != "" {
						inscriptions[idx].ImageData = item.ImageData
					}
					if inscriptions[idx].Text == "" && item.Text != "" {
						inscriptions[idx].Text = item.Text
					}
					if inscriptions[idx].Status == "" {
						inscriptions[idx].Status = item.Status
					}
				} else if includeLegacyOnly {
					// Only add new ingestion-only rows when legacy flag set
					dedupe[item.ID] = len(inscriptions)
					inscriptions = append(inscriptions, item)
				}
			}
		} else {
			fmt.Printf("Failed to read ingestion records: %v\n", err)
		}
	}

	// Include legacy file-based pending items for compatibility
	if includeLegacyOnly {
		if fileInscriptions, err := h.inscriptionService.GetAllInscriptions(); err == nil {
			for _, ins := range fileInscriptions {
				if idx, ok := dedupe[ins.ID]; ok {
					if inscriptions[idx].ImageData == "" && ins.ImageData != "" {
						inscriptions[idx].ImageData = ins.ImageData
					}
				} else {
					dedupe[ins.ID] = len(inscriptions)
					inscriptions = append(inscriptions, ins)
				}
			}
		} else {
			fmt.Printf("Failed to get file-based inscriptions: %v\n", err)
		}
	}

	response := models.PendingTransactionsResponse{
		Transactions: inscriptions,
		Total:        len(inscriptions),
	}
	h.sendSuccess(w, response)
}

func (h *InscriptionHandler) fromIngestion(rec services.IngestionRecord) models.InscriptionRequest {
	uploadsDir := os.Getenv("UPLOADS_DIR")
	if uploadsDir == "" {
		uploadsDir = "/data/uploads"
	}
	_ = os.MkdirAll(uploadsDir, 0755)

	filename := rec.Filename
	if filename == "" {
		filename = "inscription.png"
	}
	if !strings.HasPrefix(filename, rec.ID+"_") {
		filename = fmt.Sprintf("%s_%s", rec.ID, filename)
	}
	targetPath := filepath.Join(uploadsDir, filename)
	if _, err := os.Stat(targetPath); os.IsNotExist(err) {
		if data, err := base64.StdEncoding.DecodeString(rec.ImageBase64); err == nil {
			if err := os.WriteFile(targetPath, data, 0644); err != nil {
				fmt.Printf("Failed to write ingestion image to %s: %v\n", targetPath, err)
			}
		}
	}

	embeddedMsg := ""
	if msg, ok := rec.Metadata["embedded_message"].(string); ok {
		embeddedMsg = msg
	}
	if msg, ok := rec.Metadata["message"].(string); ok && embeddedMsg == "" {
		embeddedMsg = msg
	}

	return models.InscriptionRequest{
		ImageData: targetPath,
		Text:      embeddedMsg,
		Price:     0,
		Timestamp: rec.CreatedAt.Unix(),
		ID:        rec.ID,
		Status:    rec.Status,
	}
}

func (h *InscriptionHandler) fromContract(c sc.Contract) models.InscriptionRequest {
	uploadsDir := os.Getenv("UPLOADS_DIR")
	if uploadsDir == "" {
		uploadsDir = "/data/uploads"
	}
	imagePath := ""
	if c.ContractID != "" {
		if matches, _ := filepath.Glob(filepath.Join(uploadsDir, c.ContractID+"_*")); len(matches) > 0 {
			imagePath = matches[0]
		}
	}
	return models.InscriptionRequest{
		ImageData: imagePath,
		Text:      c.Title,
		Price:     float64(c.TotalBudgetSats) / 1e8,
		Timestamp: 0,
		ID:        c.ContractID,
		Status:    c.Status,
	}
}

func isPendingContractStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "pending", "claimed", "submitted", "pending_review", "approved", "published", "active":
		return false
	default:
		return true
	}
}

func computeVisiblePixelHash(imageBytes []byte, text string) string {
	sum := sha256.Sum256(append(imageBytes, []byte(text)...))
	return fmt.Sprintf("%x", sum[:])
}

// ensureIngestionImageFile writes the base64 image to uploads dir if missing and returns the path.
func ensureIngestionImageFile(rec services.IngestionRecord) (string, error) {
	uploadsDir := os.Getenv("UPLOADS_DIR")
	if uploadsDir == "" {
		uploadsDir = "/data/uploads"
	}
	if err := os.MkdirAll(uploadsDir, 0755); err != nil {
		return "", err
	}
	target := filepath.Join(uploadsDir, rec.Filename)
	if _, err := os.Stat(target); err == nil {
		return target, nil
	}
	data, err := base64.StdEncoding.DecodeString(rec.ImageBase64)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(target, data, 0644); err != nil {
		return "", err
	}
	return target, nil
}

func (h *InscriptionHandler) upsertOpenContract(visibleHash, title, priceStr string) {
	if h.store == nil || visibleHash == "" {
		return
	}
	priceSat, _ := strconv.ParseInt(priceStr, 10, 64)
	contract := sc.Contract{
		ContractID:          visibleHash,
		Title:               title,
		TotalBudgetSats:     priceSat,
		GoalsCount:          0,
		AvailableTasksCount: 0,
		Status:              "pending",
	}

	// Prefer stores that expose UpsertContractWithTasks for idempotency.
	type upserter interface {
		UpsertContractWithTasks(ctx context.Context, contract sc.Contract, tasks []sc.Task) error
	}
	if u, ok := h.store.(upserter); ok {
		_ = u.UpsertContractWithTasks(context.Background(), contract, nil)
	}
}

// HandleCreateInscription handles creating a new inscription
func (h *InscriptionHandler) HandleCreateInscription(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Parse multipart form
	contentType := r.Header.Get("Content-Type")
	bodyBytes, _ := io.ReadAll(r.Body)
	defer r.Body.Close()
	isJSON := strings.HasPrefix(contentType, "application/json") || strings.HasPrefix(contentType, "application/json;") || (len(bodyBytes) > 0 && strings.TrimSpace(string(bodyBytes))[0] == '{')

	var (
		text        string
		method      string
		price       string
		address     string
		filename    string
		imgBytes    []byte
		imageErr    error
		imageReader io.ReadCloser
	)

	if isJSON {
		var payload struct {
			Message     string `json:"message"`
			Text        string `json:"text"`
			Method      string `json:"method"`
			Price       string `json:"price"`
			Address     string `json:"address"`
			ImageBase64 string `json:"image_base64"`
			Filename    string `json:"filename"`
		}
		if err := json.Unmarshal(bodyBytes, &payload); err != nil {
			h.sendError(w, http.StatusBadRequest, "Invalid JSON")
			return
		}
		text = payload.Message
		if text == "" {
			text = payload.Text
		}
		method = payload.Method
		price = payload.Price
		address = payload.Address
		filename = payload.Filename
		if method == "" {
			method = "alpha"
		}
		if payload.ImageBase64 != "" {
			imgBytes, imageErr = base64.StdEncoding.DecodeString(payload.ImageBase64)
			if imageErr != nil {
				h.sendError(w, http.StatusBadRequest, "Invalid base64 image")
				return
			}
		}
	} else {
		// Reset the body for multipart parsing
		r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			h.sendError(w, http.StatusBadRequest, "Failed to parse form")
			return
		}

		text = r.FormValue("text")
		if text == "" {
			text = r.FormValue("message")
		}
		method = r.FormValue("method")
		if method == "" {
			method = "alpha"
		}
		price = r.FormValue("price")
		address = r.FormValue("address")

		// Get file (optional)
		file, header, err := r.FormFile("image")
		if err != nil && err != http.ErrMissingFile {
			h.sendError(w, http.StatusBadRequest, "Error processing image file")
			return
		}
		if err == nil {
			imageReader = file
			filename = header.Filename
		}
	}

	if text == "" {
		h.sendError(w, http.StatusBadRequest, "Message is required for inscription")
		return
	}

	if imageErr != nil {
		h.sendError(w, http.StatusBadRequest, "Invalid image data")
		return
	}

	if price == "" {
		price = "0"
	}

	// Slurp image bytes from multipart file if present
	if imageReader != nil {
		defer imageReader.Close()
		if b, err := io.ReadAll(imageReader); err == nil {
			imgBytes = b
		} else {
			imageErr = err
		}
	}

	// Ensure we have image bytes & filename for downstream hashing/storage
	if len(imgBytes) == 0 {
		imgBytes = placeholderPNG()
		if filename == "" {
			filename = "placeholder.png"
		}
	}
	visibleHash := computeVisiblePixelHash(imgBytes, text)
	ingestionID := visibleHash
	if ingestionID == "" {
		ingestionID = fmt.Sprintf("pending_%d", time.Now().UnixNano())
	}

	// Seed ingestion + MCP contract before proxy so both UIs see it even on proxy success.
	if h.ingestionService != nil {
		imgB64 := base64.StdEncoding.EncodeToString(imgBytes)
		skipCreate := false
		if rec, err := h.ingestionService.GetByImageAndMessage(imgB64, text); err == nil && rec != nil {
			if visibleHash != "" && rec.ID != visibleHash {
				if err := h.ingestionService.UpdateID(rec.ID, visibleHash); err != nil {
					fmt.Printf("Failed to update ingestion id from %s to %s: %v\n", rec.ID, visibleHash, err)
					ingestionID = rec.ID
				} else {
					ingestionID = visibleHash
				}
			} else {
				ingestionID = rec.ID
			}
			if visibleHash != "" {
				if err := h.ingestionService.UpdateMetadata(ingestionID, map[string]interface{}{"visible_pixel_hash": visibleHash}); err != nil {
					fmt.Printf("Failed to update ingestion metadata for %s: %v\n", ingestionID, err)
				}
			}
			skipCreate = true
		}
		meta := map[string]interface{}{
			"embedded_message":   text,
			"message":            text,
			"price":              price,
			"address":            address,
			"ingestion_id":       ingestionID,
			"visible_pixel_hash": visibleHash,
		}
		if !skipCreate {
			ingRec := services.IngestionRecord{
				ID:            ingestionID,
				Filename:      filename,
				Method:        method,
				MessageLength: len(text),
				ImageBase64:   imgB64,
				Metadata:      meta,
				Status:        "pending",
			}
			if ingRec.Filename == "" {
				ingRec.Filename = "inscription.png"
			}
			if err := h.ingestionService.Create(ingRec); err != nil {
				fmt.Printf("Failed to create ingestion record for %s: %v\n", ingestionID, err)
			}
		}
	}
	// Mirror into MCP contracts/open-contracts for AI + UI consistency.
	h.upsertOpenContract(visibleHash, text, price)

	// Proxy to starlight /inscribe to avoid direct frontend → Python exposure
	if h.proxyBase != "" {
		var buf bytes.Buffer
		writer := multipart.NewWriter(&buf)

		// File part (required by starlight)
		part, _ := writer.CreateFormFile("image", filename)
		part.Write(imgBytes)

		// Text & method (embed message, price, address as JSON string)
		embedPayload := map[string]string{
			"message": text,
			"price":   price,
			"address": address,
		}
		if msgJSON, err := json.Marshal(embedPayload); err == nil {
			writer.WriteField("message", string(msgJSON))
		} else {
			writer.WriteField("message", text)
		}
		writer.WriteField("method", method)
		writer.Close()

		proxyURL := fmt.Sprintf("%s/inscribe", strings.TrimRight(h.proxyBase, "/"))
		req, _ := http.NewRequest(http.MethodPost, proxyURL, &buf)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		if apiKey := os.Getenv("STARGATE_API_KEY"); apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}

		resp, err := http.DefaultClient.Do(req)
		if err == nil && resp != nil {
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				// We already mirrored to MCP; just return proxy response.
				w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
				w.WriteHeader(resp.StatusCode)
				w.Write(body)
				return
			}
			// log and fall through to local success to avoid 500 to UI
			fmt.Printf("Starlight proxy responded %d: %s\n", resp.StatusCode, string(body))
		} else {
			fmt.Printf("Starlight proxy error: %v\n", err)
		}
	}

	// Fallback to legacy local inscription creation
	req := models.InscribeRequest{
		Text:    text,
		Price:   price,
		Address: address,
	}
	fallbackBytes := imgBytes
	if len(fallbackBytes) == 0 {
		fallbackBytes = placeholderPNG()
		if filename == "" {
			filename = "placeholder.png"
		}
	}

	inscription, err := h.inscriptionService.CreateInscription(req, io.NopCloser(bytes.NewReader(fallbackBytes)), filename)
	if err != nil {
		h.sendError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to create inscription: %v", err))
		return
	}

	// Auto-create ingestion record for MCP sync so proposals are generated.
	if h.ingestionService != nil {
		// Already created ingestion/upsert above; skip duplicate creation here.
	}

	h.sendSuccess(w, map[string]string{
		"status":             "success",
		"id":                 inscription.ID,
		"ingestion_id":       ingestionID,
		"visible_pixel_hash": visibleHash,
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
	store           scmiddleware.Store
	ingestion       *services.IngestionService
}

// NewSmartContractHandler creates a new smart contract handler
func NewSmartContractHandler(contractService *services.SmartContractService, store scmiddleware.Store, ingestion *services.IngestionService) *SmartContractHandler {
	return &SmartContractHandler{
		BaseHandler:     NewBaseHandler(),
		contractService: contractService,
		store:           store,
		ingestion:       ingestion,
	}
}

// HandleGetContracts handles getting all smart contracts
func (h *SmartContractHandler) HandleGetContracts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Use the MCP store to get contracts instead of the service
	contracts, err := h.store.ListContracts("", nil)
	if err != nil {
		h.sendError(w, http.StatusInternalServerError, "Failed to get contracts")
		return
	}

	// Convert smart_contract.Contract to models.SmartContractImage for API compatibility
	var results []models.SmartContractImage
	for _, contract := range contracts {
		result := models.SmartContractImage{
			ContractID:   contract.ContractID,
			BlockHeight:  0,  // Not available in Contract struct
			StegoImage:   "", // Not available in Contract struct
			ContractType: "smart_contract",
			Metadata: map[string]interface{}{
				"title":             contract.Title,
				"total_budget_sats": contract.TotalBudgetSats,
				"goals_count":       contract.GoalsCount,
				"available_tasks":   contract.AvailableTasksCount,
				"status":            contract.Status,
			},
		}

		// Enrich with ingestion image (stego) if available.
		if h.ingestion != nil {
			if rec, err := h.ingestion.Get(contract.ContractID); err == nil {
				if stegoPath, serr := ensureIngestionImageFile(*rec); serr == nil {
					url := "/uploads/" + filepath.Base(stegoPath)
					result.StegoImage = url
					result.Metadata["stego_image_url"] = url
					result.Metadata["ingestion_id"] = rec.ID
				}
				if v, ok := rec.Metadata["visible_pixel_hash"].(string); ok && strings.TrimSpace(v) != "" {
					result.VisiblePixelHash = strings.TrimSpace(v)
					result.Metadata["visible_pixel_hash"] = result.VisiblePixelHash
				}
			}
		}

		results = append(results, result)
	}

	response := models.SmartContractsResponse{
		Results: results,
		Total:   len(results),
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
	dataStorage        storage.ExtendedDataStorage
}

// NewSearchHandler creates a new search handler
func NewSearchHandler(inscriptionService *services.InscriptionService, blockService *services.BlockService, dataStorage storage.ExtendedDataStorage) *SearchHandler {
	return &SearchHandler{
		BaseHandler:        NewBaseHandler(),
		inscriptionService: inscriptionService,
		blockService:       blockService,
		dataStorage:        dataStorage,
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
		h.sendSuccess(w, h.recentBlocksResponse(query))
		return
	}

	// Search inscriptions and blocks
	h.sendSuccess(w, h.searchData(query))
}

func (h *SearchHandler) recentBlocksResponse(query string) models.SearchResult {
	result := h.searchData(query)
	if len(result.Blocks) > 5 {
		result.Blocks = result.Blocks[:5]
	}
	if len(result.Inscriptions) > 5 {
		result.Inscriptions = result.Inscriptions[:5]
	}
	return result
}

func (h *SearchHandler) searchData(query string) models.SearchResult {
	q := strings.ToLower(strings.TrimSpace(query))
	var blocks []interface{}
	var inscriptions []models.InscriptionRequest
	var contracts []models.SmartContractImage
	seenInscriptions := make(map[string]bool)
	seenContracts := make(map[string]bool)

	addInscription := func(id, text string, ts int64) {
		if id == "" || seenInscriptions[id] {
			return
		}
		seenInscriptions[id] = true
		inscriptions = append(inscriptions, models.InscriptionRequest{
			ID:        id,
			Status:    "confirmed",
			Text:      text,
			Price:     0,
			Timestamp: ts,
		})
	}

	matchesQuery := func(values ...string) bool {
		if q == "" {
			return true
		}
		for _, v := range values {
			if v == "" {
				continue
			}
			if strings.Contains(strings.ToLower(v), q) {
				return true
			}
		}
		return false
	}

	metaString := func(meta map[string]any, key string) string {
		if meta == nil {
			return ""
		}
		if v, ok := meta[key]; ok && v != nil {
			return strings.TrimSpace(fmt.Sprintf("%v", v))
		}
		return ""
	}

	addContract := func(id string, height int64, imageURL string, contractType string, visibleHash string, meta map[string]any) {
		if id == "" || seenContracts[id] {
			return
		}
		seenContracts[id] = true
		contracts = append(contracts, models.SmartContractImage{
			ContractID:       id,
			BlockHeight:      height,
			StegoImage:       imageURL,
			ContractType:     contractType,
			VisiblePixelHash: visibleHash,
			Metadata:         meta,
		})
	}

	if h.dataStorage != nil {
		if recent, err := h.dataStorage.GetRecentBlocks(200); err == nil {
			for _, b := range recent {
				if cache, ok := b.(*storage.BlockDataCache); ok {
					if matchesQuery(cache.BlockHash, fmt.Sprintf("%d", cache.BlockHeight)) {
						blocks = append(blocks, map[string]interface{}{
							"id":        cache.BlockHash,
							"height":    cache.BlockHeight,
							"timestamp": cache.Timestamp,
							"tx_count":  cache.TxCount,
						})
					}
					for _, ins := range cache.Inscriptions {
						if matchesQuery(ins.TxID, ins.FileName, ins.FilePath, ins.Content, ins.ContentType) {
							addInscription(ins.TxID, ins.Content, cache.Timestamp)
						}
					}
					for _, img := range cache.Images {
						if matchesQuery(img.TxID, img.FileName, img.FilePath, img.ContentType) {
							addInscription(img.TxID, "", cache.Timestamp)
						}
					}
					for _, sc := range cache.SmartContracts {
						meta := sc.Metadata
						text := metaString(meta, "embedded_message")
						if text == "" {
							text = metaString(meta, "message")
						}
						status := strings.ToLower(metaString(meta, "confirmation_status"))
						if status == "confirmed" {
							continue
						}
						id := metaString(meta, "confirmed_txid")
						if id == "" {
							id = metaString(meta, "tx_id")
						}
						if id == "" {
							id = metaString(meta, "funding_txid")
						}
						if id == "" {
							id = metaString(meta, "visible_pixel_hash")
						}
						if id == "" {
							id = metaString(meta, "contract_id")
						}
						if id == "" {
							id = sc.ContractID
						}
						imageFile := metaString(meta, "image_file")
						if imageFile == "" {
							imageFile = filepath.Base(metaString(meta, "image_path"))
						}
						if imageFile == "" {
							imageFile = filepath.Base(strings.TrimSpace(sc.ImagePath))
						}
						imageURL := ""
						if imageFile != "" {
							imageURL = fmt.Sprintf("/api/block-image/%d/%s", cache.BlockHeight, imageFile)
						}
						visibleHash := metaString(meta, "visible_pixel_hash")
						if visibleHash == "" {
							visibleHash = metaString(meta, "pixel_hash")
						}
						if matchesQuery(
							sc.ContractID,
							metaString(meta, "contract_id"),
							metaString(meta, "ingestion_id"),
							metaString(meta, "visible_pixel_hash"),
							metaString(meta, "confirmed_txid"),
							metaString(meta, "tx_id"),
							metaString(meta, "funding_txid"),
							metaString(meta, "image_file"),
							metaString(meta, "image_path"),
							text,
						) {
							addInscription(id, text, cache.Timestamp)
							addContract(id, cache.BlockHeight, imageURL, "Smart Contract", visibleHash, meta)
						}
					}
				}
			}
		}
	}

	// Fallback to service search if nothing found or explicit query
	if len(blocks) == 0 {
		if svcBlocks, err := h.blockService.SearchBlocks(query); err == nil {
			for _, b := range svcBlocks {
				blocks = append(blocks, b)
			}
		}
	}
	if len(inscriptions) == 0 {
		if svcInscriptions, err := h.inscriptionService.SearchInscriptions(query); err == nil {
			for _, ins := range svcInscriptions {
				inscriptions = append(inscriptions, models.InscriptionRequest{
					ID:        ins.ID,
					Text:      ins.Text,
					Price:     ins.Price,
					Timestamp: ins.Timestamp,
					Status:    ins.Status,
				})
			}
		}
	}

	return models.SearchResult{
		Inscriptions: inscriptions,
		Blocks:       blocks,
		Contracts:    contracts,
	}
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
