package handlers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
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

	_ "image/gif"
	_ "image/jpeg"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"

	sc "stargate-backend/core/smart_contract"
	scmiddleware "stargate-backend/app/smart_contract"
	"stargate-backend/models"
	"stargate-backend/security"
	"stargate-backend/services"
	"stargate-backend/stego"
	auth "stargate-backend/storage/auth"
	"stargate-backend/storage/ipfs"
)

// InscriptionHandler handles inscription-related requests
type InscriptionHandler struct {
	*BaseHandler
	inscriptionService *services.InscriptionService
	ingestionService   *services.IngestionService
	proxyBase          string
	store              scmiddleware.Store
	apiKeyIssuer       auth.APIKeyIssuer
	apiKeyValidator    auth.APIKeyValidator
	RequireImage       bool
}

// NewInscriptionHandler creates a new inscription handler
func NewInscriptionHandler(inscriptionService *services.InscriptionService, ingestionService *services.IngestionService, apiKeyIssuer auth.APIKeyIssuer, apiKeyValidator auth.APIKeyValidator) *InscriptionHandler {
	requireImage := os.Getenv("STARGATE_REQUIRE_IMAGE") == "true"
	return &InscriptionHandler{
		BaseHandler:        NewBaseHandler(),
		inscriptionService: inscriptionService,
		ingestionService:   ingestionService,
		proxyBase:          os.Getenv("STARGATE_PROXY_BASE"),
		apiKeyIssuer:       apiKeyIssuer,
		apiKeyValidator:    apiKeyValidator,
		RequireImage:       requireImage,
	}
}

func placeholderPNG() []byte {
	// Try multiple paths for the real placeholder image
	paths := []string{
		"assets/quantum_lattice.png",
		"/app/assets/quantum_lattice.png",
		"./assets/quantum_lattice.png",
	}

	var data []byte
	var err error

	for _, path := range paths {
		data, err = os.ReadFile(path)
		if err == nil {
			return data
		}
	}

	// Fallback to a simple base64 PNG if file not found
	b64 := "iVBORw0KGgoAAAANSUhEUgAAAEAAAABACAIAAAAlC+aJAAAAfklEQVR4nNXOQREAIADDsFL/wiYLETy4RkHONsokTuIkTuIkTuIkTuIkTuIkTuIkTuIkTuIkTuIkTuIkTuIkTuIkTuIkTuIkTuIkTuIkTuIkTuIkTuIkTuIkTuIkTuIkTuIkTuL8HXh1AVjjAtjgr6lpAAAAAElFTkSuQmCC"
	data, _ = io.ReadAll(base64.NewDecoder(base64.StdEncoding, strings.NewReader(b64)))
	return data
}

// HandleGetInscriptions handles getting all inscriptions
// @Summary Get all pending inscriptions (smart contracts)
// @Description Get all pending inscriptions (smart contracts)
// @Tags Inscriptions
// @Produce  json
// @Success 200 {object} models.PendingTransactionsResponse
// @Router /api/inscriptions [get]
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

func isPendingContractStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "pending", "claimed", "submitted", "pending_review", "approved", "published":
		return true
	case "confirmed", "upsert", "complete", "superseded":
		return false
	case "active":
		return false
	default:
		return false
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
	// Include text (message) in hash if provided, for uniqueness of wish/inscription
	// (previously ignored the text param, now uses both for Cat 6.6)
	input := append(imageBytes, []byte(text)...)
	sum := sha256.Sum256(input)
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
	if err := os.MkdirAll(uploadsDir, 0755); err != nil {
		return "", err
	}
	target := security.SafeFilePath(uploadsDir, rec.Filename)
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
	log.Printf("DEBUG: CreateInscription handler called with method: %s, apiKeyIssuer: %v", r.Method, h.apiKeyIssuer != nil)
	if r.Method != http.MethodPost {
		h.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Only support JSON requests
	contentType := r.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "application/json") && !strings.HasPrefix(contentType, "application/json;") {
		h.sendError(w, http.StatusBadRequest, "Only JSON content type is supported")
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		h.sendError(w, http.StatusBadRequest, "Failed to read request body")
		return
	}
	defer r.Body.Close()

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
	)

	var payload struct {
		Message      string `json:"message"`
		Text         string `json:"text"`
		Method       string `json:"method"`
		Price        string `json:"price"`
		PriceUnit    string `json:"price_unit"`
		Address      string `json:"address"`
		FundingMode  string `json:"funding_mode"`
		ImageBase64  string `json:"image_base64"`
		Filename     string `json:"filename"`
		SkipProposal bool   `json:"skip_proposal"`
	}
	if err := json.Unmarshal(bodyBytes, &payload); err != nil {
		h.sendError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}
	text = payload.Message
	if text == "" {
		text = payload.Text
	}
	if text == "" {
		h.sendError(w, http.StatusBadRequest, "Message is required for inscription")
		return
	}

	method = payload.Method
	if method == "" {
		method = "alpha"
	}

	price = payload.Price
	if price == "" {
		price = "0"
	}

	priceUnit = payload.PriceUnit
	if priceUnit == "" {
		priceUnit = "btc"
	}

	address = payload.Address
	fundingMode = payload.FundingMode
	filename = payload.Filename

	if filename != "" && !security.ValidateExtension(filename, security.AllowedImageExtensions) {
		h.sendError(w, http.StatusBadRequest, "Invalid file type. Allowed types: png, jpg, jpeg, gif, webp, avif, bmp, svg")
		return
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

	// Ensure we have image bytes & filename for downstream hashing/storage
	if len(imgBytes) == 0 {
		if h.RequireImage {
			h.sendError(w, http.StatusBadRequest, "Image is required for inscription")
			return
		}
		imgBytes = placeholderPNG()
		if filename == "" {
			filename = "placeholder.png"
		}
	}
	// For inscription, only alpha is supported (detection supports all 5).
	// Return clear 400 instead of silent downgrade (Cat 6.1).
	if method != "" && method != "auto" && method != "alpha" {
		h.sendError(w, http.StatusBadRequest, "only alpha method is supported for inscription")
		return
	}
	method = resolveStegoMethod(method, filename, imgBytes)
	wishTimestamp := time.Now().Unix()
	embeddedMessage := appendWishTimestamp(text, wishTimestamp)

	// Steganography: proxy to starlight if configured, otherwise native Go
	var ingestionID string
	var stegoImgBytes []byte
	var stegoImageBase64 string
	var starlightRequestID string

	if h.proxyBase != "" {
		// Proxy to starlight /inscribe
		log.Printf("DEBUG: Proxy path selected, proxyBase=%s", h.proxyBase)
		var buf bytes.Buffer
		writer := multipart.NewWriter(&buf)

		part, _ := writer.CreateFormFile("image", filename)
		if len(imgBytes) > 0 {
			io.Copy(part, bytes.NewReader(imgBytes))
		} else {
			io.Copy(part, bytes.NewReader(placeholderPNG()))
		}

		writer.WriteField("message", embeddedMessage)
		writer.WriteField("method", method)
		writer.Close()

		proxyURL := fmt.Sprintf("%s/inscribe", strings.TrimRight(h.proxyBase, "/"))
		proxyReq, _ := http.NewRequest(http.MethodPost, proxyURL, &buf)
		proxyReq.Header.Set("Content-Type", writer.FormDataContentType())
		if apiKey := os.Getenv("STARGATE_API_KEY"); apiKey != "" {
			proxyReq.Header.Set("Authorization", "Bearer "+apiKey)
		}

		resp, err := http.DefaultClient.Do(proxyReq)
		if err != nil {
			h.sendError(w, http.StatusBadGateway, fmt.Sprintf("Proxy request to starlight failed: %v", err))
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			var errorResp struct {
				Error   string `json:"error"`
				Message string `json:"message"`
			}
			msg := "Proxy request failed"
			if json.Unmarshal(body, &errorResp) == nil {
				if errorResp.Message != "" {
					msg = errorResp.Message
				} else if errorResp.Error != "" {
					msg = errorResp.Error
				}
			}
			h.sendError(w, resp.StatusCode, msg)
			return
		}

		// Check for error fields embedded in a 200 response
		var errorCheck struct {
			Error struct {
				Code    int    `json:"code"`
				Error   string `json:"error"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(body, &errorCheck) == nil && (errorCheck.Error.Code != 0 || errorCheck.Error.Error != "" || errorCheck.Error.Message != "") {
			code := errorCheck.Error.Code
			msg := errorCheck.Error.Message
			if msg == "" {
				msg = errorCheck.Error.Error
			}
			if code == 0 {
				code = http.StatusBadRequest
			}
			if msg == "" {
				msg = "Request failed"
			}
			h.sendError(w, code, msg)
			return
		}

		var starlightResp struct {
			RequestID   string `json:"request_id"`
			ID          string `json:"id"`
			ImageSHA256 string `json:"image_sha256"`
			ImageBase64 string `json:"image_base64"`
		}
		if err := json.Unmarshal(body, &starlightResp); err != nil || starlightResp.ImageSHA256 == "" {
			h.sendError(w, http.StatusInternalServerError, "Unexpected response format from inscription service")
			return
		}

		ingestionID = starlightResp.ImageSHA256
		starlightRequestID = starlightResp.RequestID
		stegoImgBytes, err = base64.StdEncoding.DecodeString(starlightResp.ImageBase64)
		if err != nil {
			h.sendError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to decode stego image from proxy: %v", err))
			return
		}
		stegoImageBase64 = starlightResp.ImageBase64
	} else {
		// Native steganography (no proxy configured)
		log.Printf("DEBUG: Native stego path selected")
		inscribeResult, err := stego.Inscribe(imgBytes, embeddedMessage, method)
		if err != nil {
			h.sendError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to embed steganography: %v", err))
			return
		}
		ingestionID = inscribeResult.ID
		stegoImgBytes = inscribeResult.ImageBytes
		stegoImageBase64 = inscribeResult.ImageBase64
	}

	// Record the wish creator
	creatorKey := strings.TrimSpace(r.Header.Get("X-API-Key"))
	if creatorKey == "" {
		auth := r.Header.Get("Authorization")
		if strings.HasPrefix(auth, "Bearer ") {
			creatorKey = strings.TrimPrefix(auth, "Bearer ")
		}
	}
	var creatorWallet string
	if creatorKey != "" && h.apiKeyValidator != nil {
		if apiKeyRec, ok := h.apiKeyValidator.Get(creatorKey); ok {
			creatorWallet = strings.TrimSpace(apiKeyRec.Wallet)
			log.Printf("DEBUG: Found creator wallet: %s", creatorWallet)
		}
	}

	meta := map[string]interface{}{
		"embedded_message": embeddedMessage,
		"message":          text,
		"wish_timestamp":   wishTimestamp,
		"price":            price,
		"price_unit":       priceUnit,
		"address":          address,
		"funding_mode":     fundingMode,
		"creator_wallet":   creatorWallet,
	}
	if starlightRequestID != "" {
		meta["starlight_request_id"] = starlightRequestID
	} else {
		meta["native_stego"] = true
	}
	if strings.EqualFold(priceUnit, "sats") {
		meta["budget_sats"] = parsePriceSats(price)
	}

	// Write stego image to uploads directory
	uploadsDir := os.Getenv("UPLOADS_DIR")
	log.Printf("DEBUG: uploadsDir resolved to: %s", uploadsDir)
	if err := os.MkdirAll(uploadsDir, 0755); err != nil {
		h.sendError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to create uploads directory %s: %v", uploadsDir, err))
		return
	}
	// Use hash-only filename for stealth
	imageFilename := ingestionID
	imagePath := security.SafeFilePath(uploadsDir, imageFilename)
	log.Printf("DEBUG: Attempting to write stego image to %s (size: %d bytes)", imagePath, len(stegoImgBytes))
	if err := os.WriteFile(imagePath, stegoImgBytes, 0644); err != nil {
		h.sendError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to write image to %s: %v", imagePath, err))
		return
	}
	log.Printf("DEBUG: Successfully stored stego image to %s", imagePath)

	if h.ingestionService != nil {
		log.Printf("DEBUG: Creating ingestion record for %s", ingestionID)
		ingRec := services.IngestionRecord{
			ID:            ingestionID,
			Filename:      imageFilename,
			Method:        "alpha",
			MessageLength: len(embeddedMessage),
			ImageBase64:   stegoImageBase64,
			Metadata:      meta,
			Status:        "pending",
		}
		if err := h.ingestionService.Create(ingRec); err != nil {
			log.Printf("ERROR: Failed to create ingestion record for %s: %v", ingestionID, err)
		}
		// Publish announcement
		log.Printf("DEBUG: Publishing pending ingest announcement for %s", ingestionID)
		publishPendingIngestAnnouncement(ingestionID, ingestionID, imageFilename, "alpha", embeddedMessage, price, priceUnit, address, fundingMode, stegoImgBytes)
	}

	if h.store != nil {
		log.Printf("DEBUG: Mirroring into store for %s", ingestionID)
		proposalTitle := strings.TrimSpace(text)
		if strings.HasPrefix(proposalTitle, "#") {
			proposalTitle = strings.TrimSpace(strings.TrimLeft(proposalTitle, "#"))
		}
		if proposalTitle == "" {
			proposalTitle = "Wish " + ingestionID
		}

		if !payload.SkipProposal {
			proposal := sc.Proposal{
				ID:               ingestionID,
				Title:            proposalTitle,
				DescriptionMD:    text,
				VisiblePixelHash: ingestionID,
				BudgetSats:       parsePriceSats(price),
				Status:           "pending",
				CreatedAt:        time.Now(),
				Metadata: map[string]any{
					"funding_mode":   fundingMode,
					"address":        address,
					"price_unit":     priceUnit,
					"creator_wallet": creatorWallet,
				},
			}

			if err := h.store.CreateProposal(context.Background(), proposal); err != nil {
				fmt.Printf("Failed to create proposal for wish %s: %v\n", ingestionID, err)
			}
		}

		wishContract := sc.Contract{
			ContractID:      "wish-" + ingestionID,
			Title:           proposalTitle,
			TotalBudgetSats: parsePriceSats(price),
			GoalsCount:      0,
			Status:          "pending",
		}

		type upserter interface {
			UpsertContractWithTasks(ctx context.Context, contract sc.Contract, tasks []sc.Task) error
		}
		if u, ok := h.store.(upserter); ok {
			if err := u.UpsertContractWithTasks(context.Background(), wishContract, nil); err != nil {
				fmt.Printf("Failed to create wish contract %s: %v\n", wishContract.ContractID, err)
			}
		}
	}

	h.sendSuccess(w, map[string]string{
		"status":             "success",
		"id":                 ingestionID,
		"ingestion_id":       ingestionID,
		"visible_pixel_hash": ingestionID,
	})
	return
}

// HandleDeleteInscription handles deleting an inscription and its associated wish
func (h *InscriptionHandler) HandleDeleteInscription(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		h.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Extract ID from URL path (e.g., /api/inscriptions/{id})
	id := strings.TrimPrefix(r.URL.Path, "/api/inscriptions/")
	if id == "" {
		h.sendError(w, http.StatusBadRequest, "Missing ID")
		return
	}

	// Normalize ID (strip wish- prefix)
	visibleHash := strings.TrimPrefix(id, "wish-")

	// Get requester wallet from API key (provided by wrapWithAuth)
	var requesterWallet string
	apiKey := strings.TrimSpace(r.Header.Get("X-API-Key"))
	if apiKey == "" {
		auth := r.Header.Get("Authorization")
		if strings.HasPrefix(auth, "Bearer ") {
			apiKey = strings.TrimPrefix(auth, "Bearer ")
		}
	}
	if apiKey != "" && h.apiKeyValidator != nil {
		if apiKeyRec, ok := h.apiKeyValidator.Get(apiKey); ok {
			requesterWallet = strings.TrimSpace(apiKeyRec.Wallet)
		}
	}

	// 1. Check ownership via ingestion record
	if h.ingestionService != nil {
		rec, err := h.ingestionService.Get(visibleHash)
		if err != nil {
			h.sendError(w, http.StatusNotFound, "Inscription not found")
			return
		}

		// Verify ownership
		if creatorWallet, ok := rec.Metadata["creator_wallet"].(string); ok && creatorWallet != "" {
			if requesterWallet == "" || !strings.EqualFold(strings.TrimSpace(creatorWallet), requesterWallet) {
				// Special case: check global auditor status (donation address)
				donationAddr := strings.TrimSpace(os.Getenv("STARLIGHT_DONATION_ADDRESS"))
				if donationAddr == "" || !strings.EqualFold(requesterWallet, donationAddr) {
					h.sendError(w, http.StatusForbidden, "Only the wish creator or an authorized auditor can delete this wish")
					return
				}
			}
		}

		// Check status - deletion not allowed for confirmed/finalized items
		if !isPendingContractStatus(rec.Status) {
			h.sendError(w, http.StatusForbidden, fmt.Sprintf("Cannot delete a wish with status '%s' (only pending wishes can be deleted)", rec.Status))
			return
		}

		// Delete from ingestion service
		if err := h.ingestionService.Delete(r.Context(), visibleHash); err != nil {
			log.Printf("Failed to delete ingestion record %s: %v", visibleHash, err)
		}
	}

	// 2. Delete from MCP store (cascading delete)
	if h.store != nil {
		// Double check status in contract store
		wishID := wishContractID(visibleHash)
		if contract, err := h.store.GetContract(wishID); err == nil {
			if !isPendingContractStatus(contract.Status) {
				h.sendError(w, http.StatusForbidden, fmt.Sprintf("Cannot delete a contract with status '%s'", contract.Status))
				return
			}
		}

		if err := h.store.DeleteWish(r.Context(), visibleHash); err != nil {
			log.Printf("Failed to delete wish %s from store: %v", visibleHash, err)
		}
	}

	h.sendSuccess(w, map[string]string{
		"status":  "success",
		"message": "Inscription and associated wish deleted",
		"id":      id,
	})
}

// contractToInscriptionRequest converts a smart_contract.Contract to models.InscriptionRequest
func contractToInscriptionRequest(contract sc.Contract) models.InscriptionRequest {
	// Use persisted stego image URL or compute fallback
	imageURL := contract.StegoImageURL

	// Normalize relative paths from block monitor (e.g. "images/filename.png")
	if imageURL != "" && !strings.HasPrefix(imageURL, "/") && !strings.HasPrefix(imageURL, "http") {
		if contract.ConfirmedBlockHeight != nil {
			filename := filepath.Base(imageURL)
			imageURL = fmt.Sprintf("/api/block-image/%d/%s", *contract.ConfirmedBlockHeight, filename)
		}
	}

	if imageURL == "" {
		imageURL = computeStegoImageURL(contract.ContractID)
	}

	// Convert timestamp to Unix if available
	var timestamp int64
	if contract.ConfirmedAt != nil {
		timestamp = contract.ConfirmedAt.Unix()
	} else if !contract.CreatedAt.IsZero() {
		timestamp = contract.CreatedAt.Unix()
	} else if contract.GoalsCount > 0 { // Contract has some data
		timestamp = time.Now().Unix() // Use current time as fallback
	}

	height := int64(0)
	if contract.ConfirmedBlockHeight != nil {
		height = int64(*contract.ConfirmedBlockHeight)
	}

	// Determine TXID: use confirmed_txid for confirmed contracts, otherwise TBD
	txID := "TBD"
	if contract.Status == "confirmed" && contract.Metadata != nil {
		if v, ok := contract.Metadata["confirmed_txid"].(string); ok && v != "" {
			txID = v
		} else if v, ok := contract.Metadata["funding_txid"].(string); ok && v != "" {
			txID = v
		}
	}

	return models.InscriptionRequest{
		ID:              contract.ContractID,
		TXID:            txID,
		Text:            contract.Title,
		ImageData:       imageURL,
		Price:           float64(contract.TotalBudgetSats) / 1e8,
		Address:         "", // No address in contract model
		Timestamp:       timestamp,
		Status:          contract.Status,
		BlockHeight:     height,
		TotalBudgetSats: contract.TotalBudgetSats,
		AvailableTasks:  contract.AvailableTasksCount,
	}
}

// SetStore injects the MCP store so inscriptions can be mirrored into open contracts.
func (h *InscriptionHandler) SetStore(store scmiddleware.Store) {
	h.store = store
}

func (h *InscriptionHandler) fromIngestion(rec services.IngestionRecord) models.InscriptionRequest {
	uploadsDir := os.Getenv("UPLOADS_DIR")
	_ = os.MkdirAll(uploadsDir, 0755)

	// Use just the hash for consistency with HandleCreateInscription
	filename := rec.ID
	targetPath := security.SafeFilePath(uploadsDir, filename)

	if _, err := os.Stat(targetPath); os.IsNotExist(err) {
		if rec.ImageBase64 != "" {
			if data, err := base64.StdEncoding.DecodeString(rec.ImageBase64); err == nil {
				if err := os.WriteFile(targetPath, data, 0644); err != nil {
					fmt.Printf("Failed to write ingestion image to %s: %v\n", targetPath, err)
				} else {
					log.Printf("DEBUG: lazy-stored ingestion image to %s", targetPath)
				}
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
	imagePath := ""
	timestamp := int64(0)

	// Use the stego_image_url from the contract if available, falls back to old pattern
	if c.StegoImageURL != "" {
		imagePath = c.StegoImageURL
	} else if c.ContractID != "" {
		// Extract hash from contract ID and try hash-only filename first
		baseID := baseContractID(c.ContractID)
		if baseID != "" {
			hashPath := filepath.Join(uploadsDir, baseID)
			if _, err := os.Stat(hashPath); err == nil {
				imagePath = hashPath
			} else {
				// Fallback to old pattern with prefix
				if matches, _ := filepath.Glob(filepath.Join(uploadsDir, baseID+"_*")); len(matches) > 0 {
					imagePath = matches[0]
				}
			}
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
		// First try hash-only filename (new stealth naming)
		hashPath := filepath.Join(uploadsDir, baseID)
		if _, err := os.Stat(hashPath); err == nil {
			imagePath = hashPath
		} else {
			// Fallback to old pattern with prefix
			if matches, _ := filepath.Glob(filepath.Join(uploadsDir, baseID+"_*")); len(matches) > 0 {
				imagePath = matches[0]
			}
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

func publishPendingIngestAnnouncement(ingestionID, visibleHash, filename, method, message, price, priceUnit, address, fundingMode string, imgBytes []byte) {
	if !ipfsIngestSyncEnabled() {
		return
	}
	if !ipfs.IsEnabled() {
		log.Printf("pending ingest announce: IPFS disabled, skipping for %s", ingestionID)
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
	// Match loadIPFSIngestSyncConfig: enabled by default unless explicitly false.
	return !strings.EqualFold(strings.TrimSpace(os.Getenv("IPFS_INGEST_SYNC_ENABLED")), "false")
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
