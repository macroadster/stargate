package starlight

import (
	"stargate-backend/core"
)

// MockStarlightScanner provides a mock implementation for testing
type MockStarlightScanner struct{}

// NewMockStarlightScanner creates a new mock scanner
func NewMockStarlightScanner() *MockStarlightScanner {
	return &MockStarlightScanner{}
}

// Initialize initializes the mock scanner
func (m *MockStarlightScanner) Initialize() error {
	return nil
}

// ScanImage scans an image with mock results
func (m *MockStarlightScanner) ScanImage(imageData []byte, options core.ScanOptions) (*core.ScanResult, error) {
	// Return mock results
	return &core.ScanResult{
		IsStego:          false,
		StegoProbability: 0.1,
		Confidence:       0.95,
		Prediction:       "clean",
	}, nil
}

// ExtractMessage extracts a message with mock results
func (m *MockStarlightScanner) ExtractMessage(imageData []byte, method string) (*core.ExtractionResult, error) {
	return &core.ExtractionResult{
		MessageFound: false,
		ExtractionDetails: map[string]any{
			"bits_extracted":      0,
			"encoding":            "utf-8",
			"corruption_detected": false,
		},
	}, nil
}

// GetScannerInfo returns mock scanner info
func (m *MockStarlightScanner) GetScannerInfo() core.ScannerInfo {
	return core.ScannerInfo{
		ModelLoaded:  true,
		ModelVersion: "mock-1.0.0",
		ModelPath:    "mock-scanner",
		Device:       "mock-device",
	}
}

// ScanBlock scans a block with mock results
func (m *MockStarlightScanner) ScanBlock(blockHeight int64, options core.ScanOptions) (*core.BlockScanResponse, error) {
	// Return mock block scan results
	return &core.BlockScanResponse{
		BlockHeight:       blockHeight,
		BlockHash:         "mock-block-hash",
		Timestamp:         1234567890,
		TotalInscriptions: 10,
		ImagesScanned:     10,
		StegoDetected:     0,
		ProcessingTimeMs:  100.0,
		Inscriptions:      []core.BlockScanInscription{},
		RequestID:         "mock-request-id",
	}, nil
}

// IsInitialized returns true for mock scanner
func (m *MockStarlightScanner) IsInitialized() bool {
	return true
}
