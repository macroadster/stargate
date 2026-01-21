package smart_contract

import (
	"context"
	"testing"
	"time"

	"stargate-backend/core/smart_contract"
	scstore "stargate-backend/storage/smart_contract"
)

func TestReconcileSyncAnnouncement(t *testing.T) {
	store := scstore.NewMemoryStore(72 * time.Hour)
	server := NewServer(store, nil, nil)
	ctx := context.Background()

	t.Run("Reconcile Task Update", func(t *testing.T) {
		taskID := "sync-task-1"
		task := smart_contract.Task{
			TaskID:     taskID,
			ContractID: "contract-1",
			Title:      "Synced Task",
			Status:     "claimed",
			ClaimedBy:  "ai-1",
		}

		ann := &syncAnnouncement{
			Type:   "task_update",
			Issuer: "remote-node",
			Task:   &task,
		}

		if err := server.ReconcileSyncAnnouncement(ctx, ann); err != nil {
			t.Fatalf("reconcile failed: %v", err)
		}

		got, err := store.GetTask(taskID)
		if err != nil {
			t.Fatalf("failed to get task: %v", err)
		}
		if got.Status != "claimed" {
			t.Errorf("expected status claimed, got %s", got.Status)
		}
		if got.ClaimedBy != "ai-1" {
			t.Errorf("expected claimed_by ai-1, got %s", got.ClaimedBy)
		}
	})

	t.Run("Reconcile Proposal Update", func(t *testing.T) {
		proposalID := "sync-proposal-1"
		visibleHash := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
		proposal := smart_contract.Proposal{
			ID:               proposalID,
			Title:            "Synced Proposal",
			Status:           "pending",
			VisiblePixelHash: visibleHash,
			Metadata: map[string]interface{}{
				"visible_pixel_hash": visibleHash,
				"contract_id":        visibleHash,
			},
		}

		// Seed a wish contract first to satisfy validation
		wishID := "wish-" + visibleHash
		store.UpsertContractWithTasks(ctx, smart_contract.Contract{ContractID: wishID, Status: "active"}, nil)

		ann := &syncAnnouncement{
			Type:     "proposal_create",
			Issuer:   "remote-node",
			Proposal: &proposal,
		}

		if err := server.ReconcileSyncAnnouncement(ctx, ann); err != nil {
			t.Fatalf("reconcile failed: %v", err)
		}

		got, err := store.GetProposal(ctx, proposalID)
		if err != nil {
			t.Fatalf("failed to get proposal: %v", err)
		}
		if got.Title != "Synced Proposal" {
			t.Errorf("expected title Synced Proposal, got %s", got.Title)
		}
	})

	t.Run("Reconcile Escort Validation", func(t *testing.T) {
		taskID := "sync-task-2"
		status := smart_contract.EscortStatus{
			TaskID:      taskID,
			ProofStatus: "confirmed",
			LastChecked: time.Now(),
		}

		ann := &syncAnnouncement{
			Type:         "escort_validation",
			Issuer:       "remote-node",
			EscortStatus: &status,
		}

		if err := server.ReconcileSyncAnnouncement(ctx, ann); err != nil {
			t.Fatalf("reconcile failed: %v", err)
		}

		// In MemoryStore, SyncEscortStatus should store it
		// (Assuming we added a way to retrieve it, but for now we just check it doesn't error)
	})
}
