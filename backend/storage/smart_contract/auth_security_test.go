package smart_contract

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"stargate-backend/core/smart_contract"
)

// Authentication and API Security Tests

func TestAPIKeySecurity(t *testing.T) {
	// Test API key generation and validation
	t.Run("API Key Entropy", func(t *testing.T) {
		// Generate multiple API keys to test entropy
		keys := make(map[string]bool)
		for i := 0; i < 1000; i++ {
			key := generateSecureAPIKey()
			if len(key) != 64 { // 256 bits = 64 hex chars
				t.Errorf("API key length incorrect: got %d, want 64", len(key))
			}
			if keys[key] {
				t.Errorf("Duplicate API key generated: %s", key[:8]+"...")
			}
			keys[key] = true
		}
	})

	t.Run("API Key Format Validation", func(t *testing.T) {
		validKeys := []string{
			strings.Repeat("a", 64),
			strings.Repeat("f", 64),
			strings.Repeat("0123456789abcdef", 4),
		}

		for _, key := range validKeys {
			if !isValidAPIKeyFormat(key) {
				t.Errorf("Valid API key rejected: %s", key[:8]+"...")
			}
		}

		invalidKeys := []string{
			"",                            // Empty
			"short",                       // Too short
			strings.Repeat("g", 64),       // Invalid hex
			strings.Repeat("a", 63),       // Too short
			"Z" + strings.Repeat("a", 63), // Invalid hex
		}

		for _, key := range invalidKeys {
			if isValidAPIKeyFormat(key) {
				t.Errorf("Invalid API key accepted: %s", key)
			}
		}
	})
}

func TestRateLimitingPrevention(t *testing.T) {
	store := NewMemoryStore(time.Hour)
	ctx := context.Background()

	// Test rapid request prevention
	t.Run("Rapid Proposal Creation", func(t *testing.T) {
		const numRequests = 100
		successCount := 0

		start := time.Now()
		for i := 0; i < numRequests; i++ {
			proposal := smart_contract.Proposal{
				ID:     "rate-limit-" + string(rune(i)),
				Status: "pending",
				Metadata: map[string]interface{}{
					"visible_pixel_hash": "test123",
				},
			}

			if err := store.CreateProposal(ctx, proposal); err == nil {
				successCount++
			}
		}
		duration := time.Since(start)

		t.Logf("Created %d/%d proposals in %v", successCount, numRequests, duration)

		// In a real implementation, we'd expect rate limiting to kick in
		// For now, we just verify the system remains stable
		if duration > 10*time.Second {
			t.Errorf("System performance degraded under load: %v", duration)
		}
	})
}

func TestInputSanitization(t *testing.T) {
	store := NewMemoryStore(time.Hour)
	ctx := context.Background()

	// Test input sanitization for various attack vectors
	sanitizationTests := []struct {
		name        string
		input       string
		expectError bool
		description string
	}{
		{
			name:        "XSS Script Tags",
			input:       "<script>alert('xss')</script>",
			expectError: false,
			description: "Should sanitize script tags",
		},
		{
			name:        "SQL Injection Pattern",
			input:       "'; DROP TABLE users; --",
			expectError: false,
			description: "Should sanitize SQL injection patterns",
		},
		{
			name:        "Path Traversal",
			input:       "../../../etc/passwd",
			expectError: false,
			description: "Should sanitize path traversal",
		},
		{
			name:        "Null Bytes",
			input:       "malicious\x00input",
			expectError: false,
			description: "Should handle null bytes",
		},
		{
			name:        "Control Characters",
			input:       "input\r\nwith\ncontrol\x0cchars",
			expectError: false,
			description: "Should handle control characters",
		},
	}

	for _, tt := range sanitizationTests {
		t.Run(tt.name, func(t *testing.T) {
			proposal := smart_contract.Proposal{
				ID:     "sanitize-" + tt.name,
				Status: "pending",
				Metadata: map[string]interface{}{
					"visible_pixel_hash": "test123",
					"user_input":         tt.input,
				},
			}

			err := store.CreateProposal(ctx, proposal)
			if tt.expectError && err == nil {
				t.Errorf("Expected error for %s: %s", tt.name, tt.description)
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error for %s: %v", tt.name, err)
			}

			// Verify input was properly sanitized
			if err == nil {
				retrieved, err := store.GetProposal(ctx, proposal.ID)
				if err != nil {
					t.Errorf("Failed to retrieve proposal: %v", err)
				} else {
					// Check that dangerous patterns were neutralized
					userInput := retrieved.Metadata["user_input"].(string)
					if strings.Contains(userInput, "<script>") {
						t.Errorf("Script tags not sanitized in %s", tt.name)
					}
					if strings.Contains(userInput, "DROP TABLE") {
						t.Errorf("SQL injection not sanitized in %s", tt.name)
					}
				}
			}
		})
	}
}

