package models

import "time"

// InscriptionRequest represents an inscription creation request
type InscriptionRequest struct {
	ImageData string  `json:"imageData"`
	Text      string  `json:"text"`
	Price     float64 `json:"price"`
	Address   string  `json:"address,omitempty"`
	Timestamp int64   `json:"timestamp"`
	ID        string  `json:"id"`
	Status    string  `json:"status"`
}

// SmartContractImage represents a smart contract with steganographic image
type SmartContractImage struct {
	ContractID       string                 `json:"contract_id"`
	BlockHeight      int64                  `json:"block_height"`
	StegoImage       string                 `json:"stego_image_url"`
	ContractType     string                 `json:"contract_type"`
	VisiblePixelHash string                 `json:"visible_pixel_hash,omitempty"`
	Metadata         map[string]interface{} `json:"metadata"`
}

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
	Inscriptions []InscriptionRequest `json:"inscriptions"`
	Blocks       []interface{}        `json:"blocks"`
	Contracts    []SmartContractImage `json:"contracts,omitempty"`
}

// HealthResponse represents health check response
type HealthResponse struct {
	Status    string `json:"status"`
	Message   string `json:"message"`
	Timestamp int64  `json:"timestamp"`
}

// ErrorResponse represents API error response
type ErrorResponse struct {
	Error     string `json:"error"`
	Message   string `json:"message,omitempty"`
	Code      int    `json:"code,omitempty"`
	Hint      string `json:"hint,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
}

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
type SmartContractsResponse struct {
	Results []SmartContractImage `json:"results"`
	Total   int                  `json:"total"`
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

// NewErrorResponse creates an error response
func NewErrorResponse(error string, code int) *APIResponse {
	return &APIResponse{
		Success: false,
		Error: &ErrorResponse{
			Error:     error,
			Message:   error,
			Code:      code,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		},
	}
}

// NewErrorResponseWithHint creates an error response with a hint.
func NewErrorResponseWithHint(error string, code int, hint string) *APIResponse {
	resp := NewErrorResponse(error, code)
	if resp != nil && resp.Error != nil {
		resp.Error.Hint = hint
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
