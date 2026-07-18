package starlight

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"log"
	"math"
	"os"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"
	trinstar "github.com/ericchien/trin/pkg/starlight"
	"stargate-backend/core"
	"stargate-backend/stego"
)

// Method name map for Starlight detector method head (5 classes).
var trinMethodNames = [5]string{"alpha", "lsb", "palette", "exif", "eoi"}

// TrinScanner is a GGUF-backed Starlight detector implementing StarlightScannerInterface.
// Preprocessing uses LoadUnifiedInput; neural forward runs via trin pkg/starlight Session.
type TrinScanner struct {
	modelPath   string
	initialized bool
	session     *trinstar.Session
}

// NewTrinScanner creates a TrinScanner for the given model path (not yet initialized).
func NewTrinScanner(modelPath string) *TrinScanner {
	return &TrinScanner{modelPath: modelPath}
}

// ResolveTrinModelPath reads STARLIGHT_GGUF, then STARLIGHT_TRIN_MODEL as fallback alias.
// Returns empty string when neither is set.
func ResolveTrinModelPath() string {
	if p := os.Getenv("STARLIGHT_GGUF"); p != "" {
		return p
	}
	return os.Getenv("STARLIGHT_TRIN_MODEL")
}

// Initialize opens the GGUF via trin pkg/starlight and retains a session for Forward.
func (s *TrinScanner) Initialize() error {
	if s.modelPath == "" {
		return fmt.Errorf("trin model path empty")
	}
	info, err := os.Stat(s.modelPath)
	if err != nil {
		return fmt.Errorf("trin model file missing or unreadable: %s: %w", s.modelPath, err)
	}
	if info.IsDir() {
		return fmt.Errorf("trin model path is a directory: %s", s.modelPath)
	}

	// Close any previous session on re-init.
	if s.session != nil {
		_ = s.session.Close()
		s.session = nil
	}
	s.initialized = false

	sess, err := trinstar.Open(s.modelPath)
	if err != nil {
		return fmt.Errorf("trin GGUF open failed (%s): %w", s.modelPath, err)
	}

	// Require at least one tensor so empty/header-only GGUFs fail fast.
	names, err := sess.List()
	if err != nil {
		_ = sess.Close()
		return fmt.Errorf("trin GGUF list tensors failed (%s): %w", s.modelPath, err)
	}
	if len(names) == 0 {
		_ = sess.Close()
		return fmt.Errorf("trin GGUF has no tensors: %s", s.modelPath)
	}

	s.session = sess
	s.initialized = true
	log.Printf("TrinScanner initialized: path=%s tensors=%d (trin/pkg/starlight)", s.modelPath, len(names))
	return nil
}

// Close releases the Trin weight session. Safe to call multiple times.
func (s *TrinScanner) Close() error {
	if s == nil {
		return nil
	}
	s.initialized = false
	if s.session == nil {
		return nil
	}
	err := s.session.Close()
	s.session = nil
	return err
}

// forward runs BalancedStarlightDetector via the open Trin session.
// Spatial dims are always unifiedSize (256) for production UnifiedInput tensors.
func (s *TrinScanner) forward(in *UnifiedInput) (stegoLogit float32, methodLogits [5]float32, err error) {
	if in == nil {
		return 0, methodLogits, fmt.Errorf("nil UnifiedInput")
	}
	if s == nil || s.session == nil {
		return 0, methodLogits, fmt.Errorf("trin session not open")
	}

	out, err := s.session.Forward(trinstar.Inputs{
		Pixel:           in.Pixel,
		Meta:            in.Meta,
		Alpha:           in.Alpha,
		LSB:             in.LSB,
		Palette:         in.Palette,
		PaletteLSB:      in.PaletteLSB,
		FormatFeatures:  in.FormatFeatures,
		ContentFeatures: in.ContentFeatures,
		H:               unifiedSize,
		W:               unifiedSize,
		MetaWeight:      0.3,
	})
	if err != nil {
		return 0, methodLogits, err
	}
	if out == nil || len(out.Stego) < 1 {
		return 0, methodLogits, fmt.Errorf("trin forward: empty stego output")
	}
	if len(out.Method) < 5 {
		return 0, methodLogits, fmt.Errorf("trin forward: method logits len=%d want >=5", len(out.Method))
	}
	stegoLogit = out.Stego[0]
	copy(methodLogits[:], out.Method[:5])
	return stegoLogit, methodLogits, nil
}

