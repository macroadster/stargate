package stego

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"testing"
)

func TestDecodeImagePNGOK(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	got, format, err := DecodeImage(buf.Bytes())
	if err != nil {
		t.Fatalf("DecodeImage: %v", err)
	}
	if format != "png" {
		t.Fatalf("format=%q want png", format)
	}
	if got.Bounds().Dx() != 8 {
		t.Fatalf("width=%d", got.Bounds().Dx())
	}
}

func TestDecodeImageAVIFUnsupported(t *testing.T) {
	// Minimal ISO BMFF ftypavif header (not a valid image, but sniffer should catch it).
	avif := []byte{
		0x00, 0x00, 0x00, 0x20, // box size
		'f', 't', 'y', 'p',
		'a', 'v', 'i', 'f',
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
	}
	_, _, err := DecodeImage(avif)
	if !IsUnsupportedFormat(err) {
		t.Fatalf("want unsupported, got %v", err)
	}
	if !IsSoftDecodeFailure(err) {
		t.Fatalf("want soft failure, got %v", err)
	}
}

func TestDecodeImageRealAVIFFile(t *testing.T) {
	data, err := os.ReadFile("/tmp/block146754.avif")
	if err != nil {
		t.Skip("sample AVIF not present:", err)
	}
	_, _, err = DecodeImage(data)
	if !IsUnsupportedFormat(err) {
		t.Fatalf("real AVIF should be unsupported soft-fail, got %v", err)
	}
}

func TestDecodeImageEmpty(t *testing.T) {
	_, _, err := DecodeImage(nil)
	if !IsUnsupportedFormat(err) {
		t.Fatalf("want unsupported, got %v", err)
	}
}

func TestDecodeImageTooLargeBytes(t *testing.T) {
	// Build a buffer over MaxDecodeBytes without allocating full max if possible.
	// Use a small oversize slice only when MaxDecodeBytes is manageable in tests.
	// We just check the length gate with a stub that claims size; simulate by
	// temporarily not needing huge alloc — call with length check via helper.
	// Direct path: create slice of MaxDecodeBytes+1 would OOM-ish; skip if huge.
	if MaxDecodeBytes > 32*1024*1024 {
		t.Skip("MaxDecodeBytes too large for unit test allocation")
	}
	// Use a modest override check: empty header path already tested; here
	// construct slightly over-limit with make (10MB+1 is fine in CI).
	data := make([]byte, MaxDecodeBytes+1)
	// Put PNG magic so sniffer does not short-circuit as unsupported format first.
	copy(data, []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a})
	_, _, err := DecodeImage(data)
	if !IsSoftDecodeFailure(err) {
		t.Fatalf("want soft size failure, got %v", err)
	}
}

func TestSniffUnsupportedFormat(t *testing.T) {
	if SniffUnsupportedFormat([]byte("not enough")) != "" {
		t.Fatal("short payload should not sniff")
	}
	avif := make([]byte, 16)
	copy(avif[4:], []byte("ftypavif"))
	if got := SniffUnsupportedFormat(avif); got != "avif" {
		t.Fatalf("got %q", got)
	}
}
