package mcp

import (
	"context"
	"testing"
	"time"
)

func TestApproveProposalPreventsDoubleApproval(t *testing.T) {
	store := NewMemoryStore(time.Hour)
	ctx := context.Background()

	pendingA := Proposal{
		ID:     "p-a",
		Status: "pending",
		Metadata: map[string]interface{}{
			"contract_id": "contract-123",
		},
	}
	pendingB := Proposal{
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

func TestPublishRequiresApprovedAndFinalizes(t *testing.T) {
	store := NewMemoryStore(time.Hour)
	ctx := context.Background()
	contractID := "contract-publish"
	taskID := "task-1"

	prop := Proposal{
		ID:     "p-publish",
		Status: "pending",
		Metadata: map[string]interface{}{
			"contract_id": contractID,
		},
	}
	if err := store.CreateProposal(ctx, prop); err != nil {
		t.Fatalf("create proposal: %v", err)
	}

	store.tasks[taskID] = Task{
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
