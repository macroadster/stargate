package handlers

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "image/gif"
	_ "image/jpeg"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"

	sc "stargate-backend/core/smart_contract"
	scmiddleware "stargate-backend/app/smart_contract"
	"stargate-backend/models"
	"stargate-backend/services"
	storageSC "stargate-backend/storage/smart_contract"
)

// enrichedCacheTTL is a safety net so list pages cannot stay stale forever if a
// mutation path forgets to call InvalidateContractCache. Primary freshness comes
// from invalidation on ConfirmContract / status / upsert via NotifyingStore.
const enrichedCacheTTL = 30 * time.Second

// enrichedCacheEntry holds a cached open-contracts response page.
type enrichedCacheEntry struct {
	items    []models.InscriptionRequest
	cachedAt time.Time
}

// SmartContractHandler handles smart contract requests
type SmartContractHandler struct {
	*BaseHandler
	contractService *services.SmartContractService
	store           scmiddleware.Store
	ingestion       *services.IngestionService
	contractCache   *storageSC.ContractCache

	// enrichedCache stores the final processed list (after ListByIDs + conversion)
	// for fast repeated requests to the same filter (e.g. first page of open contracts).
	enrichedMu    sync.RWMutex
	enrichedCache map[string]enrichedCacheEntry
	enrichedTTL   time.Duration
}

func includeConfirmedQuery(r *http.Request) bool {
	raw := strings.TrimSpace(r.URL.Query().Get("include_confirmed"))
	if raw == "" {
		return false
	}
	return strings.EqualFold(raw, "true") || strings.EqualFold(raw, "yes") || raw == "1"
}

func hasCursorParams(r *http.Request) bool {
	q := r.URL.Query()
	return q.Get("cursor_height") != "" || q.Get("cursor_date") != "" || q.Get("cursor") != ""
}

// openStatuses returns the statuses considered "open" (non-terminal) for server-side filtering.
func openStatuses() []string {
	return []string{"pending", "created", "funded", "active"}
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
	if open := params.Get("open"); open != "" {
		key += ":open:" + open
	}

	return key
}

// NewSmartContractHandler creates a new smart contract handler
func NewSmartContractHandler(store scmiddleware.Store, ingestion *services.IngestionService, contractCache *storageSC.ContractCache) *SmartContractHandler {
	h := &SmartContractHandler{
		BaseHandler:     NewBaseHandler(),
		contractService: nil, // Not used - we query MCP store directly
		store:           store,
		ingestion:       ingestion,
		contractCache:   contractCache,
		enrichedCache:   make(map[string]enrichedCacheEntry),
		enrichedTTL:     enrichedCacheTTL,
	}
	return h
}

// InvalidateContractCache clears ALL contract list cache entries.
// Must be called after ConfirmContract / status changes / upserts so /contracts
// and open-contracts views do not serve pre-confirmation snapshots.
func (h *SmartContractHandler) InvalidateContractCache() {
	if h.contractCache != nil {
		h.contractCache.InvalidateAll()
		log.Printf("Contract raw cache invalidated (all keys)")
	}
	h.enrichedMu.Lock()
	h.enrichedCache = make(map[string]enrichedCacheEntry)
	h.enrichedMu.Unlock()
	log.Printf("Enriched contract cache cleared")
}

// getEnrichedCache returns a copy of a non-expired enriched page, if present.
func (h *SmartContractHandler) getEnrichedCache(key string) ([]models.InscriptionRequest, bool) {
	h.enrichedMu.RLock()
	entry, ok := h.enrichedCache[key]
	ttl := h.enrichedTTL
	h.enrichedMu.RUnlock()
	if !ok || len(entry.items) == 0 {
		return nil, false
	}
	if ttl > 0 && time.Since(entry.cachedAt) > ttl {
		h.enrichedMu.Lock()
		// Re-check under write lock before delete (may have been refreshed).
		if e, still := h.enrichedCache[key]; still && time.Since(e.cachedAt) > ttl {
			delete(h.enrichedCache, key)
		}
		h.enrichedMu.Unlock()
		return nil, false
	}
	out := make([]models.InscriptionRequest, len(entry.items))
	copy(out, entry.items)
	return out, true
}

