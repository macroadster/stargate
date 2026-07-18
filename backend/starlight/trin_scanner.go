package starlight

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"log"
	"math"
	"os"
	"sync"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"
	"stargate-backend/core"
	"stargate-backend/stego"
)

// Method name map for Starlight detector method head (5 classes).
var trinMethodNames = [5]string{"alpha", "lsb", "palette", "exif", "eoi"}

// TrinScanner is a GGUF-backed Starlight detector implementing StarlightScannerInterface.
// Preprocessing (LoadUnifiedInput) is real; the neural forward pass is stubbed until
// Trin emit-go / starlight_infer is linked (see ForwardStarlight).
type TrinScanner struct {
	modelPath   string
	initialized bool
	gguf        *MinimalGGUF
}

// MinimalGGUF holds a validated GGUF header / open handle (no full dequant in MVP).
type MinimalGGUF struct {
	Path     string
	Version  uint32
	NTensors uint64
	NKV      uint64
	file     *os.File
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

// Initialize validates the model path and opens a minimal GGUF header.
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

	if s.gguf != nil {
		_ = s.gguf.Close()
		s.gguf = nil
	}

	gguf, err := OpenMinimalGGUF(s.modelPath)
	if err != nil {
		return fmt.Errorf("trin GGUF open failed (%s): %w", s.modelPath, err)
	}
	s.gguf = gguf
	s.initialized = true
	log.Printf("TrinScanner initialized: path=%s version=%d tensors=%d", s.modelPath, gguf.Version, gguf.NTensors)
	return nil
}

// OpenMinimalGGUF validates GGUF magic/version and records tensor/kv counts.
// Does not import trin/internal; this is a self-contained MVP loader.
func OpenMinimalGGUF(path string) (*MinimalGGUF, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	// Header: magic u32 LE, version u32, tensor_count u64, metadata_kv_count u64
	var hdr struct {
		Magic      uint32
		Version    uint32
		NTensors   uint64
		NMetadata  uint64
	}
	if err := binary.Read(f, binary.LittleEndian, &hdr); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("read GGUF header: %w", err)
	}
	// Magic LE 0x46554747 == "GGUF"
	if hdr.Magic != 0x46554747 {
		_ = f.Close()
		return nil, fmt.Errorf("invalid GGUF magic 0x%08x (want GGUF)", hdr.Magic)
	}
	if hdr.Version != 2 && hdr.Version != 3 {
		_ = f.Close()
		return nil, fmt.Errorf("unsupported GGUF version %d (want 2 or 3)", hdr.Version)
	}

	return &MinimalGGUF{
		Path:     path,
		Version:  hdr.Version,
		NTensors: hdr.NTensors,
		NKV:      hdr.NMetadata,
		file:     f,
	}, nil
}

// Close releases the underlying file handle.
func (g *MinimalGGUF) Close() error {
	if g == nil || g.file == nil {
		return nil
	}
	err := g.file.Close()
	g.file = nil
	return err
}

var forwardStubOnce sync.Once

// ForwardStarlight runs Starlight detector forward.
// TODO: link trin emit-go / starlight_infer generated package when available.
// Current MVP: weights handle validates GGUF was opened; forward returns neutral stub
// (stego logit 0 → sigmoid 0.5, flat method logits) and logs once that forward is stubbed.
func ForwardStarlight(in *UnifiedInput, w *MinimalGGUF) (stegoLogit float32, methodLogits [5]float32, err error) {
	if in == nil {
		return 0, methodLogits, fmt.Errorf("nil UnifiedInput")
	}
	if w == nil {
		return 0, methodLogits, fmt.Errorf("nil GGUF weights")
	}
	forwardStubOnce.Do(func() {
		log.Printf("ForwardStarlight: neural forward is STUBBED (MVP); GGUF open is real. " +
			"Replace body with Trin emit-go / starlight_infer when available.")
	})
	// Neutral stub: logit 0 → prob 0.5; equal method logits (argmax → 0 = alpha)
	return 0, [5]float32{0, 0, 0, 0, 0}, nil
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

// ScanImage preprocesses with LoadUnifiedInput and runs ForwardStarlight.
func (s *TrinScanner) ScanImage(imageData []byte, options core.ScanOptions) (*core.ScanResult, error) {
	if !s.initialized {
		return nil, fmt.Errorf("TrinScanner not initialized")
	}

	input, err := LoadUnifiedInput(imageData)
	if err != nil {
		return nil, fmt.Errorf("unified input: %w", err)
	}

	stegoLogit, methodLogits, err := ForwardStarlight(input, s.gguf)
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
		ModelVersion: "trin-gguf-v0.1-stub",
		ModelPath:    s.modelPath,
		Device:       "cpu",
	}
}

// IsInitialized reports whether Initialize succeeded.
func (s *TrinScanner) IsInitialized() bool {
	return s.initialized
}
