package main

import (
	"encoding/json"
	"time"
)

// ScanOptions represents options for scanning operations
type ScanOptions struct {
	ExtractMessage      bool    `json:"extract_message"`
	ConfidenceThreshold float64 `json:"confidence_threshold"`
	IncludeMetadata     bool    `json:"include_metadata"`
}

// ScanResult represents the result of scanning an image for steganography
type ScanResult struct {
	IsStego          bool    `json:"is_stego"`
	StegoProbability float64 `json:"stego_probability"`
	StegoType        string  `json:"stego_type,omitempty"`
	Confidence       float64 `json:"confidence"`
	Prediction       string  `json:"prediction"`
	MethodID         *int    `json:"method_id,omitempty"`
	ExtractedMessage string  `json:"extracted_message,omitempty"`
	ExtractionError  string  `json:"extraction_error,omitempty"`
}

// ImageScanResult represents the result of scanning a single image
type ImageScanResult struct {
	Index      int        `json:"index"`
	SizeBytes  int        `json:"size_bytes"`
	Format     string     `json:"format"`
	ScanResult ScanResult `json:"scan_result"`
}

// TransactionScanRequest represents a request to scan a Bitcoin transaction
type TransactionScanRequest struct {
	TransactionID string      `json:"transaction_id"`
	ExtractImages bool        `json:"extract_images"`
	ScanOptions   ScanOptions `json:"scan_options"`
}

// TransactionScanResponse represents the response from scanning a transaction
type TransactionScanResponse struct {
	TransactionID string                 `json:"transaction_id"`
	BlockHeight   int                    `json:"block_height"`
	Timestamp     string                 `json:"timestamp"`
	ScanResults   map[string]interface{} `json:"scan_results"`
	Images        []ImageScanResult      `json:"images"`
	RequestID     string                 `json:"request_id"`
}

// DirectImageScanResponse represents the response from scanning a direct image upload
type DirectImageScanResponse struct {
	ScanResult       ScanResult             `json:"scan_result"`
	ImageInfo        map[string]interface{} `json:"image_info"`
	ProcessingTimeMs float64                `json:"processing_time_ms"`
	RequestID        string                 `json:"request_id"`
}

// BatchItem represents an item in a batch scan request
type BatchItem struct {
	Type          string `json:"type"` // "transaction" or "image"
	TransactionID string `json:"transaction_id,omitempty"`
	ImageData     string `json:"image_data,omitempty"` // base64 encoded
}

// BlockScanRequest represents a request to scan all transactions in a Bitcoin block
type BlockScanRequest struct {
	BlockHeight int         `json:"block_height"`
	BlockHash   string      `json:"block_hash,omitempty"`
	ScanOptions ScanOptions `json:"scan_options"`
	Limit       int         `json:"limit,omitempty"` // Limit for performance (default: all)
}

// TransactionResult represents the result for a single transaction in a block
type TransactionResult struct {
	TransactionID    string                 `json:"transaction_id"`
	BlockHeight      int                    `json:"block_height"`
	Status           string                 `json:"status"` // "completed" or "failed"
	StegoDetected    bool                   `json:"stego_detected"`
	ImagesWithStego  int                    `json:"images_with_stego"`
	TotalImages      int                    `json:"total_images"`
	ProcessingTimeMs int64                  `json:"processing_time_ms"`
	ExtractedMessage string                 `json:"extracted_message,omitempty"`
	StegoDetails     map[string]interface{} `json:"stego_details,omitempty"`
	Error            string                 `json:"error,omitempty"`
}

// BlockScanResponse represents the response from a block scan
type BlockScanResponse struct {
	BlockID               string              `json:"block_id"`
	BlockHeight           int                 `json:"block_height"`
	BlockHash             string              `json:"block_hash"`
	TotalTransactions     int                 `json:"total_transactions"`
	ProcessedTransactions int                 `json:"processed_transactions"`
	StegoDetected         int                 `json:"stego_detected"`
	TotalImages           int                 `json:"total_images"`
	ImagesWithStego       int                 `json:"images_with_stego"`
	ProcessingTimeMs      float64             `json:"processing_time_ms"`
	Results               []TransactionResult `json:"results"`
	RequestID             string              `json:"request_id"`
}

