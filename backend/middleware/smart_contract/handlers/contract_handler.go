package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/btcsuite/btcd/chaincfg"
	"stargate-backend/bitcoin"
	"stargate-backend/core/smart_contract"
	smartstore "stargate-backend/middleware/smart_contract"
	"stargate-backend/middleware/smart_contract/middleware"
	"stargate-backend/services"
)

// ContractHandler handles contract-related HTTP endpoints
type ContractHandler struct {
	store        smartstore.Store
	mempool      *bitcoin.MempoolClient
	ingestionSvc *services.IngestionService
}

// NewContractHandler creates a new contract handler
func NewContractHandler(store smartstore.Store, mempool *bitcoin.MempoolClient, ingestionSvc *services.IngestionService) *ContractHandler {
	return &ContractHandler{
		store:        store,
		mempool:      mempool,
		ingestionSvc: ingestionSvc,
	}
}

// Contracts handles GET/POST /contracts
func (h *ContractHandler) Contracts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.handleGetContracts(w, r)
	case http.MethodPost:
		h.handleCreateContract(w, r)
	default:
		middleware.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// ContractPSBT handles POST /contracts/{id}/psbt
func (h *ContractHandler) ContractPSBT(w http.ResponseWriter, r *http.Request, contractID string) {
	if !h.validatePSBTRequest(w, r) {
		return
	}

	var body struct {
		ContractorAPIKey string   `json:"contractor_api_key"`
		ContractorWallet string   `json:"contractor_wallet"`
		PayerAddresses   []string `json:"payer_addresses"`
		ChangeAddress    string   `json:"change_address"`
		BudgetSats       int64    `json:"budget_sats"`
		PixelHash        string   `json:"pixel_hash"`
		CommitmentSats   int64    `json:"commitment_sats"`
		FeeRate          int64    `json:"fee_rate_sats_vb"`
		UsePixelHash     *bool    `json:"use_pixel_hash"`
		CommitmentTarget string   `json:"commitment_target"`
		TaskID           string   `json:"task_id"`
		SplitPSBT        bool     `json:"split_psbt"`
		Payouts          []struct {
			Address    string `json:"address"`
			AmountSats int64  `json:"amount_sats"`
		} `json:"payouts"`
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		middleware.Error(w, http.StatusBadRequest, "invalid json")
		return
	}

	contract, err := h.store.GetContract(contractID)
	if err != nil {
		middleware.Error(w, http.StatusNotFound, err.Error())
		return
	}

	params := &chaincfg.TestNet4Params

	response := map[string]interface{}{
		"message":     "PSBT handling extracted - implementation needed",
		"contract_id": contractID,
		"contract":    contract,
		"params":      params,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// validatePSBTRequest validates common PSBT request requirements
func (h *ContractHandler) validatePSBTRequest(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Content-Type") != "" && !strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		middleware.Error(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return false
	}

	if h.mempool == nil {
		middleware.Error(w, http.StatusServiceUnavailable, "psbt builder unavailable")
		return false
	}

	return true
}

// handleGetContracts handles GET /contracts
func (h *ContractHandler) handleGetContracts(w http.ResponseWriter, r *http.Request) {
	filter := smart_contract.ContractFilter{}
	contracts, err := h.store.ListContracts(filter)
	if err != nil {
		middleware.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(contracts)
}

// handleCreateContract handles POST /contracts
func (h *ContractHandler) handleCreateContract(w http.ResponseWriter, r *http.Request) {
	var contract smart_contract.Contract
	if err := json.NewDecoder(r.Body).Decode(&contract); err != nil {
		middleware.Error(w, http.StatusBadRequest, "invalid json")
		return
	}

	middleware.Error(w, http.StatusNotImplemented, "contract creation not implemented")
}
