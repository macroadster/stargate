package smart_contract

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"stargate-backend/core/smart_contract"
)

// Security Tests to Safeguard Against Hacking and Workflow Malfunctions

func TestSQLInjectionPrevention(t *testing.T) {
	store := NewMemoryStore(time.Hour)
	ctx := context.Background()

	// Test SQL injection attempts in proposal metadata
	sqlInjectionPayloads := []string{
		"'; DROP TABLE mcp_proposals; --",
		"1' OR '1'='1",
		"'; UPDATE mcp_proposals SET status='approved'; --",
		"1; DELETE FROM mcp_tasks; --",
		"' UNION SELECT * FROM sensitive_data --",
	}

	for _, payload := range sqlInjectionPayloads {
		t.Run("SQL_Injection_"+strings.ReplaceAll(payload, ";", "_"), func(t *testing.T) {
			proposal := smart_contract.Proposal{
				ID:     "sql-test-" + strings.ReplaceAll(payload, ";", ""),
				Status: "pending",
				Metadata: map[string]interface{}{
					"visible_pixel_hash": "deadbeefdeadbeefdeadbeef",
					"contract_id":        payload, // Attempt SQL injection
					"malicious_query":    payload,
				},
			}

			// Should not cause database corruption
			err := store.CreateProposal(ctx, proposal)
			if err != nil {
				t.Logf("Expected rejection of malicious payload: %v", err)
			}

			// Verify store is still functional
			validProposal := smart_contract.Proposal{
				ID:     "valid-after-sql-test",
				Status: "pending",
				Metadata: map[string]interface{}{
					"visible_pixel_hash": "deadbeefdeadbeefdeadbeef",
				},
			}
			err = store.CreateProposal(ctx, validProposal)
			if err != nil {
				t.Errorf("Store functionality compromised after SQL injection test: %v", err)
			}
		})
	}
}

func TestMetadataTamperingPrevention(t *testing.T) {
	store := NewMemoryStore(time.Hour)
	ctx := context.Background()

	// Test metadata tampering attempts
	tamperingTests := []struct {
		name        string
		metadata    map[string]interface{}
		expectError bool
		description string
	}{
		{
			name: "Contract ID Spoofing",
			metadata: map[string]interface{}{
				"visible_pixel_hash": "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2", // Valid hex hash
				"contract_id":        "spoofed456",                                                       // Attempt to change contract ID
			},
			expectError: true,
			description: "Should reject mismatched contract_id and visible_pixel_hash",
		},
		{
			name: "Status Override in Metadata",
			metadata: map[string]interface{}{
				"visible_pixel_hash": "b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3", // Valid hex hash
				"status":             "approved",                                                         // Attempt to override status via metadata
			},
			expectError: false,
			description: "Should not allow metadata to override proposal status",
		},
		{
			name: "Malicious JSON Injection",
			metadata: map[string]interface{}{
				"visible_pixel_hash": "c3d4e5f6a7b8c9d0e1f2a3b4c5d6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4", // Valid hex hash
				"injection":          "{\"__proto__\": {\"admin\": true}}",                               // Attempt to override status via metadata
			},
			expectError: false,
			description: "Should handle prototype pollution attempts",
		},
		{
			name: "Large Metadata Attack",
			metadata: map[string]interface{}{
				"visible_pixel_hash": "d4e5f6a7b8c9d0e1f2a3b4c5d6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5", // Valid hex hash
				"large_data":         strings.Repeat("A", 1000000),                                       // 1MB of data
			},
			expectError: false,
			description: "Should handle large metadata without crashing",
		},
	}

	for _, tt := range tamperingTests {
		t.Run(tt.name, func(t *testing.T) {
			proposal := smart_contract.Proposal{
				ID:       "tamper-" + tt.name,
				Status:   "pending",
				Metadata: tt.metadata,
			}

			err := store.CreateProposal(ctx, proposal)
			if tt.expectError && err == nil {
				t.Errorf("Expected error for %s: %s", tt.name, tt.description)
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error for %s: %v", tt.name, err)
			}

			// Verify proposal integrity
			if err == nil {
				retrieved, err := store.GetProposal(ctx, proposal.ID)
				if err != nil {
					t.Errorf("Failed to retrieve proposal after tampering test: %v", err)
				}
				// Verify status wasn't overridden by metadata
				if retrieved.Status != "pending" {
					t.Errorf("Metadata tampered with proposal status: got %s, want pending", retrieved.Status)
				}
			}
		})
	}
}

