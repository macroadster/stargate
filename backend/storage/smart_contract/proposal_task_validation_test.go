package smart_contract

import (
	"context"
	"testing"
	"time"

	"stargate-backend/core/smart_contract"
)

func TestProposalTaskValidation(t *testing.T) {
	store := NewMemoryStore(time.Hour)
	ctx := context.Background()

	t.Run("Proposal with tasks missing budget", func(t *testing.T) {
		proposal := smart_contract.Proposal{
			ID:            "test-proposal-1",
			Title:         "Test Proposal Budget",
			DescriptionMD: "Brief text",
			BudgetSats:    3000,
			Status:        "pending",
			Tasks: []smart_contract.Task{
				{
					TaskID:     "task-1",
					Title:      "Task One",
					BudgetSats: 0, // Missing budget
					Status:     "available",
				},
				{
					TaskID:     "task-2",
					Title:      "Task Two",
					BudgetSats: 1000, // Has budget
					Status:     "available",
				},
			},
			Metadata: map[string]any{
				"visible_pixel_hash": "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
			},
		}

		err := store.CreateProposal(ctx, proposal)
		if err == nil {
			t.Error("Expected validation error for task with missing budget")
		}
		if !containsString(err.Error(), "task 1") || !containsString(err.Error(), "positive budget_sats") {
			t.Errorf("Expected error about task 1 budget, got: %v", err)
		}
	})

	t.Run("Approved proposal with no tasks", func(t *testing.T) {
		proposal := smart_contract.Proposal{
			ID:            "test-proposal-2",
			Title:         "Test Proposal No Tasks",
			DescriptionMD: "Brief text",
			BudgetSats:    3000,
			Status:        "approved", // Approved but no tasks
			Tasks:         []smart_contract.Task{},
			Metadata: map[string]any{
				"visible_pixel_hash": "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
			},
		}

		err := store.CreateProposal(ctx, proposal)
		if err == nil {
			t.Error("Expected validation error for approved proposal with no tasks")
		}
		if !containsString(err.Error(), "approved proposals must contain") {
			t.Errorf("Expected error about approved proposals needing tasks, got: %v", err)
		}
	})

	t.Run("Valid proposal with tasks and budgets", func(t *testing.T) {
		proposal := smart_contract.Proposal{
			ID:            "test-proposal-3",
			Title:         "Valid Proposal Tasks",
			DescriptionMD: "Brief text",
			BudgetSats:    3000,
			Status:        "pending",
			Tasks: []smart_contract.Task{
				{
					TaskID:      "task-1",
					Title:       "Task One",
					Description: "Task information",
					BudgetSats:  1000,
					Status:      "available",
				},
				{
					TaskID:      "task-2",
					Title:       "Task Two",
					Description: "More information",
					BudgetSats:  2000,
					Status:      "available",
				},
			},
			Metadata: map[string]any{
				"visible_pixel_hash": "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
			},
		}

		err := store.CreateProposal(ctx, proposal)
		if err != nil {
			t.Errorf("Expected success for valid proposal, got error: %v", err)
		}
	})

	t.Run("Pending proposal with no tasks (allowed)", func(t *testing.T) {
		proposal := smart_contract.Proposal{
			ID:            "test-proposal-4",
			Title:         "Pending Proposal",
			DescriptionMD: "Brief text",
			BudgetSats:    3000,
			Status:        "pending", // Pending without tasks is allowed
			Metadata: map[string]any{
				"visible_pixel_hash": "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
			},
		}

		err := store.CreateProposal(ctx, proposal)
		if err != nil {
			t.Errorf("Expected success for pending proposal without tasks, got error: %v", err)
		}
	})
}

func containsString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
