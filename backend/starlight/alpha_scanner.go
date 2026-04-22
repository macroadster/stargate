package starlight

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"log"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"
	"stargate-backend/core"
	"stargate-backend/stego"
)

// AlphaScanner is a Go-native steganography scanner using the Alpha LSB algorithm.
type AlphaScanner struct {
	initialized bool
}

// NewAlphaScanner creates a new AlphaScanner.
func NewAlphaScanner() *AlphaScanner {
	return &AlphaScanner{
		initialized: false,
	}
}

// Initialize initializes the scanner.
func (s *AlphaScanner) Initialize() error {
	s.initialized = true
	log.Printf("AlphaScanner initialized (Go-native)")
	return nil
}

// ScanImage scans an image for steganography using the Alpha LSB algorithm.
func (s *AlphaScanner) ScanImage(imageData []byte, options core.ScanOptions) (*core.ScanResult, error) {
	if !s.initialized {
		return nil, fmt.Errorf("AlphaScanner not initialized")
	}

	img, _, err := image.Decode(bytes.NewReader(imageData))
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %v", err)
	}

	payload, err := stego.ExtractAlpha(img)
	if err != nil {
		return &core.ScanResult{
			IsStego:    false,
			Confidence: 0,
			Prediction: "error",
			ExtractionError: err.Error(),
		}, nil
	}

	if payload != nil {
		result := &core.ScanResult{
			IsStego:          true,
			StegoProbability: 1.0,
			Confidence:       100.0,
			Prediction:       "stego",
			StegoType:        "alpha",
		}
		if options.ExtractMessage {
			result.ExtractedMessage = string(payload)
		}
		return result, nil
	}

	return &core.ScanResult{
		IsStego:          false,
		StegoProbability: 0.0,
		Confidence:       0.0,
		Prediction:       "clean",
	}, nil
}

// ScanBlock is currently not implemented for AlphaScanner as it requires blockchain access.
// In a single-binary architecture, this should be handled by the core logic iterating over block transactions.
func (s *AlphaScanner) ScanBlock(blockHeight int64, options core.ScanOptions) (*core.BlockScanResponse, error) {
	return &core.BlockScanResponse{
		BlockHeight: blockHeight,
		RequestID:   "not_implemented_in_alpha_scanner",
	}, fmt.Errorf("ScanBlock not implemented in native AlphaScanner")
}

// ExtractMessage extracts a hidden message using the Alpha LSB algorithm.
func (s *AlphaScanner) ExtractMessage(imageData []byte, method string) (*core.ExtractionResult, error) {
	if !s.initialized {
		return nil, fmt.Errorf("AlphaScanner not initialized")
	}

	// Currently only "alpha" or "auto" is supported.
	if method != "alpha" && method != "auto" && method != "" {
		return &core.ExtractionResult{
			MessageFound: false,
			ExtractionDetails: map[string]interface{}{
				"error": fmt.Sprintf("unsupported method: %s", method),
			},
		}, nil
	}

	img, _, err := image.Decode(bytes.NewReader(imageData))
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %v", err)
	}

	payload, err := stego.ExtractAlpha(img)
	if err != nil {
		return nil, err
	}

	if payload != nil {
		return &core.ExtractionResult{
			MessageFound:     true,
			Message:          string(payload),
			MethodUsed:       "alpha",
			MethodConfidence: 100.0,
			ExtractionDetails: map[string]interface{}{
				"algorithm": "alpha_lsb",
			},
		}, nil
	}

	return &core.ExtractionResult{
		MessageFound: false,
		ExtractionDetails: map[string]interface{}{
			"status": "no_message_found",
		},
	}, nil
}

// GetScannerInfo returns information about the scanner.
func (s *AlphaScanner) GetScannerInfo() core.ScannerInfo {
	return core.ScannerInfo{
		ModelLoaded:  s.initialized,
		ModelVersion: "Go-Alpha-v1.0",
		ModelPath:    "native",
		Device:       "cpu",
	}
}

// IsInitialized returns whether the scanner is initialized.
func (s *AlphaScanner) IsInitialized() bool {
	return s.initialized
}