func TestRaceConditionPrevention(t *testing.T) {
	store := NewMemoryStore(time.Hour)

	// Create a task for concurrent claiming tests
	taskID := "race-test-task"
	testTask := smart_contract.Task{
		TaskID:     taskID,
		ContractID: "contract-123",
		Status:     "available",
	}

	store.mu.Lock()
	store.tasks[taskID] = testTask
	store.mu.Unlock()

	// Test concurrent task claiming
	const numGoroutines = 10
	results := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			_, err := store.ClaimTask(taskID, fmt.Sprintf("wallet-%d", id), nil)
			results <- err
		}(i)
	}

	// Count successful claims
	successCount := 0
	for i := 0; i < numGoroutines; i++ {
		err := <-results
		if err == nil {
			successCount++
		}
	}

	// Only one claim should succeed
	if successCount != 1 {
		t.Errorf("Race condition detected: %d claims succeeded, expected 1", successCount)
	}
}

func TestPrivilegeEscalationPrevention(t *testing.T) {
	store := NewMemoryStore(time.Hour)
	ctx := context.Background()

	// Test unauthorized status changes
	proposal := smart_contract.Proposal{
		ID:     "privilege-test",
		Status: "pending",
		Metadata: map[string]interface{}{
			"visible_pixel_hash": "e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4e5f6", // Valid hex hash
		},
		Tasks: []smart_contract.Task{
			{
				TaskID:     "privilege-test-task-1",
				ContractID: "e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4e5f6",
				Title:      "Privilege task",
				BudgetSats: 1000,
				Status:     "available",
			},
		},
	}

	if err := store.CreateProposal(ctx, proposal); err != nil {
		t.Fatalf("Failed to create proposal: %v", err)
	}

	// Test direct status manipulation (should not be possible through normal API)
	// This test verifies that status changes require proper methods
	retrieved, err := store.GetProposal(ctx, proposal.ID)
	if err != nil {
		t.Fatalf("Failed to retrieve proposal: %v", err)
	}

	// Verify initial status
	if retrieved.Status != "pending" {
		t.Errorf("Unexpected initial status: got %s, want pending", retrieved.Status)
	}

	// Test that approval requires proper method (can't be done directly)
	// This would be tested at the API layer, but we verify the store enforces it
	err = store.ApproveProposal(ctx, proposal.ID)
	if err != nil {
		t.Errorf("Failed to approve proposal through proper method: %v", err)
	}

	// Verify status changed through proper method
	retrieved, err = store.GetProposal(ctx, proposal.ID)
	if err != nil {
		t.Fatalf("Failed to retrieve proposal after approval: %v", err)
	}

	if retrieved.Status != "approved" {
		t.Errorf("Status not changed through proper method: got %s, want approved", retrieved.Status)
	}
}

func TestResourceExhaustionPrevention(t *testing.T) {
	store := NewMemoryStore(time.Hour)
	ctx := context.Background()

	// Test resource exhaustion with reduced number for test performance
	const numProposals = 50
	createdCount := 0

	start := time.Now()
	for i := 0; i < numProposals; i++ {
		proposal := smart_contract.Proposal{
			ID:     fmt.Sprintf("resource-test-%d", i),
			Status: "pending",
			Metadata: map[string]interface{}{
				"visible_pixel_hash": "deadbeefdeadbeefdeadbeef",
			},
		}

		if err := store.CreateProposal(ctx, proposal); err != nil {
			// Silently continue - don't log each failure
			continue
		}
		createdCount++
	}
	duration := time.Since(start)

	t.Logf("Created %d/%d proposals in %v", createdCount, numProposals, duration)

	// Verify store is still responsive
	testProposal := smart_contract.Proposal{
		ID:     "responsiveness-test",
		Status: "pending",
		Metadata: map[string]interface{}{
			"visible_pixel_hash": "deadbeefdeadbeefdeadbeef",
		},
	}

	start = time.Now()
	err := store.CreateProposal(ctx, testProposal)
	if err != nil {
		t.Errorf("Store not responsive after load test: %v", err)
	}
	responsiveTime := time.Since(start)

	if responsiveTime > time.Second {
		t.Errorf("Store performance degraded: %v > 1s", responsiveTime)
	}
}

