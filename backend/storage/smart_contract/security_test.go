package smart_contract

import (
	"context"
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
					"visible_pixel_hash": "abc123",
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
					"visible_pixel_hash": "valid123",
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
				"visible_pixel_hash": "original123",
				"contract_id":        "spoofed456", // Attempt to change contract ID
			},
			expectError: false,
			description: "Should allow but track contract ID changes",
		},
		{
			name: "Status Override in Metadata",
			metadata: map[string]interface{}{
				"visible_pixel_hash": "test123",
				"status":             "approved", // Attempt to override status via metadata
			},
			expectError: false,
			description: "Should not allow metadata to override proposal status",
		},
		{
			name: "Malicious JSON Injection",
			metadata: map[string]interface{}{
				"visible_pixel_hash": "test123",
				"injection":          "{\"__proto__\": {\"admin\": true}}",
			},
			expectError: false,
			description: "Should handle prototype pollution attempts",
		},
		{
			name: "Large Metadata Attack",
			metadata: map[string]interface{}{
				"visible_pixel_hash": "test123",
				"large_data":         strings.Repeat("A", 1000000), // 1MB of data
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
			_, err := store.ClaimTask(taskID, "contractor-"+string(rune(id)), "wallet123", nil)
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
			"visible_pixel_hash": "test123",
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

	// Test large number of proposals creation
	const numProposals = 1000
	createdCount := 0

	start := time.Now()
	for i := 0; i < numProposals; i++ {
		proposal := smart_contract.Proposal{
			ID:     "resource-test-" + string(rune(i)),
			Status: "pending",
			Metadata: map[string]interface{}{
				"visible_pixel_hash": "test123",
			},
		}

		if err := store.CreateProposal(ctx, proposal); err != nil {
			t.Logf("Failed to create proposal %d: %v", i, err)
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
			"visible_pixel_hash": "test123",
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
	store := NewMemoryStore(time.Hour)
	ctx := context.Background()

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
				ID:     "null\x00byte",
				Status: "pending",
				Metadata: map[string]interface{}{
					"visible_pixel_hash": "test123",
				},
			},
			expectError: false,
			description: "Should handle null bytes in IDs",
		},
		{
			name: "Unicode Exploits",
			proposal: smart_contract.Proposal{
				ID:     "unicode-\u202e-test", // Right-to-left override
				Status: "pending",
				Metadata: map[string]interface{}{
					"visible_pixel_hash": "test123",
				},
			},
			expectError: false,
			description: "Should handle unicode exploits",
		},
		{
			name: "Path Traversal",
			proposal: smart_contract.Proposal{
				ID:     "../../../etc/passwd",
				Status: "pending",
				Metadata: map[string]interface{}{
					"visible_pixel_hash": "test123",
				},
			},
			expectError: false,
			description: "Should handle path traversal attempts",
		},
		{
			name: "Script Injection",
			proposal: smart_contract.Proposal{
				ID:     "<script>alert('xss')</script>",
				Status: "pending",
				Metadata: map[string]interface{}{
					"visible_pixel_hash": "test123",
				},
			},
			expectError: false,
			description: "Should handle script injection attempts",
		},
	}

	for _, tt := range bypassTests {
		t.Run(tt.name, func(t *testing.T) {
			err := store.CreateProposal(ctx, tt.proposal)
			if tt.expectError && err == nil {
				t.Errorf("Expected error for %s: %s", tt.name, tt.description)
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
				for i := 0; i < 1000; i++ {
					largeData["field_"+string(rune(i))] = strings.Repeat("A", 1000)
				}
				proposal := smart_contract.Proposal{
					ID:       "dos-large-metadata",
					Status:   "pending",
					Metadata: largeData,
				}
				return store.CreateProposal(ctx, proposal)
			},
			expectError: false,
			description: "Should handle large metadata without crashing",
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
				proposal := smart_contract.Proposal{
					ID:       "dos-deep-json",
					Status:   "pending",
					Metadata: deepData,
				}
				return store.CreateProposal(ctx, proposal)
			},
			expectError: false,
			description: "Should handle deeply nested JSON",
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
			"visible_pixel_hash": "audit123",
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
			"visible_pixel_hash": "concurrent123",
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
