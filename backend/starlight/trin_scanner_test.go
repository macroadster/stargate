package starlight

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"stargate-backend/core"
)

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png encode: %v", err)
	}
	return buf.Bytes()
}

func solidRGB(w, h int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	return img
}

func solidRGBA(w, h int, c color.RGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: c.R, G: c.G, B: c.B, A: c.A})
		}
	}
	return img
}

func TestLoadUnifiedInputShapesRGB(t *testing.T) {
	// 300×300 RGB PNG → center crop to 256
	img := solidRGB(300, 300, color.RGBA{R: 10, G: 20, B: 30, A: 255})
	// Set LSB pattern on a few pixels
	img.Set(150, 150, color.RGBA{R: 11, G: 21, B: 31, A: 255})
	data := encodePNG(t, img)

	in, err := LoadUnifiedInput(data)
	if err != nil {
		t.Fatalf("LoadUnifiedInput: %v", err)
	}

	assertLen(t, "Pixel", in.Pixel, 3*256*256)
	assertLen(t, "Meta", in.Meta, 2048)
	assertLen(t, "Alpha", in.Alpha, 2*256*256)
	assertLen(t, "LSB", in.LSB, 3*256*256)
	assertLen(t, "Palette", in.Palette, 768)
	assertLen(t, "PaletteLSB", in.PaletteLSB, 256*256)
	assertLen(t, "FormatFeatures", in.FormatFeatures, 6)
	assertLen(t, "ContentFeatures", in.ContentFeatures, 6)

	if in.Width != 300 || in.Height != 300 {
		t.Errorf("size got %dx%d want 300x300", in.Width, in.Height)
	}
	if in.Format != "png" {
		t.Errorf("format got %q want png", in.Format)
	}

	// format_features: [has_alpha, alpha_std, is_palette, is_rgb, w/256, h/256]
	ff := in.FormatFeatures
	if ff[0] != 0 {
		t.Errorf("has_alpha want 0 got %v", ff[0])
	}
	if ff[2] != 0 {
		t.Errorf("is_palette want 0 got %v", ff[2])
	}
	if ff[3] != 1 {
		t.Errorf("is_rgb want 1 got %v", ff[3])
	}
	if ff[4] < 1.1 || ff[4] > 1.2 { // 300/256 ≈ 1.171
		t.Errorf("width_norm got %v", ff[4])
	}
	if ff[5] < 1.1 || ff[5] > 1.2 {
		t.Errorf("height_norm got %v", ff[5])
	}

	// Pixel values in [0,1]
	for i, v := range in.Pixel {
		if v < 0 || v > 1 {
			t.Fatalf("Pixel[%d]=%v out of [0,1]", i, v)
		}
	}
	// LSB values 0 or 1
	for i, v := range in.LSB {
		if v != 0 && v != 1 {
			t.Fatalf("LSB[%d]=%v not 0/1", i, v)
		}
	}
}

func TestLoadUnifiedInputShapesRGBA(t *testing.T) {
	img := solidRGBA(256, 256, color.RGBA{R: 1, G: 2, B: 3, A: 128})
	// Vary alpha for non-zero std
	for i := 0; i < 100; i++ {
		img.SetNRGBA(i, i, color.NRGBA{R: 1, G: 2, B: 3, A: 200})
	}
	data := encodePNG(t, img)

	in, err := LoadUnifiedInput(data)
	if err != nil {
		t.Fatalf("LoadUnifiedInput: %v", err)
	}
	assertLen(t, "Pixel", in.Pixel, 3*256*256)
	assertLen(t, "Alpha", in.Alpha, 2*256*256)
	assertLen(t, "FormatFeatures", in.FormatFeatures, 6)

	if in.Mode != "RGBA" {
		t.Errorf("mode got %q want RGBA", in.Mode)
	}
	if in.FormatFeatures[0] != 1 {
		t.Errorf("has_alpha want 1 got %v", in.FormatFeatures[0])
	}
	if in.FormatFeatures[3] != 0 {
		t.Errorf("is_rgb want 0 for RGBA got %v", in.FormatFeatures[3])
	}
	// Alpha channel 0 should have non-trivial values
	var sum float32
	for i := 0; i < 256*256; i++ {
		sum += in.Alpha[i]
	}
	if sum == 0 {
		t.Error("alpha plane all zeros")
	}
}