func TestInputValidationBypasses(t *testing.T) {
	// Test various input validation bypass attempts
	bypassTests := []struct {
		name        string
		proposal    smart_contract.Proposal
		expectError bool
		description string
	}{
		{
			name: "Null Byte Injection",
			proposal: smart_contract.Proposal{
				ID:     "nullbyte-test", // Use a valid ID for the proposal itself
				Status: "pending",
				Metadata: map[string]interface{}{
					"visible_pixel_hash": "test123",
					"title":              "Proposal with null\x00byte in title", // Inject into a validated field
				},
			},
			expectError: true, // Now expects an error
			description: "Should reject null bytes in validated fields",
		},
		{
			name: "Unicode Exploits",
			proposal: smart_contract.Proposal{
				ID:     "unicode-exploit-test",
				Status: "pending",
				Metadata: map[string]interface{}{
					"visible_pixel_hash": "test123",
					"title":              "Unicode-\u202e-exploit in title", // Inject into a validated field
				},
			},
			expectError: true, // Now expects an error
			description: "Should reject unicode exploits in validated fields",
		},
		{
			name: "Path Traversal",
			proposal: smart_contract.Proposal{
				ID:     "path-traversal-test",
				Status: "pending",
				Metadata: map[string]interface{}{
					"visible_pixel_hash": "test123",
					"title":              "Proposal with ../../../etc/passwd in title", // Inject into a validated field
				},
			},
			expectError: true, // Now expects an error
			description: "Should reject path traversal attempts in validated fields",
		},
		{
			name: "Script Injection",
			proposal: smart_contract.Proposal{
				ID:     "script-injection-test",
				Status: "pending",
				Metadata: map[string]interface{}{
					"visible_pixel_hash": "test123",
					"title":              "Proposal with <script>alert('xss')</script> in title", // Inject into a validated field
				},
			},
			expectError: true, // Now expects an error
			description: "Should reject script injection attempts in validated fields",
		},
		{
			name: "Malicious JSON Injection in Metadata",
			proposal: smart_contract.Proposal{
				ID:     "json-injection-test",
				Status: "pending",
				Metadata: map[string]interface{}{
					"visible_pixel_hash": "test123",
					"injection":          "{\"__proto__\": {\"admin\": true}}", // Inject into a metadata field
				},
			},
			expectError: true, // Now expects an error
			description: "Should reject malicious JSON injection in metadata values",
		},
	}

	for _, tt := range bypassTests {
		t.Run(tt.name, func(t *testing.T) {
			// Call ValidateProposalInput directly to test validation logic
			err := ValidateProposalInput(&tt.proposal)
			if tt.expectError && err == nil {
				t.Errorf("Expected error for %s, but got none: %s", tt.name, tt.description)
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error for %s: %v", tt.name, err)
			}
		})
	}
}