// setEnrichedCache stores a copy of the enriched page under key.
func (h *SmartContractHandler) setEnrichedCache(key string, items []models.InscriptionRequest) {
	copied := make([]models.InscriptionRequest, len(items))
	copy(copied, items)
	h.enrichedMu.Lock()
	h.enrichedCache[key] = enrichedCacheEntry{items: copied, cachedAt: time.Now()}
	h.enrichedMu.Unlock()
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

	status := r.URL.Query().Get("status")
	if status != "" {
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

	// Open contracts are unconfirmed by definition (pending/created/funded/active).
	// They must not be ordered by confirmed_at/block height — sort created_at DESC
	// so the newest open wish is at the top and older ones load as the user scrolls.
	// Callers: OpenContractsView (?open=true or ?status=open).
	openParam := r.URL.Query().Get("open")
	openMode := openParam == "true" || openParam == "1" || status == "open"
	if openMode {
		filter.Statuses = openStatuses()
		filter.Status = "" // prefer explicit Statuses
		filter.OrderByConfirmedAt = false
		filter.OrderByCreatedAt = true
	}

	cacheKey := generateCacheKey(r)

	// === Instrumentation start ===
	t0 := time.Now()

	// Fast path: enriched cache (final processed list) for non-cursor queries
	var inscriptions []models.InscriptionRequest
	cachedEnriched := false
	if !hasCursorParams(r) {
		if cached, ok := h.getEnrichedCache(cacheKey); ok {
			inscriptions = cached
			cachedEnriched = true
		}
	}
	var contracts []sc.Contract
	var listDur, byIDsDur, convertDur time.Duration

	if cachedEnriched {
		// Skip list + enrichment; still derive cursors from inscriptions below.
	} else {
		// Check raw contracts cache (for first page)
		t1 := time.Now()
		if h.contractCache != nil {
			if cached, ok := h.contractCache.Get(cacheKey); ok && len(cached) > 0 {
				if !hasCursorParams(r) {
					contracts = cached
				}
			}
		}
		_ = time.Since(t1) // cache check time captured in total for now

		if len(contracts) == 0 {
			t2 := time.Now()
			var err error
			contracts, err = h.store.ListContracts(filter)
			if err != nil {
				log.Printf("Failed to get contracts: %v", err)
				h.sendError(w, http.StatusInternalServerError, "Failed to get contracts")
				return
			}
			listDur = time.Since(t2)

			// Populate raw cache
			if h.contractCache != nil && !hasCursorParams(r) {
				h.contractCache.Set(cacheKey, contracts)
			}
		} else {
			listDur = time.Since(t1)
		}

		// === Enrichment (ListByIDs + conversion) ===
		var inscriptionsList []models.InscriptionRequest
		ingestionMap := make(map[string]services.IngestionRecord)

		// Pre-fetch ingestion records in batch if service is available
		if h.ingestion != nil && len(contracts) > 0 {
			var ingestionIDs []string
			for _, c := range contracts {
				id := strings.TrimPrefix(c.ContractID, "wish-")
				ingestionIDs = append(ingestionIDs, id)
			}
			tBy := time.Now()
			if recs, err := h.ingestion.ListByIDs(ingestionIDs); err == nil {
				for _, rec := range recs {
					ingestionMap[rec.ID] = rec
				}
			}
			byIDsDur = time.Since(tBy)
		}

		tConv := time.Now()
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
				// Only fill timestamp from ingestion when the contract had none
				// (avoid overwriting confirmed_at with wish created_at).
				if inscription.Timestamp == 0 {
					inscription.Timestamp = rec.CreatedAt.Unix()
				}
			}

			inscriptionsList = append(inscriptionsList, inscription)
		}
		convertDur = time.Since(tConv)
		inscriptions = inscriptionsList

		// Cache the *enriched* final list for this key (big win for repeated calls)
		if !hasCursorParams(r) {
			h.setEnrichedCache(cacheKey, inscriptions)
		}
	}

	// Determine next cursors for pagination.
	// Prefer raw contracts when available; fall back to inscription fields so
	// cached first pages still return next_cursor_date for infinite scroll.
	// Open (unconfirmed): cursor on created_at. Confirmed lists: cursor on confirmed_at.
	nextCursor := ""
	nextCursorDate := ""
	fullPage := len(inscriptions) >= limit && limit > 0
	hasMore := fullPage
	if fullPage {
		if len(contracts) > 0 {
			lastContract := contracts[len(contracts)-1]
			if lastContract.ConfirmedBlockHeight != nil && *lastContract.ConfirmedBlockHeight > 0 {
				nextCursor = fmt.Sprintf("%d", *lastContract.ConfirmedBlockHeight)
			}
			if filter.OrderByCreatedAt {
				if !lastContract.CreatedAt.IsZero() {
					nextCursorDate = lastContract.CreatedAt.UTC().Format(time.RFC3339)
				}
			} else if lastContract.ConfirmedAt != nil {
				nextCursorDate = lastContract.ConfirmedAt.UTC().Format(time.RFC3339)
			}
		}
		if nextCursor == "" || nextCursorDate == "" {
			last := inscriptions[len(inscriptions)-1]
			if nextCursor == "" && last.BlockHeight > 0 {
				nextCursor = fmt.Sprintf("%d", last.BlockHeight)
			}
			if nextCursorDate == "" && last.Timestamp > 0 {
				nextCursorDate = time.Unix(last.Timestamp, 0).UTC().Format(time.RFC3339)
			}
		}
	}

	totalDur := time.Since(t0)

	// Instrumentation log (only log slow ones or always for visibility)
	if totalDur > 100*time.Millisecond || cachedEnriched {
		log.Printf("[open-contracts] total=%v cached_enriched=%v list=%v byids=%v convert=%v contracts=%d inscriptions=%d key=%s",
			totalDur, cachedEnriched, listDur, byIDsDur, convertDur,
			len(contracts), len(inscriptions), cacheKey)
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
