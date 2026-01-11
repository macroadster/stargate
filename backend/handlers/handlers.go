package handlers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	sc "stargate-backend/core/smart_contract"
	"stargate-backend/ipfs"
	scmiddleware "stargate-backend/middleware/smart_contract"
	"stargate-backend/models"
	"stargate-backend/services"
	"stargate-backend/storage"
	scstore "stargate-backend/storage/smart_contract"
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
		if contracts, err := h.store.ListContracts(sc.ContractFilter{}); err == nil {
			for _, c := range contracts {
				if strings.EqualFold(strings.TrimSpace(c.Status), "superseded") {
					continue
				}
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
				if looksLikeStegoManifestText(item.Text) {
					continue
				}
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
				} else if idx, ok := dedupe[wishContractID(item.ID)]; ok {
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

	sort.SliceStable(inscriptions, func(i, j int) bool {
		return inscriptions[i].Timestamp > inscriptions[j].Timestamp
	})

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
	if _, err := os.Stat(targetPath); err == nil {
		// ok
	} else if isUploadTombstoned(uploadsDir, filepath.Base(filename)) {
		targetPath = ""
	} else if os.IsNotExist(err) {
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
	embeddedMsg = stripWishTimestamp(embeddedMsg)

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
	timestamp := int64(0)
	if c.ContractID != "" {
		baseID := baseContractID(c.ContractID)
		if matches, _ := filepath.Glob(filepath.Join(uploadsDir, baseID+"_*")); len(matches) > 0 {
			imagePath = matches[0]
		}
	}
	wishText := ""
	if h.ingestionService != nil {
		ingestionID := strings.TrimSpace(c.ContractID)
		rec, err := h.ingestionService.Get(ingestionID)
		if err != nil {
			if baseID := baseContractID(ingestionID); baseID != "" && baseID != ingestionID {
				rec, _ = h.ingestionService.Get(baseID)
			}
		}
		if rec != nil {
			timestamp = rec.CreatedAt.Unix()
			if v, ok := rec.Metadata["wish_text"].(string); ok {
				wishText = strings.TrimSpace(v)
			}
			if wishText == "" {
				if v, ok := rec.Metadata["embedded_message"].(string); ok {
					wishText = strings.TrimSpace(v)
				}
			}
			if wishText == "" {
				if v, ok := rec.Metadata["message"].(string); ok {
					wishText = strings.TrimSpace(v)
				}
			}
			if v, ok := rec.Metadata["wish_timestamp"].(float64); ok && v > 0 {
				timestamp = int64(v)
			}
			if v, ok := rec.Metadata["wish_timestamp"].(string); ok && strings.TrimSpace(v) != "" {
				if parsed, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
					timestamp = parsed
				}
			}
		}
	}
	wishText = stripWishTimestamp(wishText)
	text := strings.TrimSpace(c.Title)
	if wishText != "" {
		text = wishText
	}
	return models.InscriptionRequest{
		ImageData: imagePath,
		Text:      text,
		Price:     float64(c.TotalBudgetSats) / 1e8,
		Timestamp: timestamp,
		ID:        c.ContractID,
		Status:    c.Status,
	}
}

func (h *InscriptionHandler) fromProposal(p sc.Proposal) models.InscriptionRequest {
	uploadsDir := os.Getenv("UPLOADS_DIR")
	if uploadsDir == "" {
		uploadsDir = "/data/uploads"
	}
	imagePath := ""
	baseID := baseContractID(p.ID)
	if baseID == "" {
		baseID = strings.TrimSpace(p.VisiblePixelHash)
	}
	if baseID == "" {
		if v, ok := p.Metadata["visible_pixel_hash"].(string); ok {
			baseID = strings.TrimSpace(v)
		}
	}
	if baseID != "" {
		if matches, _ := filepath.Glob(filepath.Join(uploadsDir, baseID+"_*")); len(matches) > 0 {
			imagePath = matches[0]
		}
	}
	wishText := ""
	if v, ok := p.Metadata["wish_text"].(string); ok {
		wishText = strings.TrimSpace(v)
	}
	if wishText == "" {
		if v, ok := p.Metadata["embedded_message"].(string); ok {
			wishText = strings.TrimSpace(v)
		}
	}
	if wishText == "" {
		if v, ok := p.Metadata["message"].(string); ok {
			wishText = strings.TrimSpace(v)
		}
	}
	wishText = stripWishTimestamp(wishText)
	text := strings.TrimSpace(p.DescriptionMD)
	if wishText != "" {
		text = wishText
	}
	if text == "" {
		text = p.Title
	}
	return models.InscriptionRequest{
		ImageData: imagePath,
		Text:      text,
		Price:     float64(p.BudgetSats) / 1e8,
		Timestamp: p.CreatedAt.Unix(),
		ID:        p.ID,
		Status:    p.Status,
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

func normalizeWishText(text string) string {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "#")
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	return strings.ToLower(strings.Join(strings.Fields(text), " "))
}

func wishKeyFromText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	line := text
	if idx := strings.IndexRune(text, '\n'); idx >= 0 {
		line = text[:idx]
	}
	return normalizeWishText(line)
}

func looksLikeStegoManifestText(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "schema_version:") &&
		strings.Contains(lower, "proposal_id:") &&
		strings.Contains(lower, "visible_pixel_hash:")
}