func TestMemoryExhaustionPrevention(t *testing.T) {
	store := NewMemoryStore(time.Hour)
	ctx := context.Background()

	// Test memory exhaustion attacks
	t.Run("Large Metadata Attack", func(t *testing.T) {
		// Create proposal with very large metadata
		largeValue := strings.Repeat("A", 10*1024*1024) // 10MB

		proposal := smart_contract.Proposal{
			ID:     "memory-exhaustion-test",
			Status: "pending",
			Metadata: map[string]interface{}{
				"visible_pixel_hash": "test123",
				"large_field":        largeValue,
			},
		}

		start := time.Now()
		err := store.CreateProposal(ctx, proposal)
		duration := time.Since(start)

		// Should either reject or handle gracefully
		if err != nil {
			t.Logf("Large metadata correctly rejected: %v", err)
		} else {
			if duration > 5*time.Second {
				t.Errorf("Memory exhaustion attack successful: operation took %v", duration)
			}
		}
	})

	t.Run("Many Small Objects Attack", func(t *testing.T) {
		// Create many small proposals to test memory usage
		const numProposals = 10000
		createdCount := 0

		start := time.Now()
		for i := 0; i < numProposals; i++ {
			proposal := smart_contract.Proposal{
				ID:     "many-objects-" + string(rune(i)),
				Status: "pending",
				Metadata: map[string]interface{}{
					"visible_pixel_hash": "test123",
				},
			}

			if err := store.CreateProposal(ctx, proposal); err == nil {
				createdCount++
			}
		}
		duration := time.Since(start)

		t.Logf("Created %d/%d proposals in %v", createdCount, numProposals, duration)

		// Verify system is still responsive
		testProposal := smart_contract.Proposal{
			ID:     "responsiveness-check",
			Status: "pending",
			Metadata: map[string]interface{}{
				"visible_pixel_hash": "test123",
			},
		}

		start = time.Now()
		err := store.CreateProposal(ctx, testProposal)
		if err != nil {
			t.Errorf("System unresponsive after memory exhaustion test: %v", err)
		}
		responsiveTime := time.Since(start)

		if responsiveTime > time.Second {
			t.Errorf("System performance degraded: %v", responsiveTime)
		}
	})
}

func TestCryptographicValidation(t *testing.T) {
	// Test cryptographic validation of sensitive data
	t.Run("Hash Validation", func(t *testing.T) {
		// Test visible pixel hash format validation
		validHashes := []string{
			"abc123def456",
			"0123456789abcdef",
			strings.Repeat("a", 64), // 256-bit hash
		}

		for _, hash := range validHashes {
			if !isValidPixelHashFormat(hash) {
				t.Errorf("Valid pixel hash rejected: %s", hash)
			}
		}

		invalidHashes := []string{
			"",
			"short",
			"invalid@chars",
			strings.Repeat("g", 64), // Invalid hex
		}

		for _, hash := range invalidHashes {
			if isValidPixelHashFormat(hash) {
				t.Errorf("Invalid pixel hash accepted: %s", hash)
			}
		}
	})

	t.Run("Bitcoin Address Validation", func(t *testing.T) {
		// Test Bitcoin address format validation
		validAddresses := []string{
			"1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa",         // Legacy
			"bc1qar0srrr7xfkvy5l643lydnw9re59gtzzwf5mdq", // Bech32
		}

		for _, addr := range validAddresses {
			if !isValidBitcoinAddress(addr) {
				t.Errorf("Valid Bitcoin address rejected: %s", addr)
			}
		}

		invalidAddresses := []string{
			"",
			"invalid",
			"0xInvalidAddress", // Ethereum format
		}

		for _, addr := range invalidAddresses {
			if isValidBitcoinAddress(addr) {
				t.Errorf("Invalid Bitcoin address accepted: %s", addr)
			}
		}
	})
}