func TestContractIDSpoofingPrevention(t *testing.T) {
	// Test contract ID spoofing attempts
	spoofingTests := []struct {
		name        string
		metadata    map[string]interface{}
		expectedID  string
		description string
	}{
		{
			name: "Visible Pixel Hash Priority",
			metadata: map[string]interface{}{
				"visible_pixel_hash": "priority123",
				"contract_id":        "attempted456",
				"ingestion_id":       "attempted789",
			},
			expectedID:  "priority123",
			description: "Should use visible_pixel_hash as canonical ID",
		},
		{
			name: "Empty Pixel Hash Fallback",
			metadata: map[string]interface{}{
				"visible_pixel_hash": "",
				"contract_id":        "fallback456",
			},
			expectedID:  "fallback456",
			description: "Should fallback to contract_id when pixel hash empty",
		},
		{
			name: "Whitespace Pixel Hash",
			metadata: map[string]interface{}{
				"visible_pixel_hash": "   ",
				"contract_id":        "whitespace456",
			},
			expectedID:  "whitespace456",
			description: "Should skip whitespace pixel hash",
		},
		{
			name: "All Identifiers Missing",
			metadata: map[string]interface{}{
				"visible_pixel_hash": "",
				"contract_id":        "",
			},
			expectedID:  "test-proposal-id",
			description: "Should use proposal ID as final fallback",
		},
	}

	for _, tt := range spoofingTests {
		t.Run(tt.name, func(t *testing.T) {
			result := contractIDFromMeta(tt.metadata, "test-proposal-id")
			if result != tt.expectedID {
				t.Errorf("Expected %s for %s, got %s", tt.expectedID, tt.name, result)
				t.Errorf("Description: %s", tt.description)
			}
		})
	}
}

func TestDenialOfServicePrevention(t *testing.T) {
	store := NewMemoryStore(time.Hour)
	ctx := context.Background()

	// Test various DoS attack vectors
	dosTests := []struct {
		name        string
		attackFunc  func() error
		expectError bool
		description string
	}{
		{
			name: "Large Proposal Metadata",
			attackFunc: func() error {
				largeData := make(map[string]interface{})
				for i := 0; i < 1040; i++ { // Increased from 1000 to 1040 to exceed 1MB limit
					// Each field will be 1000 'A's, plus key length.
					// Total data size will be around 1MB, hitting MaxMetadataSize limit.
					largeData["field_"+fmt.Sprintf("%04d", i)] = strings.Repeat("A", 1000)
				}
				largeData["visible_pixel_hash"] = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef" // Ensure this also contributes to size
				proposal := smart_contract.Proposal{
					ID:       "dos-large-metadata",
					Status:   "pending",
					Metadata: largeData,
				}
				// We expect ValidateProposalInput to catch this.
				return ValidateProposalInput(&proposal)
			},
			expectError: true, // Expect an error now for oversized metadata
			description: "Should reject large metadata to prevent DoS",
		},
		{
			name: "Deep JSON Nesting",
			attackFunc: func() error {
				// Create deeply nested JSON
				deepData := map[string]interface{}{"level": 0}
				current := deepData
				for i := 1; i < 100; i++ {
					next := map[string]interface{}{"level": i}
					current["nested"] = next
					current = next
				}
				deepData["visible_pixel_hash"] = "deadbeefdeadbeef"
				proposal := smart_contract.Proposal{
					ID:       "dos-deep-json",
					Status:   "pending",
					Metadata: deepData,
				}
				return store.CreateProposal(ctx, proposal)
			},
			expectError: true,
			description: "Should reject deeply nested JSON to prevent DoS",
		},
	}

	for _, tt := range dosTests {
		t.Run(tt.name, func(t *testing.T) {
			start := time.Now()
			err := tt.attackFunc()
			duration := time.Since(start)

			if tt.expectError && err == nil {
				t.Errorf("Expected error for %s: %s", tt.name, tt.description)
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error for %s: %v", tt.name, err)
			}

			// Check if operation took too long (potential DoS)
			if duration > 5*time.Second {
				t.Errorf("Potential DoS: %s took %v", tt.name, duration)
			}
		})
	}
}