func proposalContractID(p sc.Proposal) string {
	if v, ok := p.Metadata["visible_pixel_hash"].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	if v, ok := p.Metadata["contract_id"].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	if v, ok := p.Metadata["ingestion_id"].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	if strings.TrimSpace(p.VisiblePixelHash) != "" {
		return strings.TrimSpace(p.VisiblePixelHash)
	}
	return baseContractID(p.ID)
}

func ingestionContractID(rec services.IngestionRecord) string {
	if v, ok := rec.Metadata["visible_pixel_hash"].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	if v, ok := rec.Metadata["contract_id"].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	if v, ok := rec.Metadata["ingestion_id"].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return baseContractID(rec.ID)
}

func isRejectedProposalStatus(status string) bool {
	return strings.EqualFold(strings.TrimSpace(status), "rejected")
}

func appendWishTimestamp(message string, ts int64) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return message
	}
	return fmt.Sprintf("%s\n\n[stargate-ts:%d]", message, ts)
}

func stripWishTimestamp(message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return message
	}
	idx := strings.LastIndex(message, "\n\n[stargate-ts:")
	if idx < 0 {
		return message
	}
	return strings.TrimSpace(message[:idx])
}

func computeVisiblePixelHash(imageBytes []byte, text string) string {
	sum := sha256.Sum256(append(imageBytes, []byte(text)...))
	return fmt.Sprintf("%x", sum[:])
}

func wishContractID(visibleHash string) string {
	visibleHash = strings.TrimSpace(visibleHash)
	if visibleHash == "" {
		return ""
	}
	return "wish-" + visibleHash
}

func baseContractID(contractID string) string {
	contractID = strings.TrimSpace(contractID)
	if strings.HasPrefix(contractID, "wish-") {
		return strings.TrimPrefix(contractID, "wish-")
	}
	return contractID
}

func resolveStegoMethod(requested, filename string, image []byte) string {
	requested = strings.TrimSpace(strings.ToLower(requested))
	ext := strings.ToLower(filepath.Ext(filename))
	if ext == "" && len(image) > 0 {
		switch http.DetectContentType(image) {
		case "image/jpeg":
			ext = ".jpg"
		case "image/png":
			ext = ".png"
		case "image/gif":
			ext = ".gif"
		}
	}
	if isMethodCompatible(requested, ext) {
		return requested
	}
	switch ext {
	case ".jpg", ".jpeg":
		if requested == "eoi" {
			return requested
		}
		return "exif"
	case ".gif":
		return "palette"
	case ".png":
		return "alpha"
	default:
		if requested != "" {
			return requested
		}
		return "alpha"
	}
}

