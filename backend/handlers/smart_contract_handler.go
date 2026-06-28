package handlers

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	_ "image/gif"
	_ "image/jpeg"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"

	sc "stargate-backend/core/smart_contract"
	scmiddleware "stargate-backend/middleware/smart_contract"
	"stargate-backend/models"
	"stargate-backend/services"
	storageSC "stargate-backend/storage/smart_contract"
)

// SmartContractHandler handles smart contract requests
type SmartContractHandler struct {
	*BaseHandler
	contractService *services.SmartContractService
	store           scmiddleware.Store
	ingestion       *services.IngestionService
	contractCache   *storageSC.ContractCache
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

// computeStegoImageURL generates the stego image URL for a contract
func computeStegoImageURL(contractID string) string {
	// Strip "wish-" prefix to match actual filename
	hash := contractID
	if strings.HasPrefix(contractID, "wish-") {
		hash = strings.TrimPrefix(contractID, "wish-")
	}
	return fmt.Sprintf("/uploads/%s", hash)
}

// generateCacheKey creates a cache key for contract queries
func generateCacheKey(r *http.Request) string {
	params := r.URL.Query()
	key := "contracts"

	if status := params.Get("status"); status != "" {
		key += ":status:" + status
	}
	if limit := params.Get("limit"); limit != "" {
		key += ":limit:" + limit
	}
	if includeConfirmed := params.Get("include_confirmed"); includeConfirmed != "" {
		key += ":confirmed:" + includeConfirmed
	}

	return key
}

// NewSmartContractHandler creates a new smart contract handler
func NewSmartContractHandler(store scmiddleware.Store, ingestion *services.IngestionService, contractCache *storageSC.ContractCache) *SmartContractHandler {
	return &SmartContractHandler{
		BaseHandler:     NewBaseHandler(),
		contractService: nil, // Not used - we query MCP store directly
		store:           store,
		ingestion:       ingestion,
		contractCache:   contractCache,
	}
}

// InvalidateContractCache clears ALL contract cache entries aggressively
func (h *SmartContractHandler) InvalidateContractCache() {
	if h.contractCache != nil {
		// Clear ALL contracts cache entries to prevent stale data
		h.contractCache.Invalidate("contracts")
		h.contractCache.Invalidate("contracts:status:open")
		h.contractCache.Invalidate("contracts:status:active")
		h.contractCache.Invalidate("contracts:status:pending")
		h.contractCache.Invalidate("contracts:status:")
		h.contractCache.Invalidate("contracts:limit:")
		h.contractCache.Invalidate("contracts:confirmed:")
		log.Printf("Contract cache aggressively invalidated")
	}
}

// HandleGetContracts handles getting smart contracts with support for filtering and pagination
func (h *SmartContractHandler) HandleGetContracts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Parse pagination parameters
	limit := 100 // larger default for sidebar/full views
	if lim := r.URL.Query().Get("limit"); lim != "" {
		if parsed, err := strconv.Atoi(lim); err == nil && parsed > 0 && parsed <= 500 {
			limit = parsed
		}
	}

	// Build filter
	filter := sc.ContractFilter{
		Limit:              limit,
		OrderByConfirmedAt: true,
	}

	if status := r.URL.Query().Get("status"); status != "" {
		filter.Status = status
	}

	if skills := r.URL.Query().Get("skills"); skills != "" {
		filter.Skills = strings.Split(skills, ",")
	}

	// Parse cursor_height for pagination
	if cursor := r.URL.Query().Get("cursor_height"); cursor != "" && cursor != "*" {
		if parsed, err := strconv.Atoi(cursor); err == nil && parsed > 0 {
			filter.CursorHeight = &parsed
		}
	}

	// Parse cursor_date for pagination
	if cursorDate := r.URL.Query().Get("cursor_date"); cursorDate != "" {
		if parsed, err := time.Parse(time.RFC3339, cursorDate); err == nil {
			filter.CursorDate = &parsed
		}
	}

	// Parse cursor_type
	if cursorType := r.URL.Query().Get("cursor_type"); cursorType != "" {
		filter.CursorType = cursorType
	}

	// Query database
	contracts, err := h.store.ListContracts(filter)
	if err != nil {
		log.Printf("Failed to get contracts: %v", err)
		h.sendError(w, http.StatusInternalServerError, "Failed to get contracts")
		return
	}

	// Convert results to inscriptions for frontend compatibility
	var inscriptions []models.InscriptionRequest
	ingestionMap := make(map[string]services.IngestionRecord)

	// Pre-fetch ingestion records in batch if service is available
	if h.ingestion != nil && len(contracts) > 0 {
		var ingestionIDs []string
		for _, c := range contracts {
			id := strings.TrimPrefix(c.ContractID, "wish-")
			ingestionIDs = append(ingestionIDs, id)
		}
		if recs, err := h.ingestion.ListByIDs(ingestionIDs); err == nil {
			for _, rec := range recs {
				ingestionMap[rec.ID] = rec
			}
		}
	}

	for _, contract := range contracts {
		inscription := contractToInscriptionRequest(contract)

		// Enrich with ingestion data from pre-fetched map
		ingestionID := strings.TrimPrefix(contract.ContractID, "wish-")
		if rec, ok := ingestionMap[ingestionID]; ok {
			if wishText, ok := rec.Metadata["message"].(string); ok && wishText != "" {
				inscription.Text = wishText
			} else if wishText, ok := rec.Metadata["embedded_message"].(string); ok && wishText != "" {
				inscription.Text = wishText
			}
			if vph, ok := rec.Metadata["visible_pixel_hash"].(string); ok && vph != "" {
				inscription.VisiblePixelHash = vph
			}
			if inscription.Timestamp == 0 || inscription.Timestamp == time.Now().Unix() {
				inscription.Timestamp = rec.CreatedAt.Unix()
			}
		}

		inscriptions = append(inscriptions, inscription)
	}

	// Determine next cursors for pagination
	nextCursor := ""
	nextCursorDate := ""
	hasMore := false
	if len(contracts) > 0 {
		lastContract := contracts[len(contracts)-1]
		if lastContract.ConfirmedBlockHeight != nil && *lastContract.ConfirmedBlockHeight > 0 {
			nextCursor = fmt.Sprintf("%d", *lastContract.ConfirmedBlockHeight)
			hasMore = true
		}
		if lastContract.ConfirmedAt != nil {
			nextCursorDate = lastContract.ConfirmedAt.Format(time.RFC3339)
			hasMore = true
		}
	}

	// Build response matching frontend expectations
	response := map[string]interface{}{
		"contracts":        inscriptions,
		"transactions":     inscriptions, // for backward compatibility
		"total":            len(inscriptions),
		"limit":            limit,
		"next_cursor":      nextCursor,
		"next_cursor_date": nextCursorDate,
		"has_more":         hasMore,
	}

	w.Header().Set("Content-Type", "application/json")
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
