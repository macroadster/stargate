package handlers

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	_ "image/gif"
	_ "image/jpeg"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"

	sc "stargate-backend/core/smart_contract"
	scmiddleware "stargate-backend/app/smart_contract"
	"stargate-backend/models"
	"stargate-backend/services"
	"stargate-backend/storage"
)

// SearchHandler handles search requests
type SearchHandler struct {
	*BaseHandler
	inscriptionService *services.InscriptionService
	blockService       *services.BlockService
	dataStorage        storage.ExtendedDataStorage
	store              scmiddleware.Store
}

// NewSearchHandler creates a new search handler
func NewSearchHandler(inscriptionService *services.InscriptionService, blockService *services.BlockService, dataStorage storage.ExtendedDataStorage, store scmiddleware.Store) *SearchHandler {
	return &SearchHandler{
		BaseHandler:        NewBaseHandler(),
		inscriptionService: inscriptionService,
		blockService:       blockService,
		dataStorage:        dataStorage,
		store:              store,
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

// SetStore sets the smart contract store for searching proposals and contracts
func (h *SearchHandler) SetStore(store scmiddleware.Store) {
	h.store = store
}

func (h *SearchHandler) recentBlocksResponse(query string) models.SearchResult {
	result := h.searchData(query)
	if len(result.Blocks) > 5 {
		result.Blocks = result.Blocks[:5]
	}
	if len(result.Inscriptions) > 5 {
		result.Inscriptions = result.Inscriptions[:5]
	}
	if len(result.Transactions) > 5 {
		result.Transactions = result.Transactions[:5]
	}
	if len(result.Contracts) > 5 {
		result.Contracts = result.Contracts[:5]
	}
	if len(result.Proposals) > 5 {
		result.Proposals = result.Proposals[:5]
	}
	return result
}

func (h *SearchHandler) searchData(query string) models.SearchResult {
	q := strings.ToLower(strings.TrimSpace(query))
	var blocks []models.SearchResultItem
	var inscriptions []models.SearchResultItem
	var transactions []models.SearchResultItem
	var contracts []models.SearchResultItem
	var proposals []models.SearchResultItem
	seenBlocks := make(map[string]bool)
	seenInscriptions := make(map[string]bool)
	seenTransactions := make(map[string]bool)
	seenContracts := make(map[string]bool)
	seenProposals := make(map[string]bool)

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

	addBlock := func(id string, height int64, timestamp int64, txCount int) {
		if id == "" || seenBlocks[id] || seenBlocks[fmt.Sprintf("%d", height)] {
			return
		}
		seenBlocks[id] = true
		seenBlocks[fmt.Sprintf("%d", height)] = true
		blocks = append(blocks, models.SearchResultItem{
			Type:        "block",
			ID:          id,
			BlockHeight: height,
			Timestamp:   timestamp,
			TxCount:     txCount,
		})
	}

	addInscription := func(id, text string, ts int64, blockHeight int64) {
		if id == "" || seenInscriptions[id] {
			return
		}
		seenInscriptions[id] = true
		inscriptions = append(inscriptions, models.SearchResultItem{
			Type:        "inscription",
			ID:          id,
			Text:        text,
			Timestamp:   ts,
			BlockHeight: blockHeight,
			Status:      "confirmed",
		})
	}

	addTransaction := func(id, text string, ts int64, blockHeight int64) {
		if id == "" || seenTransactions[id] {
			return
		}
		seenTransactions[id] = true
		transactions = append(transactions, models.SearchResultItem{
			Type:        "transaction",
			ID:          id,
			Text:        text,
			Timestamp:   ts,
			BlockHeight: blockHeight,
			Status:      "confirmed",
		})
	}

	addContract := func(id string, height int64, imageURL string, contractType string, visibleHash string, meta map[string]any, title string, budgetSats int64, status string, confirmedBlockHeight *int) {
		if id == "" || seenContracts[id] {
			return
		}
		seenContracts[id] = true

		// Determine TXID based on status: use confirmed_txid/funding_txid for confirmed contracts, TBD otherwise
		txID := "TBD"
		if status == "confirmed" {
			if txID = metaString(meta, "confirmed_txid"); txID == "" {
				txID = metaString(meta, "funding_txid")
			}
		}

		contracts = append(contracts, models.SearchResultItem{
			Type:                 "contract",
			ID:                   id,
			TXID:                 txID,
			ContractID:           id,
			BlockHeight:          height,
			ConfirmedBlockHeight: confirmedBlockHeight,
			Title:                title,
			VisiblePixelHash:     visibleHash,
			BudgetSats:           budgetSats,
			Metadata:             meta,
			Status:               status,
			StegoImageURL:        imageURL,
		})
	}

	addProposal := func(id, title string, status string, budgetSats int64, createdAt time.Time, visibleHash string) {
		if id == "" || seenProposals[id] {
			return
		}
		seenProposals[id] = true
		proposals = append(proposals, models.SearchResultItem{
			Type:             "proposal",
			ID:               id,
			ProposalID:       id,
			Title:            title,
			Status:           status,
			BudgetSats:       budgetSats,
			Timestamp:        createdAt.Unix(),
			VisiblePixelHash: visibleHash,
		})
	}

	if h.dataStorage != nil {
		if recent, err := h.dataStorage.GetRecentBlocks(200); err == nil {
			for _, b := range recent {
				if cache, ok := b.(*storage.BlockDataCache); ok {
					if matchesQuery(cache.BlockHash, fmt.Sprintf("%d", cache.BlockHeight)) {
						addBlock(cache.BlockHash, cache.BlockHeight, cache.Timestamp, cache.TxCount)
					}
					for _, ins := range cache.Inscriptions {
						if matchesQuery(ins.TxID, ins.FileName, ins.FilePath, ins.Content, ins.ContentType) {
							addInscription(ins.TxID, ins.Content, cache.Timestamp, cache.BlockHeight)
						}
					}
					for _, img := range cache.Images {
						if matchesQuery(img.TxID, img.FileName, img.FilePath, img.ContentType) {
							addTransaction(img.TxID, "", cache.Timestamp, cache.BlockHeight)
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
							addTransaction(id, text, cache.Timestamp, cache.BlockHeight)
							// Extract confirmed_block_height from metadata if available
							var confirmedHeight *int
							if h, ok := meta["confirmed_block_height"].(float64); ok {
								hInt := int(h)
								confirmedHeight = &hInt
							}
							addContract(id, cache.BlockHeight, imageURL, "Smart Contract", visibleHash, meta, "", 0, metaString(meta, "confirmation_status"), confirmedHeight)
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
				if m, ok := b.(map[string]interface{}); ok {
					height, _ := m["height"].(int64)
					if height == 0 {
						if f, ok := m["height"].(float64); ok {
							height = int64(f)
						}
					}
					addBlock(fmt.Sprintf("%v", m["id"]), height, 0, 0)
				}
			}
		}
	}
	if len(inscriptions) == 0 && len(transactions) == 0 {
		if svcInscriptions, err := h.inscriptionService.SearchInscriptions(query); err == nil {
			for _, ins := range svcInscriptions {
				addInscription(ins.ID, ins.Text, ins.Timestamp, 0)
			}
		}
	}

	// Search contracts from Store
	if h.store != nil {
		contractList, err := h.store.ListContracts(sc.ContractFilter{})
		if err == nil {
			for _, c := range contractList {
				if matchesQuery(c.ContractID, c.Title, strings.Join(c.Skills, " ")) {
					blockHeight := int64(0)
					if c.ConfirmedBlockHeight != nil {
						blockHeight = int64(*c.ConfirmedBlockHeight)
					}
					// Extract visible_pixel_hash from contract_id (format: wish-{hash})
					visibleHash := ""
					if strings.HasPrefix(c.ContractID, "wish-") {
						visibleHash = strings.TrimPrefix(c.ContractID, "wish-")
					}
					addContract(c.ContractID, blockHeight, c.StegoImageURL, "Smart Contract", visibleHash, c.Metadata, c.Title, c.TotalBudgetSats, c.Status, c.ConfirmedBlockHeight)
				}
			}
		}
	}

	proposalList, err := h.store.ListProposals(context.Background(), sc.ProposalFilter{})
	if err == nil {
		for _, p := range proposalList {
			if matchesQuery(p.ID, p.Title, p.DescriptionMD, p.VisiblePixelHash, p.Status) {
				addProposal(p.ID, p.Title, p.Status, p.BudgetSats, p.CreatedAt, p.VisiblePixelHash)
			}
		}
	}

	return models.SearchResult{
		Inscriptions: inscriptions,
		Transactions: transactions,
		Blocks:       blocks,
		Contracts:    contracts,
		Proposals:    proposals,
	}
}