func isMethodCompatible(method, ext string) bool {
	if method == "" {
		return false
	}
	switch ext {
	case ".jpg", ".jpeg":
		return method == "exif" || method == "eoi"
	case ".gif":
		return method == "palette"
	case ".png":
		return method == "alpha" || method == "lsb"
	default:
		return true
	}
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
	if isUploadTombstoned(uploadsDir, filepath.Base(rec.Filename)) {
		return "", fmt.Errorf("upload tombstoned")
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

func isUploadTombstoned(uploadsDir, filename string) bool {
	if uploadsDir == "" || filename == "" {
		return false
	}
	path := filepath.Join(uploadsDir, ".ipfs-mirror-deleted.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var payload struct {
		Paths []string `json:"paths"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return false
	}
	for _, rel := range payload.Paths {
		if filepath.Base(rel) == filename {
			return true
		}
	}
	return false
}

func (h *InscriptionHandler) upsertOpenContract(visibleHash, title, priceStr string) {
	if h.store == nil || visibleHash == "" {
		return
	}
	priceSat := parsePriceSats(priceStr)
	contract := sc.Contract{
		ContractID:          wishContractID(visibleHash),
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

func parsePriceSats(raw string) int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	if strings.Contains(raw, ".") {
		if v, err := strconv.ParseFloat(raw, 64); err == nil {
			return int64(v * 1e8)
		}
		return 0
	}
	if v, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return v
	}
	return 0
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
		priceUnit   string
		address     string
		fundingMode string
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
			PriceUnit   string `json:"price_unit"`
			Address     string `json:"address"`
			FundingMode string `json:"funding_mode"`
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
		priceUnit = payload.PriceUnit
		address = payload.Address
		fundingMode = payload.FundingMode
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
			if filename == "" {
				filename = "image.png"
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
		priceUnit = r.FormValue("price_unit")
		address = r.FormValue("address")
		fundingMode = r.FormValue("funding_mode")

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
	if priceUnit == "" {
		priceUnit = "btc"
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
	method = resolveStegoMethod(method, filename, imgBytes)
	wishTimestamp := time.Now().Unix()
	embeddedMessage := appendWishTimestamp(text, wishTimestamp)

	// Proxy to starlight /inscribe to avoid direct frontend → Python exposure
	if h.proxyBase != "" {
		fmt.Printf("DEBUG: Proxy path selected, proxyBase=%s\n", h.proxyBase)
		var buf bytes.Buffer
		writer := multipart.NewWriter(&buf)

		// File part (required by starlight) - use io.Copy like working implementation
		part, _ := writer.CreateFormFile("image", filename)
		if len(imgBytes) > 0 {
			io.Copy(part, bytes.NewReader(imgBytes))
		} else {
			// Use placeholder if no image provided
			io.Copy(part, bytes.NewReader(placeholderPNG()))
		}

		// Message & method (simple text message like working implementation)
		writer.WriteField("message", embeddedMessage)
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
				// Parse Starlight response to extract stego hash and image
				var starlightResponse struct {
					RequestID   string `json:"request_id"`
					ID          string `json:"id"`
					ImageSHA256 string `json:"image_sha256"`
					ImageBase64 string `json:"image_base64"`
				}

				if err := json.Unmarshal(body, &starlightResponse); err == nil && starlightResponse.ImageSHA256 != "" {
					ingestionID := starlightResponse.ImageSHA256

					meta := map[string]interface{}{
						"embedded_message":     embeddedMessage,
						"message":              text,
						"wish_timestamp":       wishTimestamp,
						"price":                price,
						"price_unit":           priceUnit,
						"address":              address,
						"funding_mode":         fundingMode,
						"starlight_request_id": starlightResponse.RequestID,
					}
					if strings.EqualFold(priceUnit, "sats") {
						meta["budget_sats"] = parsePriceSats(price)
					}

					if h.ingestionService != nil {
						imgB64 := base64.StdEncoding.EncodeToString(imgBytes)
						if starlightResponse.ImageBase64 != "" {
							imgB64 = starlightResponse.ImageBase64
						}
						ingRec := services.IngestionRecord{
							ID:            ingestionID,
							Filename:      filename,
							Method:        method,
							MessageLength: len(embeddedMessage),
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
						publishPendingIngestAnnouncement(ingestionID, ingestionID, filename, method, embeddedMessage, price, priceUnit, address, fundingMode, imgBytes)
					}

					if h.store != nil {
						ctx := context.Background()
						proposalID := "proposal-" + starlightResponse.ImageSHA256
						budget := parsePriceSats(price)
						fundingAddr := scstore.FundingAddressFromMeta(meta)

						tasks := scstore.BuildTasksFromMarkdown(proposalID, embeddedMessage, starlightResponse.ImageSHA256, budget, fundingAddr)

						proposalMeta := map[string]interface{}{
							"ingestion_id":       ingestionID,
							"visible_pixel_hash": starlightResponse.ImageSHA256,
							"budget_sats":        budget,
							"funding_address":    fundingAddr,
						}

						if starlightResponse.ImageBase64 != "" {
							stegoBytes, err := base64.StdEncoding.DecodeString(starlightResponse.ImageBase64)
							if err == nil {
								stegoSum := sha256.Sum256(stegoBytes)
								stegoContractID := hex.EncodeToString(stegoSum[:])
								proposalMeta["stego_contract_id"] = stegoContractID
							}
						}

						proposalTitle := strings.TrimSpace(text)
						if strings.HasPrefix(proposalTitle, "#") {
							proposalTitle = strings.TrimSpace(strings.TrimLeft(proposalTitle, "#"))
						}
						if proposalTitle == "" {
							proposalTitle = "Wish " + starlightResponse.ImageSHA256
						}

						proposal := sc.Proposal{
							ID:               proposalID,
							Title:            proposalTitle,
							DescriptionMD:    embeddedMessage,
							VisiblePixelHash: starlightResponse.ImageSHA256,
							Tasks:            tasks,
							Status:           "pending",
							CreatedAt:        time.Now(),
							Metadata:         proposalMeta,
						}

						if err := h.store.CreateProposal(ctx, proposal); err != nil {
							fmt.Printf("Failed to create proposal: %v\n", err)
						}

						contractID := "wish-" + starlightResponse.ImageSHA256
						if u, ok := h.store.(interface {
							UpsertContractWithTasks(ctx context.Context, contract sc.Contract, tasks []sc.Task) error
						}); ok {
							_ = u.UpsertContractWithTasks(context.Background(), sc.Contract{
								ContractID:      contractID,
								Title:           proposalTitle,
								TotalBudgetSats: budget,
								GoalsCount:      0,
								Status:          "pending",
							}, nil)
						}
					}

					h.sendSuccess(w, map[string]string{
						"status":             "success",
						"id":                 ingestionID,
						"ingestion_id":       ingestionID,
						"visible_pixel_hash": starlightResponse.ImageSHA256,
					})
					return
				}
			}

			// Starlight-api returned an error - forward the error to client
			// Don't fall back to local inscription - that causes duplicate proposal creation
			w.WriteHeader(resp.StatusCode)
			w.Write(body)
			return
		}
	}

	// Only use fallback path if proxy is not configured
	// Fallback to legacy local inscription creation
	// Do NOT create proposals when starlight-api returns error - that causes duplicate proposals
	fmt.Printf("DEBUG: Taking fallback path (proxy not configured or starlight-api error)\n")
	visibleHash := computeVisiblePixelHash(imgBytes, embeddedMessage)
	req := models.InscribeRequest{
		Text:    embeddedMessage,
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

	h.sendSuccess(w, map[string]string{
		"status":             "success",
		"id":                 inscription.ID,
		"ingestion_id":       visibleHash,
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

func includeConfirmedQuery(r *http.Request) bool {
	raw := strings.TrimSpace(r.URL.Query().Get("include_confirmed"))
	if raw == "" {
		return false
	}
	return strings.EqualFold(raw, "true") || strings.EqualFold(raw, "yes") || raw == "1"
}

func proofConfirmed(proof *sc.MerkleProof) bool {
	if proof == nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(proof.ConfirmationStatus), "confirmed") {
		return true
	}
	if proof.ConfirmedAt != nil {
		return true
	}
	return false
}

func proofsConfirmed(proofs []sc.MerkleProof) bool {
	for i := range proofs {
		if proofConfirmed(&proofs[i]) {
			return true
		}
	}
	return false
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
	contracts, err := h.store.ListContracts(sc.ContractFilter{})
	if err != nil {
		h.sendError(w, http.StatusInternalServerError, "Failed to get contracts")
		return
	}

	// Convert smart_contract.Contract to models.SmartContractImage for API compatibility
	var results []models.SmartContractImage
	includeConfirmed := includeConfirmedQuery(r)
	for _, contract := range contracts {
		if !includeConfirmed {
			_, proofs, err := h.store.ContractFunding(contract.ContractID)
			if err != nil {
				log.Printf("contract funding lookup failed for %s: %v", contract.ContractID, err)
			} else if proofsConfirmed(proofs) {
				continue
			}
		}
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
	metaFundingTxIDs := func(meta map[string]any) []string {
		var txids []string
		add := func(value string) {
			value = strings.TrimSpace(value)
			if value == "" {
				return
			}
			for _, existing := range txids {
				if existing == value {
					return
				}
			}
			txids = append(txids, value)
		}
		if meta == nil {
			return txids
		}
		add(metaString(meta, "funding_txid"))
		switch v := meta["funding_txids"].(type) {
		case []string:
			for _, txid := range v {
				add(txid)
			}
		case []any:
			for _, item := range v {
				if txid, ok := item.(string); ok {
					add(txid)
				}
			}
		case string:
			for _, part := range strings.Split(v, ",") {
				add(part)
			}
		}
		return txids
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
							if id == "" {
								if txids := metaFundingTxIDs(meta); len(txids) > 0 {
									id = txids[0]
								}
							}
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
							strings.Join(metaFundingTxIDs(meta), " "),
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

type pendingIngestAnnouncement struct {
	Type             string `json:"type"`
	IngestionID      string `json:"ingestion_id"`
	VisiblePixelHash string `json:"visible_pixel_hash,omitempty"`
	ImageCID         string `json:"image_cid"`
	Filename         string `json:"filename,omitempty"`
	Method           string `json:"method,omitempty"`
	Message          string `json:"message,omitempty"`
	Price            string `json:"price,omitempty"`
	PriceUnit        string `json:"price_unit,omitempty"`
	Address          string `json:"address,omitempty"`
	FundingMode      string `json:"funding_mode,omitempty"`
	Timestamp        int64  `json:"timestamp"`
}

func publishPendingIngestAnnouncement(ingestionID, visibleHash, filename, method, message, price, priceUnit, address, fundingMode string, imgBytes []byte) {
	if !ipfsIngestSyncEnabled() {
		return
	}
	if strings.TrimSpace(ingestionID) == "" || len(imgBytes) == 0 {
		return
	}
	topic := strings.TrimSpace(os.Getenv("IPFS_MIRROR_TOPIC"))
	if topic == "" {
		topic = "stargate-uploads"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client := ipfs.NewClientFromEnv()
	ext := filepath.Ext(filename)
	if ext == "" {
		ext = ".png"
	}
	name := fmt.Sprintf("pending-%s%s", ingestionID, ext)
	imageCID, err := client.AddBytes(ctx, name, imgBytes)
	if err != nil {
		log.Printf("pending ingest announce: ipfs add failed for %s: %v", ingestionID, err)
		return
	}
	ann := pendingIngestAnnouncement{
		Type:             "pending_ingest",
		IngestionID:      ingestionID,
		VisiblePixelHash: strings.TrimSpace(visibleHash),
		ImageCID:         imageCID,
		Filename:         filename,
		Method:           method,
		Message:          message,
		Price:            price,
		PriceUnit:        priceUnit,
		Address:          address,
		FundingMode:      fundingMode,
		Timestamp:        time.Now().Unix(),
	}
	payload, err := json.Marshal(ann)
	if err != nil {
		log.Printf("pending ingest announce: marshal failed for %s: %v", ingestionID, err)
		return
	}
	if err := client.PubsubPublish(ctx, topic, payload); err != nil {
		log.Printf("pending ingest announce: publish failed for %s: %v", ingestionID, err)
	}
}

func ipfsIngestSyncEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("IPFS_INGEST_SYNC_ENABLED")), "true")
}

func (h *InscriptionHandler) updateContractID(oldHash, newContractID string) {
	// Update the contract ID in the database to match the new stego hash
	if h.store != nil {
		// Try to get the existing contract by old ID first
		contract, err := h.store.GetContract(oldHash)
		if err != nil {
			// Create new contract if it doesn't exist
			contract = sc.Contract{
				ContractID: newContractID,
				Title:      "Auto-generated wish",
				Status:     "pending",
			}
		} else {
			// Update existing contract with new ID
			contract.ContractID = newContractID
		}

		// Use the available upsert method
		if upserter, ok := h.store.(interface {
			UpsertContractWithTasks(ctx context.Context, contract sc.Contract, tasks []sc.Task) error
		}); ok {
			if err := upserter.UpsertContractWithTasks(context.Background(), contract, nil); err != nil {
				fmt.Printf("Failed to update contract ID from %s to %s: %v\n", oldHash, newContractID, err)
			} else {
				fmt.Printf("Updated contract ID from %s to %s\n", oldHash, newContractID)
			}
		} else {
			fmt.Printf("Store does not support UpsertContractWithTasks\n")
		}
	}
}
