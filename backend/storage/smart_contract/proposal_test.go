package smart_contract

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"stargate-backend/core/smart_contract"
)

func TestProposalLimitPerWish(t *testing.T) {
	store := NewMemoryStore(time.Hour)
	ctx := context.Background()

	pixelHash := "d0b1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1"

	for i := 1; i <= 5; i++ {
		prop := smart_contract.Proposal{
			ID:               fmt.Sprintf("proposal-%d", i),
			Status:           "pending",
			VisiblePixelHash: pixelHash,
			Title:            fmt.Sprintf("Proposal %d", i),
		}
		if err := store.CreateProposal(ctx, prop); err != nil {
			t.Fatalf("create proposal %d: %v", i, err)
		}
	}

	// 6th proposal should fail
	prop6 := smart_contract.Proposal{
		ID:               "proposal-6",
		Status:           "pending",
		VisiblePixelHash: pixelHash,
		Title:            "Proposal 6",
	}
	if err := store.CreateProposal(ctx, prop6); err == nil {
		t.Fatalf("expected 6th proposal to fail")
	} else if !strings.Contains(err.Error(), "maximum of 5 proposals reached") {
		t.Fatalf("expected 'maximum of 5 proposals reached' error, got: %v", err)
	}
}

func TestProposalFailsIfApprovedExists(t *testing.T) {
	store := NewMemoryStore(time.Hour)
	ctx := context.Background()

	pixelHash := "e0b1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1"

	// Create and approve a proposal
	prop1 := smart_contract.Proposal{
		ID:               "proposal-approved",
		Status:           "approved",
		VisiblePixelHash: pixelHash,
		Title:            "Approved Proposal",
	}
	if err := store.CreateProposal(ctx, prop1); err != nil {
		t.Fatalf("create approved proposal: %v", err)
	}

	// Create another proposal for the same wish - should fail
	prop2 := smart_contract.Proposal{
		ID:               "proposal-pending",
		Status:           "pending",
		VisiblePixelHash: pixelHash,
		Title:            "Pending Proposal",
	}
	if err := store.CreateProposal(ctx, prop2); err == nil {
		t.Fatalf("expected proposal creation to fail because an approved proposal exists")
	} else if !strings.Contains(err.Error(), "already approved/published") {
		t.Fatalf("expected 'already approved/published' error, got: %v", err)
	}
}

func TestApproveProposalPreventsDoubleApproval(t *testing.T) {
	store := NewMemoryStore(time.Hour)
	ctx := context.Background()

	pendingA := smart_contract.Proposal{
		ID:     "p-a",
		Status: "pending",
		Metadata: map[string]interface{}{
			"contract_id":        "1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2b",
			"visible_pixel_hash": "1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2b", // Valid 64-char hex
		},
		Tasks: []smart_contract.Task{
			{
				TaskID:     "p-a-task-1",
				ContractID: "1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2b",
				Title:      "Task A",
				BudgetSats: 1000,
				Status:     "available",
			},
		},
	}
	pendingB := smart_contract.Proposal{
		ID:     "p-b",
		Status: "pending",
		Metadata: map[string]interface{}{
			"contract_id":        "1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2b",
			"visible_pixel_hash": "1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2b", // Same as pendingA
		},
		Tasks: []smart_contract.Task{
			{
				TaskID:     "p-b-task-1",
				ContractID: "1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2b",
				Title:      "Task B",
				BudgetSats: 1000,
				Status:     "available",
			},
		},
	}
	if err := store.CreateProposal(ctx, pendingA); err != nil {
		t.Fatalf("create proposal A: %v", err)
	}
	if err := store.CreateProposal(ctx, pendingB); err != nil {
		t.Fatalf("create proposal B: %v", err)
	}

	if err := store.ApproveProposal(ctx, pendingA.ID); err != nil {
		t.Fatalf("approve first proposal: %v", err)
	}
	if err := store.ApproveProposal(ctx, pendingB.ID); err == nil {
		t.Fatalf("expected second approval to fail for same contract")
	}

	gotA, _ := store.GetProposal(ctx, pendingA.ID)
	if gotA.Status != "approved" {
		t.Fatalf("expected proposal A approved, got %s", gotA.Status)
	}
	gotB, _ := store.GetProposal(ctx, pendingB.ID)
	if gotB.Status != "rejected" && gotB.Status != "pending" {
		t.Fatalf("expected proposal B not approved, got %s", gotB.Status)
	}
}

