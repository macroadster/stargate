package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	smartcontract "stargate-backend/core/smart_contract"
	"stargate-backend/middleware/smart_contract"
	"stargate-backend/middleware/smart_contract/middleware"
)

// Store interface for proposal handler
type Store = smartcontract.Store

// ProposalHandler handles proposal-related HTTP endpoints
type ProposalHandler struct {
	store        Store
	ingestionSvc interface{} // services.IngestionService - using interface to avoid import cycle
}

// NewProposalHandler creates a new proposal handler
func NewProposalHandler(store Store, ingestionSvc interface{}) *ProposalHandler {
	return &ProposalHandler{
		store:        store,
		ingestionSvc: ingestionSvc,
	}
}

// Proposals handles GET/POST /proposals
func (h *ProposalHandler) Proposals(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.handleGetProposals(w, r)
	case http.MethodPost:
		h.handleCreateProposal(w, r)
	default:
		middleware.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleGetProposals handles GET /proposals
func (h *ProposalHandler) handleGetProposals(w http.ResponseWriter, r *http.Request) {
	filter := smartcontract.ProposalFilter{}
	proposals, err := h.store.ListProposals(r.Context(), filter)
	if err != nil {
		middleware.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(proposals)
}

// handleCreateProposal handles POST /proposals
func (h *ProposalHandler) handleCreateProposal(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID               string                 `json:"id"`
		IngestionID      string                 `json:"ingestion_id"`
		ContractID       string                 `json:"contract_id"`
		Title            string                 `json:"title"`
		DescriptionMD    string                 `json:"description_md"`
		VisiblePixelHash string                 `json:"visible_pixel_hash"`
		BudgetSats       int64                  `json:"budget_sats"`
		Status           string                 `json:"status"`
		Metadata         map[string]interface{} `json:"metadata"`
		Tasks            []smartcontract.Task   `json:"tasks"`
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		middleware.Error(w, http.StatusBadRequest, "invalid json")
		return
	}

	proposal := smartcontract.Proposal{
		ID:               body.ID,
		IngestionID:      body.IngestionID,
		ContractID:       body.ContractID,
		Title:            body.Title,
		DescriptionMD:    body.DescriptionMD,
		VisiblePixelHash: body.VisiblePixelHash,
		BudgetSats:       body.BudgetSats,
		Status:           body.Status,
		Metadata:         body.Metadata,
		Tasks:            body.Tasks,
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}

	if err := h.store.CreateProposal(r.Context(), proposal); err != nil {
		middleware.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(proposal)
}