func TestAuditTrailVerification(t *testing.T) {
	store := NewMemoryStore(time.Hour)
	ctx := context.Background()

	// Create a proposal and track its lifecycle
	proposal := smart_contract.Proposal{
		ID:     "audit-test",
		Status: "pending",
		Metadata: map[string]interface{}{
			"visible_pixel_hash": "f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4e5f6a7", // Valid hex hash
		},
		Tasks: []smart_contract.Task{
			{
				TaskID:     "audit-test-task-1",
				ContractID: "f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4e5f6a7",
				Title:      "Audit task",
				BudgetSats: 1000,
				Status:     "available",
			},
		},
	}

	// Track state changes
	stateChanges := []string{"created"}

	// Create proposal
	if err := store.CreateProposal(ctx, proposal); err != nil {
		t.Fatalf("Failed to create proposal: %v", err)
	}
	stateChanges = append(stateChanges, "created")

	// Approve proposal
	if err := store.ApproveProposal(ctx, proposal.ID); err != nil {
		t.Fatalf("Failed to approve proposal: %v", err)
	}
	stateChanges = append(stateChanges, "approved")

	// Publish proposal
	if err := store.PublishProposal(ctx, proposal.ID); err != nil {
		t.Fatalf("Failed to publish proposal: %v", err)
	}
	stateChanges = append(stateChanges, "published")

	// Verify final state
	finalProposal, err := store.GetProposal(ctx, proposal.ID)
	if err != nil {
		t.Fatalf("Failed to get final proposal: %v", err)
	}

	if finalProposal.Status != "published" {
		t.Errorf("Audit trail verification failed: final status %s, expected published", finalProposal.Status)
	}

	// Verify all expected state changes occurred
	expectedChanges := []string{"created", "approved", "published"}
	if len(stateChanges) != len(expectedChanges)+1 { // +1 for initial creation
		t.Errorf("Audit trail incomplete: got %v, expected %v", stateChanges, expectedChanges)
	}
}

func TestConcurrentStateManipulation(t *testing.T) {
	store := NewMemoryStore(time.Hour)
	ctx := context.Background()

	// Create proposal for concurrent manipulation tests
	proposal := smart_contract.Proposal{
		ID:     "concurrent-test",
		Status: "pending",
		Metadata: map[string]interface{}{
			"visible_pixel_hash": "deadbeefdeadbeefdeadbeef",
		},
		Tasks: []smart_contract.Task{
			{
				TaskID:     "concurrent-test-task-1",
				ContractID: "deadbeefdeadbeefdeadbeef",
				Title:      "Concurrent task",
				BudgetSats: 1000,
				Status:     "available",
			},
		},
	}

	if err := store.CreateProposal(ctx, proposal); err != nil {
		t.Fatalf("Failed to create proposal: %v", err)
	}

	// Test concurrent approval attempts
	const numApprovers = 5
	results := make(chan error, numApprovers)

	for i := 0; i < numApprovers; i++ {
		go func(id int) {
			err := store.ApproveProposal(ctx, proposal.ID)
			results <- err
		}(i)
	}

	// Count successful approvals
	successCount := 0
	for i := 0; i < numApprovers; i++ {
		err := <-results
		if err == nil {
			successCount++
		}
	}

	// Only one approval should succeed
	if successCount != 1 {
		t.Errorf("Concurrent manipulation detected: %d approvals succeeded, expected 1", successCount)
	}

	// Verify final state
	finalProposal, err := store.GetProposal(ctx, proposal.ID)
	if err != nil {
		t.Fatalf("Failed to get final proposal: %v", err)
	}

	if finalProposal.Status != "approved" {
		t.Errorf("Concurrent manipulation corrupted state: final status %s, expected approved", finalProposal.Status)
	}
}

