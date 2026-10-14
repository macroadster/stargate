package starlight

import (
	"os"
	"strings"
	"testing"

	"stargate-backend/core"
)

func opusQwenGGUF(t *testing.T) string {
	t.Helper()
	path := os.Getenv("OPUS_QWEN_GGUF")
	if path == "" {
		path = "/Users/eric/sandbox/starlight/starlight/datasets/opus_submission_2026/model/qwen2.5-0.5b-instruct-q8_0.gguf"
	}
	if _, err := os.Stat(path); err != nil {
		t.Skip(err)
	}
	return path
}

func TestTryInitScannersQwen(t *testing.T) {
	path := opusQwenGGUF(t)
	t.Setenv("STARGATE_DATA_DIR", t.TempDir())
	t.Setenv("STARLIGHT_LLM_GGUF", path)
	t.Setenv("STARLIGHT_GGUF", "")
	t.Setenv("STARLIGHT_TRIN_MODEL", "")
	t.Setenv("STARLIGHT_GGUF_FORCE_DOWNLOAD", "")
	blockHFDownload(t)

	sc, typ, err := tryInitScanners()
	if err != nil {
		t.Fatal(err)
	}
	if typ != "qwen2" {
		t.Fatalf("type got %q want qwen2", typ)
	}
	if sc == nil || !sc.IsInitialized() {
		t.Fatal("qwen scanner not initialized")
	}
	info := sc.GetScannerInfo()
	if info.ModelVersion != "qwen2.5-0.5b-instruct" {
		t.Errorf("version %q", info.ModelVersion)
	}
	if c, ok := sc.(*QwenScanner); ok {
		t.Cleanup(func() { _ = c.Close() })
	}
}

func TestQwenScannerQuotesAlpha(t *testing.T) {
	imgPath := "/Users/eric/sandbox/starlight/starlight/datasets/opus_submission_2026/eval/fixtures/opus_protocol_alpha_005.png"
	data, err := os.ReadFile(imgPath)
	if err != nil {
		t.Skip(err)
	}
	path := opusQwenGGUF(t)
	sc := NewQwenScanner(path)
	if err := sc.Initialize(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sc.Close() })
	old := qwenMaxNew
	qwenMaxNew = 32
	t.Cleanup(func() { qwenMaxNew = old })

	res, err := sc.ScanImage(data, core.ScanOptions{ExtractMessage: true})
	if err != nil {
		t.Fatal(err)
	}
	const want = "OPUS-2026-LOOK: the alpha channel kept this note for the 2142 archive."
	t.Logf("reply=%q stego=%v type=%s", res.ExtractedMessage, res.IsStego, res.StegoType)
	if !res.IsStego || res.StegoType != "alpha" {
		t.Fatalf("expected alpha stego, got %+v", res)
	}
	if !strings.Contains(res.ExtractedMessage, want) {
		t.Fatalf("model reply did not quote the payload: %q", res.ExtractedMessage)
	}
}
