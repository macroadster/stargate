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

// SmartContractImage is an alias to the canonical definition in core (see core/types.go).
// This preserves compatibility for any external references while eliminating the duplicate.
type SmartContractImage = core.SmartContractImage

// ContractMetadata represents smart contract metadata
type ContractMetadata struct {
	Name        string `json:"name"`
	Symbol      string `json:"symbol"`
	Decimals    int    `json:"decimals,omitempty"`
	TotalSupply string `json:"total_supply,omitempty"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

// Block represents a Bitcoin block
type Block struct {
	ID             string                 `json:"id"`
	Height         int64                  `json:"height"`
	Timestamp      int64                  `json:"timestamp"`
	Hash           string                 `json:"hash"`
	TxCount        int                    `json:"tx_count"`
	Size           int                    `json:"size"`
	SmartContracts int                    `json:"smart_contracts"`
	Extra          map[string]interface{} `json:"extra,omitempty"`
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

// HealthResponse is an alias to the canonical definition in core (richer shape with Version/Scanner/Bitcoin + RFC3339 timestamp).
// Old simple {Message + unix ts} shape removed as part of type unification.
type HealthResponse = core.HealthResponse

// ErrorResponse is an alias to the canonical (rich, nested) definition in core.
// The old flat {error, code:int, hint} shape is retired in favor of the unified core one.
type ErrorResponse = core.ErrorResponse

// SuccessResponse represents API success response
type SuccessResponse struct {
	Status string      `json:"status"`
	Data   interface{} `json:"data,omitempty"`
}

// QRCodeRequest represents QR code generation request
type QRCodeRequest struct {
	Address string `json:"address"`
	Amount  string `json:"amount,omitempty"`
}

// BlockImagesRequest represents block images request
type BlockImagesRequest struct {
	Height int64 `json:"height"`
}

// BlockImagesResponse represents block images response
type BlockImagesResponse struct {
	Images []map[string]interface{} `json:"images"`
	Total  int                      `json:"total"`
}

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

// SearchRequest represents search request
type SearchRequest struct {
	Query string `json:"q"`
}

// PendingTransactionsResponse represents pending transactions response
type PendingTransactionsResponse struct {
	Transactions []InscriptionRequest `json:"transactions"`
	Total        int                  `json:"total"`
}

// SmartContractsResponse represents smart contracts response
// Uses the canonical core.SmartContractImage (unified type definition).
type SmartContractsResponse struct {
	Results []core.SmartContractImage `json:"results"`
	Total   int                       `json:"total"`
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

// NewErrorResponseWithHint creates an error response with a hint (stored in Details for the canonical shape).
func NewErrorResponseWithHint(error string, code int, hint string) *APIResponse {
	resp := NewErrorResponse(error, code)
	if resp != nil && resp.Error != nil {
		// Attach hint into the canonical details map (core shape has no top-level Hint).
		if resp.Error.Error.Details == nil {
			resp.Error.Error.Details = map[string]interface{}{}
		}
		resp.Error.Error.Details["hint"] = hint
	}
	return resp
}

// NewSuccessResponseWithMeta creates a success response with metadata
func NewSuccessResponseWithMeta(data interface{}, meta map[string]interface{}) *APIResponse {
	return &APIResponse{
		Success: true,
		Data:    data,
		Meta:    meta,
	}
}
