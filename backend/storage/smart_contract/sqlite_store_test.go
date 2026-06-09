package smart_contract

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	core "stargate-backend/core/smart_contract"
)

func newTestSQLiteStore(t *testing.T) *SQLiteStore {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "mcp.db")
	store, err := NewSQLiteStore(dbPath, time.Hour, false)
	if err != nil {
		t.Fatalf("create sqlite store: %v", err)
	}

	t.Cleanup(store.Close)
	return store
}

func TestSQLiteStoreUpsertTaskUpdatesFullRecord(t *testing.T) {
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	contract := core.Contract{
		ContractID:          "contract-1",
		Title:               "Initial Contract",
		Status:              "active",
		CreatedAt:           time.Now().UTC(),
		TotalBudgetSats:     1000,
		AvailableTasksCount: 1,
	}
	task := core.Task{
		TaskID:         "task-1",
		ContractID:     contract.ContractID,
		Title:          "Initial Task",
		Description:    "Initial description",
		BudgetSats:     100,
		Skills:         []string{"go"},
		Status:         "available",
		Difficulty:     "easy",
		EstimatedHours: 1,
		Requirements:   map[string]string{"lang": "go"},
	}

	if err := store.UpsertContractWithTasks(ctx, contract, []core.Task{task}); err != nil {
		t.Fatalf("seed contract and task: %v", err)
	}

	claimedAt := time.Now().UTC().Truncate(time.Second)
	expiresAt := claimedAt.Add(time.Hour)
	proof := &core.MerkleProof{
		TxID:               "tx-1",
		VisiblePixelHash:   "pixel-1",
		ConfirmationStatus: "provisional",
		SeenAt:             claimedAt,
	}

	updated := core.Task{
		TaskID:         task.TaskID,
		ContractID:     contract.ContractID,
		GoalID:         "goal-2",
		Title:          "Updated Task",
		Description:    "Updated description",
		BudgetSats:     250,
		Skills:         []string{"rust", "sql"},
		Status:         "claimed",
		ClaimedBy:      "wallet-1",
		ClaimedAt:      &claimedAt,
		ClaimExpires:   &expiresAt,
		Difficulty:     "hard",
		EstimatedHours: 6,
		Requirements:   map[string]string{"lang": "rust"},
		MerkleProof:    proof,
	}

	if err := store.UpsertTask(ctx, updated); err != nil {
		t.Fatalf("upsert task: %v", err)
	}

	got, err := store.GetTask(task.TaskID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}

	if got.Title != updated.Title {
		t.Fatalf("expected title %q, got %q", updated.Title, got.Title)
	}
	if got.Description != updated.Description {
		t.Fatalf("expected description %q, got %q", updated.Description, got.Description)
	}
	if got.BudgetSats != updated.BudgetSats {
		t.Fatalf("expected budget %d, got %d", updated.BudgetSats, got.BudgetSats)
	}
	if len(got.Skills) != 2 || got.Skills[0] != "rust" || got.Skills[1] != "sql" {
		t.Fatalf("expected updated skills, got %#v", got.Skills)
	}
	if got.ClaimedBy != updated.ClaimedBy {
		t.Fatalf("expected claimed_by %q, got %q", updated.ClaimedBy, got.ClaimedBy)
	}
	if got.Difficulty != updated.Difficulty {
		t.Fatalf("expected difficulty %q, got %q", updated.Difficulty, got.Difficulty)
	}
	if got.EstimatedHours != updated.EstimatedHours {
		t.Fatalf("expected estimated_hours %d, got %d", updated.EstimatedHours, got.EstimatedHours)
	}
	if got.Requirements["lang"] != "rust" {
		t.Fatalf("expected updated requirements, got %#v", got.Requirements)
	}
	if got.MerkleProof == nil || got.MerkleProof.TxID != proof.TxID {
		t.Fatalf("expected updated proof, got %#v", got.MerkleProof)
	}
}