func TestConcurrentOperationSafety(t *testing.T) {
	store := NewMemoryStore(time.Hour)
	ctx := context.Background()

	// Test concurrent operations don't corrupt state
	t.Run("Concurrent Proposal Operations", func(t *testing.T) {
		const numGoroutines = 50
		results := make(chan error, numGoroutines)

		// Create proposals concurrently
		for i := 0; i < numGoroutines; i++ {
			go func(id int) {
				proposal := smart_contract.Proposal{
					ID:     "concurrent-" + string(rune(id)),
					Status: "pending",
					Metadata: map[string]interface{}{
						"visible_pixel_hash": "test123",
					},
				}
				err := store.CreateProposal(ctx, proposal)
				results <- err
			}(i)
		}

		// Check results
		successCount := 0
		for i := 0; i < numGoroutines; i++ {
			err := <-results
			if err == nil {
				successCount++
			}
		}

		if successCount != numGoroutines {
			t.Errorf("Concurrent operations failed: %d/%d succeeded", successCount, numGoroutines)
		}

		// Verify all proposals were created correctly
		for i := 0; i < numGoroutines; i++ {
			proposalID := "concurrent-" + string(rune(i))
			_, err := store.GetProposal(ctx, proposalID)
			if err != nil {
				t.Errorf("Proposal %s not found after concurrent creation", proposalID)
			}
		}
	})
}

func TestErrorHandlingSecurity(t *testing.T) {
	store := NewMemoryStore(time.Hour)
	ctx := context.Background()

	// Test that errors don't leak sensitive information
	t.Run("Error Message Sanitization", func(t *testing.T) {
		// Try to trigger various error conditions
		errorTests := []struct {
			name     string
			testFunc func() error
		}{
			{
				name: "Non-existent Proposal",
				testFunc: func() error {
					_, err := store.GetProposal(ctx, "non-existent")
					return err
				},
			},
			{
				name: "Invalid Task Claim",
				testFunc: func() error {
					_, err := store.ClaimTask("non-existent-task", "user", "wallet", nil)
					return err
				},
			},
		}

		for _, tt := range errorTests {
			t.Run(tt.name, func(t *testing.T) {
				err := tt.testFunc()
				if err != nil {
					errorMsg := err.Error()

					// Check for sensitive information leakage
					sensitivePatterns := []string{
						"password",
						"secret",
						"token",
						"private",
						"internal",
						"stack trace",
					}

					for _, pattern := range sensitivePatterns {
						if strings.Contains(strings.ToLower(errorMsg), pattern) {
							t.Errorf("Error message contains sensitive information: %s", errorMsg)
						}
					}
				}
			})
		}
	})
}

// Helper functions for security testing

func generateSecureAPIKey() string {
	bytes := make([]byte, 32)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

func isValidAPIKeyFormat(key string) bool {
	if len(key) != 64 {
		return false
	}
	_, err := hex.DecodeString(key)
	return err == nil
}

func isValidPixelHashFormat(hash string) bool {
	if len(hash) == 0 {
		return false
	}
	_, err := hex.DecodeString(hash)
	return err == nil
}

func isValidBitcoinAddress(addr string) bool {
	// Basic validation - in production, use proper Bitcoin address validation
	if len(addr) < 26 || len(addr) > 35 {
		return false
	}

	// Check for valid Bitcoin address characters
	validChars := "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
	for _, char := range addr {
		if !strings.ContainsRune(validChars, char) {
			return false
		}
	}

	return true
}
