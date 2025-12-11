package smart_contract

import (
	"context"
	"testing"
	"time"

	"stargate-backend/core/smart_contract"
)

func TestApproveProposalPreventsDoubleApproval(t *testing.T) {
	store := NewMemoryStore(time.Hour)
	ctx := context.Background()

	pendingA := smart_contract.Proposal{
		ID:     "p-a",
		Status: "pending",
		Metadata: map[string]interface{}{
			"contract_id": "contract-123",
		},
	}
	pendingB := smart_contract.Proposal{
		ID:     "p-b",
		Status: "pending",
		Metadata: map[string]interface{}{
			"contract_id": "contract-123",
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

	pixelHash := "abc123pixelhash"

	// Create first proposal with pixel hash
	proposalA := smart_contract.Proposal{
		ID:     "proposal-a",
		Status: "pending",
		Metadata: map[string]interface{}{
			"visible_pixel_hash": pixelHash,
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
	contractID := "contract-publish"
	taskID := "task-1"

	prop := smart_contract.Proposal{
		ID:     "p-publish",
		Status: "pending",
		Metadata: map[string]interface{}{
			"contract_id": contractID,
		},
	}
	if err := store.CreateProposal(ctx, prop); err != nil {
		t.Fatalf("create proposal: %v", err)
	}

	store.tasks[taskID] = smart_contract.Task{
		TaskID:     taskID,
		ContractID: contractID,
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