func sigmoid(x float32) float32 {
	return float32(1.0 / (1.0 + math.Exp(-float64(x))))
}

func argmax5(v [5]float32) int {
	best := 0
	for i := 1; i < 5; i++ {
		if v[i] > v[best] {
			best = i
		}
	}
	return best
}

// ScanImage preprocesses with LoadUnifiedInput and runs real Trin forward.
func (s *TrinScanner) ScanImage(imageData []byte, options core.ScanOptions) (*core.ScanResult, error) {
	if !s.initialized || s.session == nil {
		return nil, fmt.Errorf("TrinScanner not initialized")
	}

	input, err := LoadUnifiedInput(imageData)
	if err != nil {
		return nil, fmt.Errorf("unified input: %w", err)
	}

	stegoLogit, methodLogits, err := s.forward(input)
	if err != nil {
		return nil, fmt.Errorf("forward: %w", err)
	}

	stegoProb := float64(sigmoid(stegoLogit))
	methodID := argmax5(methodLogits)
	threshold := 0.5
	if options.ConfidenceThreshold > 0 {
		threshold = options.ConfidenceThreshold
	}
	isStego := stegoProb > threshold

	result := &core.ScanResult{
		IsStego:          isStego,
		StegoProbability: stegoProb,
		// Confidence as percentage (aligned with AlphaScanner scale of 0–100)
		Confidence: stegoProb * 100.0,
		MethodID:   &methodID,
	}
	if isStego {
		result.Prediction = "stego"
		result.StegoType = trinMethodNames[methodID]
	} else {
		result.Prediction = "clean"
	}

	if options.ExtractMessage && isStego {
		method := trinMethodNames[methodID]
		ext, extErr := s.ExtractMessage(imageData, method)
		if extErr != nil {
			result.ExtractionError = extErr.Error()
		} else if ext != nil && ext.MessageFound {
			result.ExtractedMessage = ext.Message
		} else if ext != nil && ext.ExtractionDetails != nil {
			if e, ok := ext.ExtractionDetails["error"].(string); ok {
				result.ExtractionError = e
			}
		}
	}

	return result, nil
}

// ScanBlock is not implemented for TrinScanner (requires blockchain orchestration).
func (s *TrinScanner) ScanBlock(blockHeight int64, options core.ScanOptions) (*core.BlockScanResponse, error) {
	return &core.BlockScanResponse{
		BlockHeight: blockHeight,
		RequestID:   "not_implemented_in_trin_scanner",
	}, fmt.Errorf("ScanBlock not implemented in TrinScanner")
}

// ExtractMessage extracts a hidden message; alpha/auto/"" use stego.ExtractAlpha.
// Other methods return not-found for MVP.
func (s *TrinScanner) ExtractMessage(imageData []byte, method string) (*core.ExtractionResult, error) {
	if !s.initialized {
		return nil, fmt.Errorf("TrinScanner not initialized")
	}

	if method != "alpha" && method != "auto" && method != "" {
		return &core.ExtractionResult{
			MessageFound: false,
			MethodUsed:   method,
			ExtractionDetails: map[string]interface{}{
				"status": "method_not_implemented_mvp",
				"method": method,
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

// GetScannerInfo returns scanner metadata.
func (s *TrinScanner) GetScannerInfo() core.ScannerInfo {
	return core.ScannerInfo{
		ModelLoaded:  s.initialized,
		ModelVersion: "trin-pkg-starlight",
		ModelPath:    s.modelPath,
		Device:       "cpu",
	}
}

// IsInitialized reports whether Initialize succeeded.
func (s *TrinScanner) IsInitialized() bool {
	return s.initialized
}