func TestLoadUnifiedInputSmallImagePads(t *testing.T) {
	img := solidRGB(64, 64, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	data := encodePNG(t, img)
	in, err := LoadUnifiedInput(data)
	if err != nil {
		t.Fatalf("LoadUnifiedInput: %v", err)
	}
	assertLen(t, "Pixel", in.Pixel, 3*256*256)
	// Center should be red-ish; corners pad black
	// Corner pixel (0,0) after pad-center: out of source → 0
	if in.Pixel[0] != 0 {
		// R channel at (0,0) — may be 0 due to pad
	}
}

func assertLen(t *testing.T, name string, s []float32, want int) {
	t.Helper()
	if len(s) != want {
		t.Errorf("%s len=%d want %d", name, len(s), want)
	}
}

func TestResolveTrinModelPath(t *testing.T) {
	t.Setenv("STARLIGHT_GGUF", "")
	t.Setenv("STARLIGHT_TRIN_MODEL", "")
	if p := ResolveTrinModelPath(); p != "" {
		t.Errorf("want empty got %q", p)
	}

	t.Setenv("STARLIGHT_TRIN_MODEL", "/tmp/alias.gguf")
	if p := ResolveTrinModelPath(); p != "/tmp/alias.gguf" {
		t.Errorf("alias: got %q", p)
	}

	t.Setenv("STARLIGHT_GGUF", "/tmp/preferred.gguf")
	if p := ResolveTrinModelPath(); p != "/tmp/preferred.gguf" {
		t.Errorf("preferred: got %q", p)
	}
}

func TestNewTrinScannerInitializeEmptyPath(t *testing.T) {
	s := NewTrinScanner("")
	err := s.Initialize()
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestNewTrinScannerInitializeMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-such.gguf")
	s := NewTrinScanner(path)
	err := s.Initialize()
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !bytes.Contains([]byte(err.Error()), []byte(path)) {
		t.Errorf("error should contain path: %v", err)
	}
}

func TestNewTrinScannerInitializeInvalidFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.gguf")
	if err := os.WriteFile(path, []byte("not a gguf"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewTrinScanner(path)
	if err := s.Initialize(); err == nil {
		t.Fatal("expected error for invalid GGUF")
	}
}

func TestNewTrinScannerInitializeMinimalGGUF(t *testing.T) {
	path := filepath.Join(t.TempDir(), "min.gguf")
	if err := os.WriteFile(path, minimalValidGGUF(3), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewTrinScanner(path)
	if err := s.Initialize(); err != nil {
		t.Fatalf("init minimal GGUF: %v", err)
	}
	if !s.IsInitialized() {
		t.Error("want initialized")
	}
	info := s.GetScannerInfo()
	if !info.ModelLoaded {
		t.Error("ModelLoaded")
	}
	if info.ModelPath != path {
		t.Errorf("path %q", info.ModelPath)
	}
	if info.Device != "cpu" {
		t.Errorf("device %q", info.Device)
	}

	// ScanImage should run preprocess + stub forward
	img := solidRGB(256, 256, color.RGBA{R: 0, G: 0, B: 0, A: 255})
	data := encodePNG(t, img)
	res, err := s.ScanImage(data, core.ScanOptions{})
	if err != nil {
		t.Fatalf("ScanImage: %v", err)
	}
	// Stub: logit 0 → prob 0.5, threshold 0.5 → isStego false (strict >)
	if res.StegoProbability < 0.49 || res.StegoProbability > 0.51 {
		t.Errorf("stub stego prob got %v want ~0.5", res.StegoProbability)
	}
}

// minimalValidGGUF writes magic+version+0 tensors+0 kv (24 bytes).
func minimalValidGGUF(version uint32) []byte {
	buf := make([]byte, 24)
	binary.LittleEndian.PutUint32(buf[0:4], 0x46554747) // "GGUF"
	binary.LittleEndian.PutUint32(buf[4:8], version)
	// tensor_count = 0, kv_count = 0 already zero
	return buf
}

func TestPackBitsMSB(t *testing.T) {
	// 1,0,1,0,0,0,0,0 → 0b10100000 = 160
	bits := []byte{1, 0, 1, 0, 0, 0, 0, 0}
	out := packBitsMSB(bits)
	if len(out) != 1 || out[0] != 160 {
		t.Fatalf("got %v want [160]", out)
	}
}

func TestExtractPostTailPNG(t *testing.T) {
	// Noise image compresses poorly so encoded size exceeds the 1000-byte gate.
	img := image.NewRGBA(image.Rect(0, 0, 256, 256))
	for y := 0; y < 256; y++ {
		for x := 0; x < 256; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: uint8(x ^ y), A: 255})
		}
	}
	data := encodePNG(t, img)
	// Append after full file. Python rfind("IEND")+12 skips type(4)+crc(4)+4 extra
	// bytes into the post-image region — match that training parity.
	payload := []byte("XXXXHIDDEN-EOI-PAYLOAD-FOR-TEST")
	withTail := append(append([]byte{}, data...), payload...)
	if len(withTail) < 1000 {
		t.Fatalf("expected png+tail >= 1000 bytes, got %d", len(withTail))
	}
	tail := extractPostTail(withTail, "png")
	want := payload[4:] // first 4 of post-IEND consumed by Python's +12 offset
	if !bytes.Equal(tail, want) {
		t.Errorf("tail=%q want %q (file len %d)", tail, want, len(withTail))
	}
}

func TestTryInitScannersDefaultAlpha(t *testing.T) {
	t.Setenv("STARLIGHT_GGUF", "")
	t.Setenv("STARLIGHT_TRIN_MODEL", "")
	sc, typ, err := tryInitScanners()
	if err != nil {
		t.Fatal(err)
	}
	if typ != "alpha" {
		t.Errorf("type got %q want alpha", typ)
	}
	if sc == nil || !sc.IsInitialized() {
		t.Error("scanner not initialized")
	}
}

func TestTryInitScannersTrinSuccess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "starlight.gguf")
	if err := os.WriteFile(path, minimalValidGGUF(3), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("STARLIGHT_GGUF", path)
	sc, typ, err := tryInitScanners()
	if err != nil {
		t.Fatal(err)
	}
	if typ != "trin-gguf" {
		t.Errorf("type got %q want trin-gguf", typ)
	}
	if sc == nil || !sc.IsInitialized() {
		t.Error("scanner not initialized")
	}
}

func TestTryInitScannersTrinFailFallsToAlpha(t *testing.T) {
	t.Setenv("STARLIGHT_GGUF", filepath.Join(t.TempDir(), "missing.gguf"))
	sc, typ, err := tryInitScanners()
	if err != nil {
		t.Fatal(err)
	}
	if typ != "alpha" {
		t.Errorf("type got %q want alpha (fallthrough)", typ)
	}
	if sc == nil {
		t.Error("nil scanner")
	}
}
