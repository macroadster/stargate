package models

import (
	"fmt"

	"stargate-backend/core"
)

// InscriptionRequest represents an inscription creation request
type InscriptionRequest struct {
	ImageData        string  `json:"imageData"`
	Text             string  `json:"text"`
	Price            float64 `json:"price"`
	Address          string  `json:"address,omitempty"`
	Timestamp        int64   `json:"timestamp"`
	ID               string  `json:"id"`
	TXID             string  `json:"tx_id,omitempty"`
	Status           string  `json:"status"`
	BlockHeight      int64   `json:"blockHeight,omitempty"`
	VisiblePixelHash string  `json:"visiblePixelHash,omitempty"`
	TotalBudgetSats  int64   `json:"totalBudgetSats,omitempty"`
	AvailableTasks   int     `json:"availableTasks,omitempty"`
}

// SearchResult represents search results
type SearchResult struct {
	Inscriptions []SearchResultItem `json:"inscriptions"`
	Transactions []SearchResultItem `json:"transactions"`
	Blocks       []SearchResultItem `json:"blocks"`
	Contracts    []SearchResultItem `json:"contracts"`
	Proposals    []SearchResultItem `json:"proposals"`
}

// SearchResultItem represents a single search result with type and navigation info
type SearchResultItem struct {
	Type                 string                 `json:"type"` // inscription, transaction, block, contract, proposal
	ID                   string                 `json:"id"`
	TXID                 string                 `json:"tx_id,omitempty"`
	Title                string                 `json:"title,omitempty"`
	BlockHeight          int64                  `json:"block_height,omitempty"`
	ConfirmedBlockHeight *int                   `json:"confirmed_block_height,omitempty"`
	ContractID           string                 `json:"contract_id,omitempty"`
	ProposalID           string                 `json:"proposal_id,omitempty"`
	Status               string                 `json:"status,omitempty"`
	Timestamp            int64                  `json:"timestamp,omitempty"`
	Text                 string                 `json:"text,omitempty"`
	VisiblePixelHash     string                 `json:"visible_pixel_hash,omitempty"`
	BudgetSats           int64                  `json:"budget_sats,omitempty"`
	TxCount              int                    `json:"tx_count,omitempty"`
	Metadata             map[string]interface{} `json:"metadata,omitempty"`
	StegoImageURL        string                 `json:"stego_image_url,omitempty"`
}

// ErrorResponse is an alias to the canonical (rich, nested) definition in core.
// The old flat {error, code:int, hint} shape is retired in favor of the unified core one.
type ErrorResponse = core.ErrorResponse

// CreateContractRequest represents contract creation request
type CreateContractRequest struct {
	ContractID   string                 `json:"contract_id"`
	BlockHeight  int64                  `json:"block_height"`
	ContractType string                 `json:"contract_type"`
	Metadata     map[string]interface{} `json:"metadata"`
}

// InscribeRequest represents inscription request
type InscribeRequest struct {
	Text    string `json:"text"`
	Price   string `json:"price"`
	Address string `json:"address,omitempty"`
}

// PendingTransactionsResponse represents pending transactions response
type PendingTransactionsResponse struct {
	Transactions []InscriptionRequest `json:"transactions"`
	Total        int                  `json:"total"`
}

// APIResponse represents a generic API response
type APIResponse struct {
	Success bool                   `json:"success"`
	Data    interface{}            `json:"data,omitempty"`
	Error   *ErrorResponse         `json:"error,omitempty"`
	Meta    map[string]interface{} `json:"meta,omitempty"`
}

// NewSuccessResponse creates a success response
func NewSuccessResponse(data interface{}) *APIResponse {
	return &APIResponse{
		Success: true,
		Data:    data,
	}
}

// NewErrorResponse creates an error response using the canonical core error shape (nested details).
// The int code is stringified for the unified core.ErrorDetails.Code field.
func NewErrorResponse(error string, code int) *APIResponse {
	coreErr := core.NewErrorResponse(fmt.Sprintf("%d", code), error, "", map[string]interface{}{})
	// core.NewErrorResponse populates Timestamp + the nested structure.
	// We embed it directly into the legacy APIResponse envelope for the inscription paths.
	return &APIResponse{
		Success: false,
		Error:   &coreErr, // *core.ErrorResponse via alias
	}
}
