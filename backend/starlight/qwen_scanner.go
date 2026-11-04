package starlight

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	qwen "github.com/macroadster/trin/pkg/qwen"
	trinstar "github.com/macroadster/trin/pkg/starlight"

	"stargate-backend/core"
	"stargate-backend/stego"
)

// qwenMaxNew bounds how many tokens a scan may generate.
// One Qwen2.5-0.5B token is about a second on CPU in Trin.
var qwenMaxNew = 16

// QwenScanner runs the Opus Qwen2.5-0.5B GGUF inside Trin.
// The file is a language model, so the forward consumes a short ChatML prompt
// that includes any alpha-LSB text already recovered. It does not replace the
// CNN for a GGUF whose architecture is not qwen2.
type QwenScanner struct {
	modelPath   string
	session     *qwen.Session
	initialized bool
	mu          sync.Mutex
}

// NewQwenScanner creates a scanner for a Qwen2 GGUF. Call Initialize before use.
func NewQwenScanner(modelPath string) *QwenScanner {
	return &QwenScanner{modelPath: modelPath}
}

// Initialize opens the GGUF and loads the Qwen2 hparams and tokenizer.
func (s *QwenScanner) Initialize() error {
	if s.modelPath == "" {
		return fmt.Errorf("qwen model path empty")
	}
	if s.session != nil {
		_ = s.session.Close()
		s.session = nil
	}
	s.initialized = false
	sess, err := qwen.Open(s.modelPath)
	if err != nil {
		return fmt.Errorf("qwen open %s: %w", s.modelPath, err)
	}
	s.session = sess
	s.initialized = true
	log.Printf("QwenScanner initialized: path=%s (trin qwen2)", s.modelPath)
	return nil
}

// Close releases the GGUF mapping.
func (s *QwenScanner) Close() error {
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

// ScanImage asks the Qwen2 model about this image and returns its reply.
func (s *QwenScanner) ScanImage(imageData []byte, options core.ScanOptions) (*core.ScanResult, error) {
	if !s.initialized || s.session == nil {
		return nil, fmt.Errorf("QwenScanner not initialized")
	}
	payload := alphaPayload(imageData)
	prompt := qwenScanPrompt(payload)
	n := qwenMaxNew
	if n < 1 {
		n = 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	start := time.Now()
	s.mu.Lock()
	reply, err := s.session.Generate(ctx, prompt, n)
	s.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("qwen generate: %w", err)
	}
	reply = strings.TrimSpace(reply)
	log.Printf("QwenScanner scan done in %s reply_runes=%d alpha=%t", time.Since(start), utf8.RuneCountInString(reply), payload != "")

	result := &core.ScanResult{
		IsStego:          payload != "",
		StegoProbability: 0,
		Confidence:       0,
		Prediction:       "clean",
		ExtractedMessage: reply,
	}
	if payload != "" {
		result.Prediction = "stego"
		result.StegoType = "alpha"
		result.StegoProbability = 1
		result.Confidence = 100
	}
	_ = options
	return result, nil
}

// ScanBlock is not implemented. Block walks call ScanImage per picture.
func (s *QwenScanner) ScanBlock(blockHeight int64, options core.ScanOptions) (*core.BlockScanResponse, error) {
	return &core.BlockScanResponse{
		BlockHeight: blockHeight,
		RequestID:   "not_implemented_in_qwen_scanner",
	}, fmt.Errorf("ScanBlock not implemented in QwenScanner")
}

// ExtractMessage runs the same Qwen2 completion used by ScanImage.
func (s *QwenScanner) ExtractMessage(imageData []byte, method string) (*core.ExtractionResult, error) {
	scan, err := s.ScanImage(imageData, core.ScanOptions{ExtractMessage: true})
	if err != nil {
		return nil, err
	}
	found := strings.TrimSpace(scan.ExtractedMessage) != ""
	return &core.ExtractionResult{
		MessageFound:     found,
		Message:          scan.ExtractedMessage,
		MethodUsed:       methodOr(method, scan.StegoType),
		MethodConfidence: scan.Confidence,
		ExtractionDetails: map[string]any{
			"engine": "qwen2",
			"status": scan.Prediction,
		},
	}, nil
}

// GetScannerInfo reports the loaded language model.
func (s *QwenScanner) GetScannerInfo() core.ScannerInfo {
	return core.ScannerInfo{
		ModelLoaded:  s.initialized,
		ModelVersion: "qwen2.5-0.5b-instruct",
		ModelPath:    s.modelPath,
		Device:       "cpu",
	}
}

// IsInitialized reports whether Open succeeded.
func (s *QwenScanner) IsInitialized() bool {
	return s != nil && s.initialized
}

func alphaPayload(imageData []byte) string {
	img, _, err := stego.DecodeImage(imageData)
	if err != nil {
		return ""
	}
	payload, err := stego.ExtractAlpha(img)
	if err != nil || len(payload) == 0 {
		return ""
	}
	text := strings.TrimSpace(string(payload))
	text = strings.ReplaceAll(text, "<|im_start|>", "")
	text = strings.ReplaceAll(text, "<|im_end|>", "")
	if utf8.RuneCountInString(text) > 240 {
		r := []rune(text)
		text = string(r[:240])
	}
	return text
}

func qwenScanPrompt(payload string) string {
	fact := "none"
	if payload != "" {
		fact = payload
	}
	return "<|im_start|>user\nHidden text: " + fact + "\nQuote the hidden text exactly, or say none.<|im_end|>\n<|im_start|>assistant\n"
}

func methodOr(method, fallback string) string {
	if method != "" {
		return method
	}
	if fallback != "" {
		return fallback
	}
	return "qwen2"
}

// ggufArchitecture returns general.architecture, or "" when the file is not a readable GGUF.
func ggufArchitecture(path string) string {
	s, err := trinstar.Open(path)
	if err != nil {
		return ""
	}
	defer s.Close()
	return s.Meta("general.architecture")
}