func TestPixelHashDeterminesContractIdentity(t *testing.T) {
	store := NewMemoryStore(time.Hour)
	ctx := context.Background()

	pixelHash := "f9e8d7c6b5a41234567890abcdef1234567890abcdef1234567890abcdef1234" // Valid 64-char hex

	// Create first proposal with pixel hash
	proposalA := smart_contract.Proposal{
		ID:     "proposal-a",
		Status: "pending",
		Metadata: map[string]interface{}{
			"visible_pixel_hash": pixelHash,
		},
		Tasks: []smart_contract.Task{
			{
				TaskID:     "proposal-a-task-1",
				ContractID: pixelHash,
				Title:      "Task A",
				BudgetSats: 1000,
				Status:     "available",
			},
		},
	}
	if err := store.CreateProposal(ctx, proposalA); err != nil {
		t.Fatalf("create proposal A: %v", err)
	}

	// Create second proposal with same pixel hash
	proposalB := smart_contract.Proposal{
		ID:     "proposal-b",
		Status: "pending",
		Metadata: map[string]interface{}{
			"visible_pixel_hash": pixelHash,
		},
		Tasks: []smart_contract.Task{
			{
				TaskID:     "proposal-b-task-1",
				ContractID: pixelHash,
				Title:      "Task B",
				BudgetSats: 1000,
				Status:     "available",
			},
		},
	}
	if err := store.CreateProposal(ctx, proposalB); err != nil {
		t.Fatalf("create proposal B: %v", err)
	}

	// Approve first proposal
	if err := store.ApproveProposal(ctx, proposalA.ID); err != nil {
		t.Fatalf("approve first proposal: %v", err)
	}

	// Try to approve second proposal - should fail
	if err := store.ApproveProposal(ctx, proposalB.ID); err == nil {
		t.Fatalf("expected second approval to fail for same pixel hash")
	}

	gotA, _ := store.GetProposal(ctx, proposalA.ID)
	if gotA.Status != "approved" {
		t.Fatalf("expected proposal A approved, got %s", gotA.Status)
	}

	gotB, _ := store.GetProposal(ctx, proposalB.ID)
	if gotB.Status != "rejected" {
		t.Fatalf("expected proposal B rejected due to same pixel hash, got %s", gotB.Status)
	}
}

func TestPublishRequiresApprovedAndFinalizes(t *testing.T) {
	store := NewMemoryStore(time.Hour)
	ctx := context.Background()
	taskID := "task-1"

	prop := smart_contract.Proposal{
		ID:     "p-publish",
		Status: "pending",
		Metadata: map[string]interface{}{
			"contract_id":        "a0b1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1",
			"visible_pixel_hash": "a0b1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1", // Valid 64-char hex
		},
	}
	if err := store.CreateProposal(ctx, prop); err != nil {
		t.Fatalf("create proposal: %v", err)
	}

	store.tasks[taskID] = smart_contract.Task{
		TaskID:     taskID,
		ContractID: "a0b1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1", // Use the same valid visible_pixel_hash as contract ID
		Status:     "submitted",
	}

	if err := store.PublishProposal(ctx, prop.ID); err == nil {
		t.Fatalf("expected publish to fail when not approved")
	}

	if err := store.ApproveProposal(ctx, prop.ID); err != nil {
		t.Fatalf("approve proposal: %v", err)
	}
	if err := store.PublishProposal(ctx, prop.ID); err != nil {
		t.Fatalf("publish after approval: %v", err)
	}

	gotProp, _ := store.GetProposal(ctx, prop.ID)
	if gotProp.Status != "published" {
		t.Fatalf("expected proposal published, got %s", gotProp.Status)
	}
	task := store.tasks[taskID]
	if task.Status != "published" {
		t.Fatalf("expected task published, got %s", task.Status)
	}
}

func TestUpdateProposalPending(t *testing.T) {
	store := NewMemoryStore(time.Hour)
	ctx := context.Background()

	proposal := smart_contract.Proposal{
		ID:     "proposal-update",
		Status: "pending",
		Title:  "Original Title",
		Metadata: map[string]interface{}{
			"visible_pixel_hash": "b0b1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1",
		},
		Tasks: []smart_contract.Task{
			{
				TaskID:     "proposal-update-task-1",
				ContractID: "proposal-update",
				Title:      "Task",
				BudgetSats: 1000,
				Status:     "available",
			},
		},
	}
	if err := store.CreateProposal(ctx, proposal); err != nil {
		t.Fatalf("create proposal: %v", err)
	}

	update := smart_contract.Proposal{
		ID:    proposal.ID,
		Title: "Revised Proposal",
	}
	if err := store.UpdateProposal(ctx, update); err != nil {
		t.Fatalf("update proposal: %v", err)
	}

	got, _ := store.GetProposal(ctx, proposal.ID)
	if got.Title != "Revised Proposal" {
		t.Fatalf("expected title updated, got %s", got.Title)
	}
}

func TestUpdateProposalRejectedWhenApproved(t *testing.T) {
	store := NewMemoryStore(time.Hour)
	ctx := context.Background()

	proposal := smart_contract.Proposal{
		ID:     "proposal-update-approved",
		Status: "pending",
		Title:  "Original Title",
		Metadata: map[string]interface{}{
			"visible_pixel_hash": "c0b1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1",
		},
		Tasks: []smart_contract.Task{
			{
				TaskID:     "proposal-update-approved-task-1",
				ContractID: "proposal-update-approved",
				Title:      "Task",
				BudgetSats: 1000,
				Status:     "available",
			},
		},
	}
	if err := store.CreateProposal(ctx, proposal); err != nil {
		t.Fatalf("create proposal: %v", err)
	}
	if err := store.ApproveProposal(ctx, proposal.ID); err != nil {
		t.Fatalf("approve proposal: %v", err)
	}

	update := smart_contract.Proposal{
		ID:    proposal.ID,
		Title: "Revised Proposal",
	}
	if err := store.UpdateProposal(ctx, update); err == nil {
		t.Fatalf("expected update to fail after approval")
	}
}
