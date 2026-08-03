package starlight

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"testing"

	"stargate-backend/core"
)

func TestAlphaScannerExtractAVIFSoftFail(t *testing.T) {
	s := NewAlphaScanner()
	if err := s.Initialize(); err != nil {
		t.Fatal(err)
	}

	// Minimal ftypavif
	avif := []byte{
		0x00, 0x00, 0x00, 0x20,
		'f', 't', 'y', 'p',
		'a', 'v', 'i', 'f',
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
	}
	res, err := s.ExtractMessage(avif, "auto")
	if err != nil {
		t.Fatalf("ExtractMessage should soft-fail, got err: %v", err)
	}
	if res.MessageFound {
		t.Fatal("expected MessageFound=false")
	}
	if status, _ := res.ExtractionDetails["status"].(string); status != "unsupported_format" {
		t.Fatalf("status=%v want unsupported_format", res.ExtractionDetails["status"])
	}

	scan, err := s.ScanImage(avif, core.ScanOptions{ExtractMessage: true})
	if err != nil {
		t.Fatalf("ScanImage should soft-fail, got err: %v", err)
	}
	if scan.IsStego || scan.Prediction != "unsupported_format" {
		t.Fatalf("scan=%+v", scan)
	}
}

func TestAlphaScannerExtractPNGNoMessage(t *testing.T) {
	s := NewAlphaScanner()
	_ = s.Initialize()

	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	res, err := s.ExtractMessage(buf.Bytes(), "")
	if err != nil {
		t.Fatal(err)
	}
	if res.MessageFound {
		t.Fatal("expected no message")
	}
}

func TestAlphaScannerExtractRealBlockAVIF(t *testing.T) {
	data, err := os.ReadFile("/tmp/block146754.avif")
	if err != nil {
		t.Skip(err)
	}
	s := NewAlphaScanner()
	_ = s.Initialize()
	res, err := s.ExtractMessage(data, "auto")
	if err != nil {
		t.Fatalf("real AVIF must not hard-error: %v", err)
	}
	if res.MessageFound {
		t.Fatal("unexpected message")
	}
	if status, _ := res.ExtractionDetails["status"].(string); status != "unsupported_format" {
		t.Fatalf("status=%v", res.ExtractionDetails)
	}
}
