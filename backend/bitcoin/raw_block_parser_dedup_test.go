package bitcoin

import (
	"bytes"
	"testing"
)

func TestDedupImage_SVG_Preservation(t *testing.T) {
	// SVG content containing a JPEG signature (FF D8 FF)
	// If dedupImage treats this as a generic image, it calls trimToImageSignatureLocal,
	// which searches for FF D8 FF and trims everything before it.
	jpegSig := []byte{0xFF, 0xD8, 0xFF}
	svgContent := []byte(`<svg>data="`)
	svgContent = append(svgContent, jpegSig...)
	svgContent = append(svgContent, []byte(`"</svg>`)...) // <svg>data="...JPEG..."</svg>

	img := ExtractedImageData{
		TxID:        "tx1",
		ContentType: "image/svg+xml",
		Format:      "svg",
		Data:        svgContent,
		FileName:    "test.svg",
	}

	seen := make(map[string]bool)
	
	// This should NOT modify img.Data for SVGs
	dedupImage(&img, seen)

	if !bytes.Equal(img.Data, svgContent) {
		t.Errorf("dedupImage corrupted SVG data.\nExpected length: %d\nGot length: %d\nData starts with: %x", 
			len(svgContent), len(img.Data), img.Data[:10])
	}
}
