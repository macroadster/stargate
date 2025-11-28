package starlight

import (
	"fmt"

	"stargate-backend/core"
)

// StarlightScanner integrates with the Python Starlight scanner
type StarlightScanner struct {
	modelPath   string
	pythonPath  string
	scriptPath  string
	initialized bool
	quiet       bool
}

// NewStarlightScanner creates a new Starlight scanner instance
func NewStarlightScanner(modelPath string) *StarlightScanner {
	if modelPath == "" {
		modelPath = "models/detector_balanced.onnx"
	}

	return &StarlightScanner{
		modelPath:   modelPath,
		pythonPath:  "python3",
		scriptPath:  "../../starlight/bitcoin_api_simple.py",
		initialized: false,
		quiet:       true,
	}
}

// Initialize initializes the scanner
func (s *StarlightScanner) Initialize() error {
	// This scanner is deprecated - use ProxyScanner instead
	// Real steganography scanning should be done via Python API
	return fmt.Errorf("StarlightScanner deprecated - use ProxyScanner to connect to Python backend")
}

// ScanImage scans an image for steganography
func (s *StarlightScanner) ScanImage(imageData []byte, options core.ScanOptions) (*core.ScanResult, error) {
	return nil, fmt.Errorf("StarlightScanner deprecated - use ProxyScanner to connect to Python backend")
}

// ExtractMessage extracts hidden message from steganographic image
func (s *StarlightScanner) ExtractMessage(imageData []byte, method string) (*core.ExtractionResult, error) {
	return nil, fmt.Errorf("StarlightScanner deprecated - use ProxyScanner to connect to Python backend")
}

// GetScannerInfo returns information about the scanner
func (s *StarlightScanner) GetScannerInfo() core.ScannerInfo {
	return core.ScannerInfo{
		ModelLoaded:  false,
		ModelVersion: "deprecated",
		ModelPath:    "deprecated",
		Device:       "deprecated",
	}
}

// IsInitialized returns whether the scanner is initialized
func (s *StarlightScanner) IsInitialized() bool {
	return false
}