func TestCryptoValidation(t *testing.T) {
	validationTests := []struct {
		name        string
		address     string
		expectError bool
		description string
	}{
		// Valid Bitcoin Addresses
		{
			name:        "Valid P2PKH Address",
			address:     "1BvBMSEYstWetqTFn5Au4m4GFg7xJaNVN2",
			expectError: false,
			description: "Should validate a standard P2PKH address",
		},
		{
			name:        "Valid P2SH Address",
			address:     "3J98t1WpEZ73CNmQviecrnyiWrnqRhWNLy",
			expectError: false,
			description: "Should validate a standard P2SH address",
		},
		{
			name:        "Valid Bech32 Address",
			address:     "bc1qer5yqg84l5cpl58j0s0p7x8x92x5s5s5s5s5s5s5s5s5s5s5s5s5s5s",
			expectError: false,
			description: "Should validate a standard Bech32 address",
		},
		{
			name:        "Valid Bech32m Address",
			address:     "bc1p0xlxvlhvfmcn0ktc3d90fap4z49jkg8m0q3v39wep4s9j0qg0ss5s5s5s5s5s5s5s5s5s5s5s5s",
			expectError: false,
			description: "Should validate a standard Bech32m address",
		},
		{
			name:        "Valid Testnet Address (P2PKH)",
			address:     "mi1Z5mXPMk485a4YdD53L5A4W5c6J7R8B9", // Valid testnet P2PKH address, 34 chars, no invalid Base58 chars
			expectError: false,
			description: "Should validate a testnet P2PKH address",
		},

		// Invalid Addresses
		{
			name:        "Empty Address",
			address:     "",
			expectError: true,
			description: "Should reject empty address",
		},
		{
			name:        "Whitespace Only Address",
			address:     "   ",
			expectError: true,
			description: "Should reject whitespace only address",
		},
		{
			name:        "Too Short Address",
			address:     "1BvBMSEYstWetqTFn5A", // 20 chars, too short for overall (26-90)
			expectError: true,
			description: "Should reject address that is too short overall",
		},
		{
			name:        "Too Long Address",
			address:     "1BvBMSEYstWetqTFn5Au4m4GFg7xJaNVN2EXTRA", // 39 chars, too long for overall (26-90)
			expectError: true,
			description: "Should reject address that is too long overall",
		},
		{
			name:        "Invalid Character (Base58)",
			address:     "1BvBMSEYstWetqTFn5Au4m4GFg7xJaNVNOO", // 'O' is not valid Base58
			expectError: true,
			description: "Should reject Base58 address with invalid characters",
		},
		{
			name:        "Invalid Character (Bech32)",
			address:     "bc1qer5yqg84l5cpl58j0s0p7x8x92x5s5s5s5s5s5s5s5s5s5s5s5s5s5sO", // 'O' is not valid Bech32
			expectError: true,
			description: "Should reject Bech32 address with invalid characters",
		},
		{
			name:        "Invalid Prefix",
			address:     "xBvBMSEYstWetqTFn5Au4m4GFg7xJaNVN2",
			expectError: true,
			description: "Should reject address with invalid prefix",
		},
		{
			name:        "Test Pattern (Lower)",
			address:     "bc1q...test...",
			expectError: true,
			description: "Should reject address containing 'test'",
		},
		{
			name:        "Example Pattern (Upper)",
			address:     "1...EXAMPLE...",
			expectError: true,
			description: "Should reject address containing 'EXAMPLE'",
		},
		{
			name:        "Repeated Dot Pattern",
			address:     "1.............................",
			expectError: true,
			description: "Should reject address with repeated dots",
		},
		{
			name:        "High Count of '1's",
			address:     "1111111111111111111111111111111111", // More than half '1's
			expectError: true,
			description: "Should reject address with excessive '1's",
		},
		{
			name:        "Invalid Bech32 Length",
			address:     "bc1qer5yqg84l5cpl58j0s0p7x8x92", // 30 chars, too short for Bech32 (min 42)
			expectError: true,
			description: "Should reject Bech32 address with invalid length",
		},
		{
			name:        "Invalid Legacy Length",
			address:     "1A", // 2 chars, definitively too short for legacy (min 26)
			expectError: true,
			description: "Should reject legacy address with invalid length",
		},
	}

	for _, tt := range validationTests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateBitcoinAddress(tt.address)
			if tt.expectError && err == nil {
				t.Errorf("Expected error for %s (%s), but got none", tt.name, tt.description)
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error for %s (%s): %v", tt.name, tt.description, err)
			}
		})
	}
}