func TestSQLiteStoreContractReworkRequestLifecycle(t *testing.T) {
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	contract := core.Contract{
		ContractID:          "contract-rework",
		Title:               "Needs Rework",
		Status:              "active",
		CreatedAt:           time.Now().UTC(),
		TotalBudgetSats:     2000,
		AvailableTasksCount: 1,
	}
	task := core.Task{
		TaskID:      "task-rework",
		ContractID:  contract.ContractID,
		Title:       "Task",
		BudgetSats:  500,
		Status:      "submitted",
		Description: "Submitted task",
	}

	if err := store.UpsertContractWithTasks(ctx, contract, []core.Task{task}); err != nil {
		t.Fatalf("seed contract: %v", err)
	}

	req, err := store.CreateContractReworkRequest(ctx, contract.ContractID, "wallet-1", "needs changes")
	if err != nil {
		t.Fatalf("create rework request: %v", err)
	}

	reqs, err := store.GetContractReworkRequests(ctx, contract.ContractID)
	if err != nil {
		t.Fatalf("get rework requests: %v", err)
	}
	if len(reqs) != 1 {
		t.Fatalf("expected 1 rework request, got %d", len(reqs))
	}
	if reqs[0].RequestID != req.RequestID {
		t.Fatalf("expected request id %q, got %q", req.RequestID, reqs[0].RequestID)
	}
	if reqs[0].Requester != "wallet-1" {
		t.Fatalf("expected requester wallet-1, got %q", reqs[0].Requester)
	}
	if reqs[0].CreatedAt.IsZero() {
		t.Fatal("expected created_at to round-trip")
	}

	if err := store.ResolveContractReworkRequest(ctx, contract.ContractID, req.RequestID); err != nil {
		t.Fatalf("resolve rework request: %v", err)
	}

	reqs, err = store.GetContractReworkRequests(ctx, contract.ContractID)
	if err != nil {
		t.Fatalf("get rework requests after resolve: %v", err)
	}
	if len(reqs) != 1 {
		t.Fatalf("expected 1 rework request after resolve, got %d", len(reqs))
	}
	if reqs[0].Status != "resolved" {
		t.Fatalf("expected status resolved, got %q", reqs[0].Status)
	}
	if reqs[0].ResolvedAt == nil || reqs[0].ResolvedAt.IsZero() {
		t.Fatal("expected resolved_at to be set")
	}

	gotTask, err := store.GetTask(task.TaskID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if gotTask.Status != "rejected" {
		t.Fatalf("expected task status rejected after rework request, got %q", gotTask.Status)
	}
}

func TestSQLiteStoreProposalWorkflowValidation(t *testing.T) {
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	visibleHash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	proposalA := core.Proposal{
		ID:               "proposal-a",
		Title:            "Proposal A",
		DescriptionMD:    "### Task 1: Ship it",
		VisiblePixelHash: visibleHash,
		Status:           "pending",
		BudgetSats:       1000,
		Metadata: map[string]interface{}{
			"visible_pixel_hash": visibleHash,
			"contract_id":        visibleHash,
		},
		Tasks: []core.Task{
			{
				TaskID:     "proposal-a-task-1",
				ContractID: visibleHash,
				Title:      "Task A",
				BudgetSats: 1000,
				Status:     "available",
			},
		},
	}
	proposalB := core.Proposal{
		ID:               "proposal-b",
		Title:            "Proposal B",
		DescriptionMD:    "### Task 1: Competing plan",
		VisiblePixelHash: visibleHash,
		Status:           "pending",
		BudgetSats:       1000,
		Metadata: map[string]interface{}{
			"visible_pixel_hash": visibleHash,
			"contract_id":        visibleHash,
		},
		Tasks: []core.Task{
			{
				TaskID:     "proposal-b-task-1",
				ContractID: visibleHash,
				Title:      "Task B",
				BudgetSats: 1000,
				Status:     "available",
			},
		},
	}

	if err := store.CreateProposal(ctx, proposalA); err != nil {
		t.Fatalf("create proposal A: %v", err)
	}
	if err := store.CreateProposal(ctx, proposalB); err != nil {
		t.Fatalf("create proposal B: %v", err)
	}
	if err := store.UpsertContractWithTasks(ctx, core.Contract{
		ContractID:          visibleHash,
		Title:               "Contract",
		Status:              "active",
		CreatedAt:           time.Now().UTC(),
		TotalBudgetSats:     1000,
		AvailableTasksCount: 1,
	}, proposalA.Tasks); err != nil {
		t.Fatalf("seed contract tasks: %v", err)
	}

	if err := store.PublishProposal(ctx, proposalA.ID); err == nil {
		t.Fatal("expected publish to fail for pending proposal")
	}

	if err := store.ApproveProposal(ctx, proposalA.ID); err != nil {
		t.Fatalf("approve proposal A: %v", err)
	}

	gotA, err := store.GetProposal(ctx, proposalA.ID)
	if err != nil {
		t.Fatalf("get proposal A: %v", err)
	}
	if gotA.Status != "approved" {
		t.Fatalf("expected proposal A approved, got %q", gotA.Status)
	}

	gotB, err := store.GetProposal(ctx, proposalB.ID)
	if err != nil {
		t.Fatalf("get proposal B: %v", err)
	}
	if gotB.Status != "rejected" {
		t.Fatalf("expected competing proposal rejected, got %q", gotB.Status)
	}

	if err := store.ApproveProposal(ctx, proposalB.ID); err == nil {
		t.Fatal("expected second approval to fail for same contract")
	}

	claim, err := store.ClaimTask(proposalA.Tasks[0].TaskID, "wallet-1", nil)
	if err != nil {
		t.Fatalf("claim task: %v", err)
	}
	gotClaim, err := store.GetClaim(claim.ClaimID)
	if err != nil {
		t.Fatalf("get claim: %v", err)
	}
	if gotClaim.Status != "active" {
		t.Fatalf("expected active claim before publish, got %q", gotClaim.Status)
	}

	tasks, err := store.ListTasks(core.TaskFilter{ContractID: visibleHash})
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}

	foundClaimed := false
	for _, task := range tasks {
		if task.Status == "claimed" {
			foundClaimed = true
		}
	}
	if !foundClaimed {
		t.Fatal("expected claimed task before publish")
	}

	if err := store.PublishProposal(ctx, proposalA.ID); err != nil {
		t.Fatalf("publish approved proposal: %v", err)
	}

	tasks, err = store.ListTasks(core.TaskFilter{ContractID: visibleHash})
	if err != nil {
		t.Fatalf("list tasks after publish: %v", err)
	}
	if tasks[0].Status != "published" {
		t.Fatalf("expected task published, got %q", tasks[0].Status)
	}
}
