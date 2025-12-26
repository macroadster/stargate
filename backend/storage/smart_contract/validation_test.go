package smart_contract

import (
	"context"
	"testing"
	"time"

	"stargate-backend/core/smart_contract"
)

func TestStatusFieldValidation(t *testing.T) {
	store := NewMemoryStore(time.Hour)
	ctx := context.Background()

	tests := []struct {
		name        string
		proposal    smart_contract.Proposal
		expectError bool
		description string
	}{
		{
			name: "Valid pending status",
			proposal: smart_contract.Proposal{
				ID:     "valid-pending",
				Status: "pending",
				Metadata: map[string]interface{}{
					"visible_pixel_hash": "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2", // Valid 64-char hex
				},
			},
			expectError: false,
			description: "Should accept valid pending status",
		},
		{
			name: "Valid approved status",
			proposal: smart_contract.Proposal{
				ID:     "valid-approved",
				Status: "approved",
				Metadata: map[string]interface{}{
					"visible_pixel_hash": "b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3", // Valid 64-char hex
				},
				Tasks: []smart_contract.Task{
					{
						TaskID:     "task-1",
						BudgetSats: 100,
					},
				},
			},
			expectError: false,
			description: "Should accept valid approved status",
		},
		{
			name: "Empty status defaults to pending",
			proposal: smart_contract.Proposal{
				ID:     "empty-status",
				Status: "",
				Metadata: map[string]interface{}{
					"visible_pixel_hash": "c3d4e5f6a7b8c9d0e1f2a3b4c5d6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4", // Valid 64-char hex
				},
			},
			expectError: false,
			description: "Should default empty status to pending",
		},
		{
			name: "Invalid status rejected",
			proposal: smart_contract.Proposal{
				ID:     "invalid-status",
				Status: "invalid_status",
				Metadata: map[string]interface{}{
					"visible_pixel_hash": "d4e5f6a7b8c9d0e1f2a3b4c5d6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5", // Valid 64-char hex
				},
			},
			expectError: true,
			description: "Should reject invalid status values",
		},
		{
			name: "Null status handling",
			proposal: smart_contract.Proposal{
				ID: "null-status",
				Metadata: map[string]interface{}{
					"visible_pixel_hash": "e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4e5f6", // Valid 64-char hex
				},
			},
			expectError: false,
			description: "Should handle null status gracefully",
		},
	}

	for _, tt := range tests {
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

func TestVisiblePixelHashValidation(t *testing.T) {
	store := NewMemoryStore(time.Hour)
	ctx := context.Background()

	tests := []struct {
		name        string
		proposal    smart_contract.Proposal
		expectError bool
		description string
	}{
		{
			name: "Valid visible_pixel_hash",
			proposal: smart_contract.Proposal{
				ID:     "valid-pixel",
				Status: "pending",
				Metadata: map[string]interface{}{
					"visible_pixel_hash": "f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4e5f6a7", // Valid 64-char hex
				},
			},
			expectError: false,
			description: "Should accept valid visible_pixel_hash",
		},
		{
			name: "Missing visible_pixel_hash and image_scan_data",
			proposal: smart_contract.Proposal{
				ID:     "no-pixel-hash",
				Status: "pending",
				Metadata: map[string]interface{}{
					"contract_id": "contract-123",
					// visible_pixel_hash is intentionally missing
				},
			},
			expectError: true, // Expect error because neither visible_pixel_hash nor image_scan_data is present
			description: "Should reject proposal without pixel hash or scan data",
		},
		{
			name: "Empty visible_pixel_hash with image_scan_data",
			proposal: smart_contract.Proposal{
				ID:     "empty-pixel-with-scan",
				Status: "pending",
				Metadata: map[string]interface{}{
					"visible_pixel_hash": "", // Empty but image_scan_data is present
					"image_scan_data": map[string]interface{}{
						"scan_result": "success",
					},
				},
			},
			expectError: false,
			description: "Should accept empty pixel hash when scan data exists",
		},
		{
			name: "Whitespace-only visible_pixel_hash",
			proposal: smart_contract.Proposal{
				ID:     "whitespace-pixel",
				Status: "pending",
				Metadata: map[string]interface{}{
					"visible_pixel_hash": "   ", // Whitespace only
				},
			},
			expectError: true, // Expect error because whitespace-only is invalid
			description: "Should reject whitespace-only pixel hash",
		},
		{
			name: "Valid image_scan_data without pixel hash",
			proposal: smart_contract.Proposal{
				ID:     "scan-data-only",
				Status: "pending",
				Metadata: map[string]interface{}{
					// visible_pixel_hash is intentionally missing
					"image_scan_data": map[string]interface{}{
						"pixels": [][]string{{"ff", "00"}, {"aa", "bb"}},
					},
				},
			},
			expectError: false,
			description: "Should accept valid image_scan_data without pixel hash",
		},
	}

	for _, tt := range tests {
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

func TestProposalWorkflowTransitions(t *testing.T) {
	store := NewMemoryStore(time.Hour)
	ctx := context.Background()

	tests := []struct {
		name           string
		initialStatus  string
		targetStatus   string
		expectError    bool
		description    string
		setupFunc      func(string) error
		transitionFunc func(string) error
	}{
		{
			name:          "Pending to Approved - Valid",
			initialStatus: "pending",
			targetStatus:  "approved",
			expectError:   false,
			description:   "Should allow pending to approved transition",
			transitionFunc: func(id string) error {
				return store.ApproveProposal(ctx, id)
			},
		},
		{
			name:          "Approved to Published - Valid",
			initialStatus: "pending", // Changed from "approved" to "pending"
			targetStatus:  "published",
			expectError:   false,
			description:   "Should allow approved to published transition",
			setupFunc: func(id string) error {
				return store.ApproveProposal(ctx, id)
			},
			transitionFunc: func(id string) error {
				return store.PublishProposal(ctx, id)
			},
		},
		{
			name:          "Pending to Published - Invalid",
			initialStatus: "pending",
			targetStatus:  "published",
			expectError:   true,
			description:   "Should prevent pending to published transition",
			transitionFunc: func(id string) error {
				return store.PublishProposal(ctx, id)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uniqueHash := ""
			switch tt.name {
			case "Pending to Approved - Valid":
				uniqueHash = "1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a" // Valid 64-char hex
			case "Approved to Published - Valid":
				uniqueHash = "2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a" // Valid 64-char hex
			case "Pending to Published - Invalid":
				uniqueHash = "3a3a3a3a3a3a3a3a3a3a3a3a3a3a3a3a3a3a3a3a3a3a3a3a3a3a3a3a3a3a3a3a" // Valid 64-char hex
			default:
				uniqueHash = "fa1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1f" // Valid 64-char hex
			}

			proposal := smart_contract.Proposal{
				ID:     "workflow-" + tt.name,
				Status: tt.initialStatus,
				Metadata: map[string]interface{}{
					"visible_pixel_hash": uniqueHash, 
				},
				Tasks: []smart_contract.Task{
					{
						TaskID:     "workflow-" + tt.name + "-task-1",
						ContractID: uniqueHash,
						Title:      "Workflow task",
						BudgetSats: 1000,
						Status:     "available",
					},
				},
			}

			if err := store.CreateProposal(ctx, proposal); err != nil {
				t.Fatalf("Failed to create proposal: %v", err)
			}

			if tt.setupFunc != nil {
				if err := tt.setupFunc(proposal.ID); err != nil {
					t.Fatalf("Setup failed: %v", err)
				}
			}

			err := tt.transitionFunc(proposal.ID)
			if tt.expectError && err == nil {
				t.Errorf("Expected error for %s: %s", tt.name, tt.description)
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error for %s: %v", tt.name, err)
			}
		})
	}
}

func TestContractIDResolution(t *testing.T) {
	tests := []struct {
		name        string
		metadata    map[string]interface{}
		expectedID  string
		description string
	}{
		{
			name: "Visible pixel hash takes priority",
			metadata: map[string]interface{}{
				"visible_pixel_hash": "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2", // Valid 64-char hex
				"contract_id":        "contract456",
				"ingestion_id":       "ingestion789",
			},
			expectedID:  "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2",
			description: "Should use visible_pixel_hash as primary identifier",
		},
		{
			name: "Fallback to contract_id",
			metadata: map[string]interface{}{
				"contract_id":  "contract456",
				"ingestion_id": "ingestion789",
			},
			expectedID:  "contract456",
			description: "Should fallback to contract_id when no pixel hash",
		},
		{
			name: "Fallback to ingestion_id",
			metadata: map[string]interface{}{
				"ingestion_id": "ingestion789",
			},
			expectedID:  "ingestion789",
			description: "Should fallback to ingestion_id when no contract_id",
		},
		{
			name: "Empty metadata uses proposal ID",
			metadata: map[string]interface{}{
				"empty_field": "",
			},
			expectedID:  "test-proposal-id",
			description: "Should use proposal ID when no valid identifiers found",
		},
		{
			name: "Whitespace pixel hash falls back",
			metadata: map[string]interface{}{
				"visible_pixel_hash": "   ",
				"contract_id":        "contract456",
			},
			expectedID:  "contract456",
			description: "Should skip whitespace pixel hash and fallback",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := contractIDFromMeta(tt.metadata, "test-proposal-id")
			if result != tt.expectedID {
				t.Errorf("Expected %s for %s, got %s", tt.expectedID, tt.name, result)
				t.Errorf("Description: %s", tt.description)
			}
		})
	}
}

func TestProposalVisibilityWithPixelHash(t *testing.T) {
	store := NewMemoryStore(time.Hour)
	ctx := context.Background()

	// Test that setting visible_pixel_hash to contract ID doesn't cause disappearance
	pixelHashAsContractID := "a1a2a3a4b1b2b3b4c1c2c3c4d1d2d3d4e1e2e3e4f1f2f3f4a0a0a0a0a0a0a0a0" // Valid 64-char hex

		proposal := smart_contract.Proposal{
			ID:     "visibility-test",
			Status: "pending",
			Metadata: map[string]interface{}{
				"visible_pixel_hash": pixelHashAsContractID,
				"contract_id":        pixelHashAsContractID,
			},
		}

	// Create proposal
	if err := store.CreateProposal(ctx, proposal); err != nil {
		t.Fatalf("Failed to create proposal: %v", err)
	}

	// Verify proposal is retrievable
	retrieved, err := store.GetProposal(ctx, proposal.ID)
	if err != nil {
		t.Fatalf("Failed to retrieve proposal: %v", err)
	}

	if retrieved.Status != "pending" {
		t.Errorf("Expected proposal status to be pending, got %s", retrieved.Status)
	}

	// Verify it appears in listings
	filter := smart_contract.ProposalFilter{}
	proposals, err := store.ListProposals(ctx, filter)
	if err != nil {
		t.Fatalf("Failed to list proposals: %v", err)
	}

	found := false
	for _, p := range proposals {
		if p.ID == proposal.ID {
			found = true
			break
		}
	}

	if !found {
		t.Error("Proposal with visible_pixel_hash as contract ID not found in listings")
	}
}

func TestStatusFieldPreventsClaimingTasks(t *testing.T) {
	store := NewMemoryStore(time.Hour)

	// Test claiming tasks with various statuses
	claimTests := []struct {
		name        string
		taskStatus  string
		expectError bool
	}{
		{"Claim available task", "available", false},
		{"Claim claimed task", "claimed", true},
		{"Claim submitted task", "submitted", true},
		{"Claim published task", "published", true},
	}

	for _, tt := range claimTests {
		t.Run(tt.name, func(t *testing.T) {
			// Create task manually in the store since CreateTask doesn't exist
			taskID := "task-" + tt.name
			testTask := smart_contract.Task{
				TaskID:     taskID,
				ContractID: "contract-" + tt.name,
				Status:     tt.taskStatus,
			}

			// Access the internal tasks map directly for testing
			store.mu.Lock()
			store.tasks[taskID] = testTask
			store.mu.Unlock()

			_, err := store.ClaimTask(taskID, "contractor-123", "wallet123", nil)
			if tt.expectError && err == nil {
				t.Errorf("Expected error when claiming task with status %s", tt.taskStatus)
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error when claiming task with status %s: %v", tt.taskStatus, err)
			}
		})
	}
}