// ExtractionResult represents the result of extracting a hidden message
type ExtractionResult struct {
	MessageFound      bool                   `json:"message_found"`
	Message           string                 `json:"message,omitempty"`
	MethodUsed        string                 `json:"method_used,omitempty"`
	MethodConfidence  float64                `json:"method_confidence,omitempty"`
	ExtractionDetails map[string]interface{} `json:"extraction_details"`
}

// ExtractResponse represents the response from message extraction
type ExtractResponse struct {
	ExtractionResult ExtractionResult       `json:"extraction_result"`
	ImageInfo        map[string]interface{} `json:"image_info"`
	ProcessingTimeMs float64                `json:"processing_time_ms"`
	RequestID        string                 `json:"request_id"`
}

// HealthResponse represents the health check response
type HealthResponse struct {
	Status    string      `json:"status"`
	Timestamp string      `json:"timestamp"`
	Version   string      `json:"version"`
	Scanner   ScannerInfo `json:"scanner"`
	Bitcoin   BitcoinInfo `json:"bitcoin"`
}

// ScannerInfo represents scanner status information
type ScannerInfo struct {
	ModelLoaded  bool   `json:"model_loaded"`
	ModelVersion string `json:"model_version"`
	ModelPath    string `json:"model_path"`
	Device       string `json:"device"`
}

// BitcoinInfo represents Bitcoin node status information
type BitcoinInfo struct {
	NodeConnected bool   `json:"node_connected"`
	NodeURL       string `json:"node_url"`
	BlockHeight   int    `json:"block_height"`
}

// InfoResponse represents the API information response
type InfoResponse struct {
	Name             string            `json:"name"`
	Version          string            `json:"version"`
	Description      string            `json:"description"`
	SupportedFormats []string          `json:"supported_formats"`
	StegoMethods     []string          `json:"stego_methods"`
	MaxImageSize     int               `json:"max_image_size"`
	Endpoints        map[string]string `json:"endpoints"`
}

// TransactionInfo represents basic transaction information
type TransactionInfo struct {
	TransactionID string      `json:"transaction_id"`
	BlockHeight   int         `json:"block_height"`
	Timestamp     string      `json:"timestamp"`
	Status        string      `json:"status"`
	Images        []ImageInfo `json:"images,omitempty"`
	TotalImages   int         `json:"total_images"`
}

// ImageInfo represents information about an image in a transaction
type ImageInfo struct {
	Index     int    `json:"index"`
	SizeBytes int    `json:"size_bytes"`
	Format    string `json:"format"`
	DataURL   string `json:"data_url,omitempty"` // base64 data URL
}

// ErrorResponse represents a standard error response
type ErrorResponse struct {
	Error ErrorDetails `json:"error"`
}

// ErrorDetails represents detailed error information
type ErrorDetails struct {
	Code      string                 `json:"code"`
	Message   string                 `json:"message"`
	Details   map[string]interface{} `json:"details,omitempty"`
	Timestamp string                 `json:"timestamp"`
	RequestID string                 `json:"request_id"`
}

// Helper functions for creating responses

func NewHealthResponse(status string, scanner ScannerInfo, bitcoin BitcoinInfo) HealthResponse {
	return HealthResponse{
		Status:    status,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Version:   "1.0.0",
		Scanner:   scanner,
		Bitcoin:   bitcoin,
	}
}

func NewInfoResponse() InfoResponse {
	return InfoResponse{
		Name:             "Starlight Bitcoin Steganography Scanner",
		Version:          "1.0.0",
		Description:      "AI-powered steganography detection for Bitcoin transaction images",
		SupportedFormats: []string{"png", "jpg", "jpeg", "gif", "bmp", "webp"},
		StegoMethods:     []string{"alpha", "palette", "lsb.rgb", "exif", "raw"},
		MaxImageSize:     10485760, // 10MB
		Endpoints: map[string]string{
			"scan_tx":         "/scan/transaction",
			"scan_image":      "/scan/image",
			"block_scan":      "/scan/block",
			"extract":         "/extract",
			"get_transaction": "/transaction/{txid}",
		},
	}
}

func NewErrorResponse(code, message, requestID string, details map[string]interface{}) ErrorResponse {
	return ErrorResponse{
		Error: ErrorDetails{
			Code:      code,
			Message:   message,
			Details:   details,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			RequestID: requestID,
		},
	}
}

func (e ErrorResponse) ToJSON() ([]byte, error) {
	return json.Marshal(e)
}
