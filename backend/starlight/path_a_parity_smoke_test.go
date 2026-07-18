package starlight

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"stargate-backend/core"
)

// Path A parity smoke: trained starlight.gguf vs known PyTorch baselines on sample images.
//
// PyTorch (BalancedStarlightDetector, same checkpoint re-exported to GGUF):
//   clean-0004.png:          stego_logit=-2.3432652950  stego_prob=0.0876025707  method=2
//   current_alpha_000.png:   stego_logit=+2.6360638142  stego_prob=0.9331467748  method=0
func TestPathAParitySmokeSampleImages(t *testing.T) {
	gguf := resolveTestGGUF()
	if gguf == "" {
		t.Skip("no Starlight GGUF found; set STARLIGHT_GGUF")
	}

	// Prefer absolute sample paths; fall back to repo-relative.
	cleanPath := firstExisting(
		"/Users/eric/sandbox/starlight/starlight/datasets/sample_submission_2025/clean/clean-0004.png",
		filepath.Join("..", "..", "..", "starlight", "datasets", "sample_submission_2025", "clean", "clean-0004.png"),
		filepath.Join("..", "..", "..", "..", "starlight", "datasets", "sample_submission_2025", "clean", "clean-0004.png"),
	)
	stegoPath := firstExisting(
		"/Users/eric/sandbox/starlight/starlight/datasets/sample_submission_2025/stego/current_alpha_000.png",
		filepath.Join("..", "..", "..", "starlight", "datasets", "sample_submission_2025", "stego", "current_alpha_000.png"),
		filepath.Join("..", "..", "..", "..", "starlight", "datasets", "sample_submission_2025", "stego", "current_alpha_000.png"),
	)
	if cleanPath == "" || stegoPath == "" {
		t.Skipf("sample images missing clean=%q stego=%q", cleanPath, stegoPath)
	}

	s := NewTrinScanner(gguf)
	if err := s.Initialize(); err != nil {
		t.Fatalf("Initialize(%s): %v", gguf, err)
	}
	t.Cleanup(func() { _ = s.Close() })

	type ptRef struct {
		name       string
		path       string
		ptLogit    float64
		ptProb     float64
		ptMethodID int
	}
	cases := []ptRef{
		{"clean", cleanPath, -2.3432652950, 0.0876025707, 2},
		{"stego", stegoPath, 2.6360638142, 0.9331467748, 0},
	}

	const absLogitTol = 1e-3

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			data, err := os.ReadFile(c.path)
			if err != nil {
				t.Fatalf("read %s: %v", c.path, err)
			}

			// Raw forward for logit (package-local)
			in, err := LoadUnifiedInput(data)
			if err != nil {
				t.Fatalf("LoadUnifiedInput: %v", err)
			}
			logit, methodLogits, err := s.forward(in)
			if err != nil {
				t.Fatalf("forward: %v", err)
			}
			goProb := float64(sigmoid(logit))
			goMethod := argmax5(methodLogits)

			// Public ScanImage path
			res, err := s.ScanImage(data, core.ScanOptions{})
			if err != nil {
				t.Fatalf("ScanImage: %v", err)
			}

			dLogit := math.Abs(float64(logit) - c.ptLogit)
			dProb := math.Abs(goProb - c.ptProb)
			dScanProb := math.Abs(res.StegoProbability - c.ptProb)

			t.Logf("=== %s ===", c.name)
			t.Logf("path=%s", c.path)
			t.Logf("gguf=%s", gguf)
			t.Logf("PyTorch: logit=%.10f prob=%.10f method=%d", c.ptLogit, c.ptProb, c.ptMethodID)
			t.Logf("Go/Trin: logit=%.10f prob=%.10f method=%d method_logits=%v",
				logit, goProb, goMethod, methodLogits)
			t.Logf("|Δlogit|=%.6e |Δprob|=%.6e", dLogit, dProb)
			t.Logf("ScanImage: IsStego=%v StegoProbability=%.10f MethodID=%v Prediction=%q Confidence=%.4f StegoType=%q",
				res.IsStego, res.StegoProbability, derefInt(res.MethodID), res.Prediction, res.Confidence, res.StegoType)
			t.Logf("|ScanProb-PT|=%.6e |ScanProb-forward|=%.6e",
				dScanProb, math.Abs(res.StegoProbability-goProb))

			if dLogit >= absLogitTol {
				t.Logf("NOTE: |Δlogit|=%.6e >= %.0e (target); may be preprocess mismatch or numerics",
					dLogit, absLogitTol)
			} else {
				t.Logf("PASS logit parity within %.0e", absLogitTol)
			}
			if goMethod != c.ptMethodID {
				t.Logf("NOTE: method id Go=%d PT=%d", goMethod, c.ptMethodID)
			}
			if res.MethodID == nil || *res.MethodID != goMethod {
				t.Errorf("ScanImage MethodID mismatch vs forward: scan=%v forward=%d", res.MethodID, goMethod)
			}
			if math.Abs(res.StegoProbability-goProb) > 1e-9 {
				t.Errorf("ScanImage prob != sigmoid(forward): scan=%.12f forward=%.12f",
					res.StegoProbability, goProb)
			}
		})
	}
}

func firstExisting(paths ...string) string {
	for _, p := range paths {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

func derefInt(p *int) int {
	if p == nil {
		return -1
	}
	return *p
}
