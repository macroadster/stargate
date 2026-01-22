package bitcoin

import (
	"testing"
)

func TestSanitizeInscriptionsForDisk_SVG(t *testing.T) {
	// Setup test data
	svgContent := `<svg xmlns="http://www.w3.org/2000/svg"><text>Hello</text></svg>`
	
	// Add binary garbage at the start (simulating pushdata or other noise)
	// The SVG cleanup logic should remove this by finding the first '<'.
	// The generic image logic will NOT remove this because it's not a known image signature wrapper.
	garbage := string([]byte{0x04, 0xDE, 0xAD, 0xBE, 0xEF})
	fullContent := garbage + svgContent
	
inscriptions := []InscriptionData{
		{
			TxID:        "test_tx",
			ContentType: "image/svg+xml",
			Content:     fullContent,
			FileName:    "test.svg",
		},
	}

	// Run sanitization
	cleaned := sanitizeInscriptionsForDisk(inscriptions)

	// Check results
	if len(cleaned) != 1 {
		t.Fatalf("Expected 1 inscription, got %d", len(cleaned))
	}

	result := cleaned[0].Content
	
	// If bug exists (SVG cleanup skipped), result will still contain garbage.
	// If fixed, result should be cleaned (starting with <).
	
	if result != svgContent {
		t.Errorf("SVG content was NOT cleaned up.\nExpected: %s\nGot (hex): %x", svgContent, result)
	}
}
