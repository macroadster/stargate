package smart_contract

import (
	"context"
	"path/filepath"
	"strings"
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

func TestSQLiteClaimTaskPersistsContractorWallet(t *testing.T) {
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	const wallet = "tb1qcssj2506qzmvryuvqaa83ncm80umfmz9py5qyy"
	const pixelHash = "09b23298cdfca94a9ecf332dff923941daf4b2c2f3fe2656398001f70236637e"

	contract := core.Contract{
		ContractID:          "wish-" + pixelHash,
		Title:               "Spreadsheet Web App Delivery",
		Status:              "active",
		CreatedAt:           time.Now().UTC(),
		TotalBudgetSats:     1000,
		AvailableTasksCount: 1,
	}
	// Seed with provisional merkle proof (as proposal publish does) but no contractor_wallet yet.
	task := core.Task{
		TaskID:      "proposal-test-task-1",
		ContractID:  contract.ContractID,
		Title:       "Implement Spreadsheet web app",
		Description: "Ship the app",
		BudgetSats:  500,
		Status:      "available",
		MerkleProof: &core.MerkleProof{
			VisiblePixelHash:   pixelHash,
			FundedAmountSats:   500,
			ConfirmationStatus: "provisional",
		},
	}
	if err := store.UpsertContractWithTasks(ctx, contract, []core.Task{task}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	claim, err := store.ClaimTask(task.TaskID, wallet, nil)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if claim.AiIdentifier != wallet {
		t.Fatalf("claim ai_identifier: got %q want %q", claim.AiIdentifier, wallet)
	}

	got, err := store.GetTask(task.TaskID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if got.Status != "claimed" {
		t.Fatalf("status: got %q want claimed", got.Status)
	}
	if got.ClaimedBy != wallet {
		t.Fatalf("claimed_by: got %q want %q", got.ClaimedBy, wallet)
	}
	if got.ContractorWallet != wallet {
		t.Fatalf("contractor_wallet: got %q want %q", got.ContractorWallet, wallet)
	}
	if got.MerkleProof == nil {
		t.Fatal("expected merkle_proof after claim")
	}
	if got.MerkleProof.ContractorWallet != wallet {
		t.Fatalf("merkle_proof.contractor_wallet: got %q want %q", got.MerkleProof.ContractorWallet, wallet)
	}
	// Existing provisional fields must survive claim.
	if got.MerkleProof.VisiblePixelHash != pixelHash {
		t.Fatalf("visible_pixel_hash wiped: got %q", got.MerkleProof.VisiblePixelHash)
	}
	if got.MerkleProof.FundedAmountSats != 500 {
		t.Fatalf("funded_amount_sats wiped: got %d", got.MerkleProof.FundedAmountSats)
	}
	if got.MerkleProof.ConfirmationStatus != "provisional" {
		t.Fatalf("confirmation_status wiped: got %q", got.MerkleProof.ConfirmationStatus)
	}

	// ListTasks path also exposes contractor_wallet (build_psbt uses this).
	listed, err := store.ListTasks(core.TaskFilter{ContractID: contract.ContractID})
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("list tasks len: got %d", len(listed))
	}
	if listed[0].ContractorWallet != wallet {
		t.Fatalf("list contractor_wallet: got %q want %q", listed[0].ContractorWallet, wallet)
	}

	// UpdateTaskProof must not wipe contractor_wallet when callers rewrite provisional proofs.
	newProof := &core.MerkleProof{
		VisiblePixelHash:   pixelHash,
		FundedAmountSats:   500,
		ConfirmationStatus: "provisional",
		// intentionally omit ContractorWallet
	}
	if err := store.UpdateTaskProof(ctx, task.TaskID, newProof); err != nil {
		t.Fatalf("update proof: %v", err)
	}
	got, err = store.GetTask(task.TaskID)
	if err != nil {
		t.Fatalf("get task after proof update: %v", err)
	}
	if got.ContractorWallet != wallet || got.MerkleProof == nil || got.MerkleProof.ContractorWallet != wallet {
		t.Fatalf("contractor_wallet lost after UpdateTaskProof: task=%q proof=%#v", got.ContractorWallet, got.MerkleProof)
	}

	// Idempotent re-claim still returns the active claim and keeps wallet.
	claim2, err := store.ClaimTask(task.TaskID, wallet, nil)
	if err != nil {
		t.Fatalf("re-claim: %v", err)
	}
	if claim2.ClaimID != claim.ClaimID {
		t.Fatalf("re-claim id: got %q want %q", claim2.ClaimID, claim.ClaimID)
	}
	got, err = store.GetTask(task.TaskID)
	if err != nil {
		t.Fatalf("get task after re-claim: %v", err)
	}
	if got.ContractorWallet != wallet {
		t.Fatalf("contractor_wallet after re-claim: got %q want %q", got.ContractorWallet, wallet)
	}
}

func TestSQLiteClaimTaskBackfillsMissingContractorWallet(t *testing.T) {
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	const wallet = "tb1qcssj2506qzmvryuvqaa83ncm80umfmz9py5qyy"

	contract := core.Contract{
		ContractID:          "contract-backfill",
		Title:               "Backfill",
		Status:              "active",
		CreatedAt:           time.Now().UTC(),
		TotalBudgetSats:     100,
		AvailableTasksCount: 1,
	}
	task := core.Task{
		TaskID:     "task-backfill-1",
		ContractID: contract.ContractID,
		Title:      "Backfill task",
		BudgetSats: 100,
		Status:     "available",
	}
	if err := store.UpsertContractWithTasks(ctx, contract, []core.Task{task}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Simulate legacy claim: status claimed + claim row, but no contractor_wallet in merkle_proof.
	if _, err := store.ClaimTask(task.TaskID, wallet, nil); err != nil {
		t.Fatalf("initial claim: %v", err)
	}
	// Wipe contractor_wallet as if written by a pre-fix sqlite path.
	if err := store.UpdateTaskProof(ctx, task.TaskID, &core.MerkleProof{
		ConfirmationStatus: "provisional",
		// no contractor_wallet — but UpdateTaskProof now preserves existing; force-wipe via raw SQL
	}); err != nil {
		t.Fatalf("update proof: %v", err)
	}
	// Force-clear wallet the way the old claim path left tasks (no contractor_wallet in stored proof).
	if _, err := store.db.Exec(`UPDATE mcp_tasks SET merkle_proof=? WHERE task_id=?`,
		`{"confirmation_status":"provisional"}`, task.TaskID); err != nil {
		t.Fatalf("force wipe: %v", err)
	}
	var rawProof string
	if err := store.db.QueryRow(`SELECT merkle_proof FROM mcp_tasks WHERE task_id=?`, task.TaskID).Scan(&rawProof); err != nil {
		t.Fatalf("read raw proof: %v", err)
	}
	if strings.Contains(rawProof, "contractor_wallet") {
		t.Fatalf("expected raw merkle_proof without contractor_wallet, got %s", rawProof)
	}

	// Re-claim by same wallet should persist contractor_wallet back into storage.
	if _, err := store.ClaimTask(task.TaskID, wallet, nil); err != nil {
		t.Fatalf("re-claim backfill: %v", err)
	}
	if err := store.db.QueryRow(`SELECT merkle_proof FROM mcp_tasks WHERE task_id=?`, task.TaskID).Scan(&rawProof); err != nil {
		t.Fatalf("read raw proof after backfill: %v", err)
	}
	if !strings.Contains(rawProof, wallet) {
		t.Fatalf("expected persisted contractor_wallet in merkle_proof, got %s", rawProof)
	}
	got, err := store.GetTask(task.TaskID)
	if err != nil {
		t.Fatalf("get after backfill: %v", err)
	}
	if got.ContractorWallet != wallet {
		t.Fatalf("backfill contractor_wallet: got %q want %q", got.ContractorWallet, wallet)
	}
	if got.MerkleProof == nil || got.MerkleProof.ContractorWallet != wallet {
		t.Fatalf("backfill merkle proof wallet missing: %#v", got.MerkleProof)
	}
}

func TestSQLiteGetTaskFallsBackContractorWalletFromClaimedBy(t *testing.T) {
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	const wallet = "tb1qcssj2506qzmvryuvqaa83ncm80umfmz9py5qyy"
	contract := core.Contract{
		ContractID:          "contract-fallback",
		Title:               "Fallback",
		Status:              "active",
		CreatedAt:           time.Now().UTC(),
		TotalBudgetSats:     100,
		AvailableTasksCount: 1,
	}
	task := core.Task{
		TaskID:     "task-fallback-1",
		ContractID: contract.ContractID,
		Title:      "Fallback task",
		BudgetSats: 100,
		Status:     "available",
	}
	if err := store.UpsertContractWithTasks(ctx, contract, []core.Task{task}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := store.ClaimTask(task.TaskID, wallet, nil); err != nil {
		t.Fatalf("claim: %v", err)
	}
	// Legacy row shape: claimed_by set, merkle_proof without contractor_wallet.
	if _, err := store.db.Exec(`UPDATE mcp_tasks SET merkle_proof=? WHERE task_id=?`,
		`{"confirmation_status":"provisional","funded_amount_sats":100}`, task.TaskID); err != nil {
		t.Fatalf("wipe proof wallet: %v", err)
	}

	got, err := store.GetTask(task.TaskID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ContractorWallet != wallet {
		t.Fatalf("expected fallback contractor_wallet %q, got %q", wallet, got.ContractorWallet)
	}
	if got.MerkleProof == nil || got.MerkleProof.ContractorWallet != wallet {
		t.Fatalf("expected in-memory merkle proof wallet backfill, got %#v", got.MerkleProof)
	}

	listed, err := store.ListTasks(core.TaskFilter{ContractID: contract.ContractID})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 1 || listed[0].ContractorWallet != wallet {
		t.Fatalf("list fallback failed: %#v", listed)
	}
}

// Regression: force-scan/oracle reconcile panics when contract metadata is JSON null
// ("assignment to entry in nil map" in ConfirmContract).
func TestSQLiteConfirmContractWithNullMetadata(t *testing.T) {
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	// Use a non-wish id so ConfirmContract does not self-supersede after confirm.
	const contractID = "09b23298cdfca94a9ecf332dff923941daf4b2c2f3fe2656398001f70236637e"
	contract := core.Contract{
		ContractID:          contractID,
		Title:               "Null metadata contract",
		Status:              "active",
		CreatedAt:           time.Now().UTC(),
		TotalBudgetSats:     1000,
		AvailableTasksCount: 0,
	}
	if err := store.UpsertContractWithTasks(ctx, contract, nil); err != nil {
		t.Fatalf("seed contract: %v", err)
	}
	// JSON null unmarshals into a nil map — the panic path on force scan.
	if _, err := store.db.Exec(`UPDATE mcp_contracts SET metadata=? WHERE contract_id=?`, "null", contractID); err != nil {
		t.Fatalf("set null metadata: %v", err)
	}

	const txid = "6df5d5c0ec58aa3b000000000000000000000000000000000000000000000000"
	const height = 143438
	if err := store.ConfirmContract(ctx, contractID, height, txid); err != nil {
		t.Fatalf("ConfirmContract with null metadata: %v", err)
	}

	got, err := store.GetContract(contractID)
	if err != nil {
		t.Fatalf("GetContract after confirm: %v", err)
	}
	if got.Status != "confirmed" {
		t.Fatalf("status: got %q want confirmed", got.Status)
	}
	if got.Metadata == nil {
		t.Fatal("metadata should not be nil after confirm")
	}
	if got.Metadata["confirmed_txid"] != txid {
		t.Fatalf("confirmed_txid: got %#v want %q", got.Metadata["confirmed_txid"], txid)
	}
}

func TestBlockImageFileKeyStripsWishPrefix(t *testing.T) {
	const hash = "a72d3bcda257ff166b14393b96651a8a49bdc20d8ab7e8a8d239be662db21f59"
	if got := BlockImageFileKey("wish-" + hash); got != hash {
		t.Fatalf("wish prefix: got %q want %q", got, hash)
	}
	if got := BlockImageFileKey(hash); got != hash {
		t.Fatalf("bare: got %q want %q", got, hash)
	}
}

func TestSQLiteConfirmContractStegoURLUsesBareHash(t *testing.T) {
	store := newTestSQLiteStore(t)
	ctx := context.Background()
	const hash = "a72d3bcda257ff166b14393b96651a8a49bdc20d8ab7e8a8d239be662db21f59"
	wishID := "wish-" + hash
	if err := store.UpsertContractWithTasks(ctx, core.Contract{
		ContractID: wishID,
		Title:      "Twin Dragons",
		Status:     "active",
		CreatedAt:  time.Now().UTC(),
	}, nil); err != nil {
		t.Fatalf("seed: %v", err)
	}
	const height = 145333
	const txid = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if err := store.ConfirmContract(ctx, wishID, height, txid); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	got, err := store.GetContract(wishID)
	if err != nil {
		t.Fatal(err)
	}
	want := "/api/block-image/145333/" + hash
	if got.StegoImageURL != want {
		t.Fatalf("stego_image_url: got %q want %q", got.StegoImageURL, want)
	}
	if strings.Contains(got.StegoImageURL, "wish-") {
		t.Fatalf("stego_image_url must not contain wish- prefix: %q", got.StegoImageURL)
	}
}

func TestSQLiteConfirmContractBootstrapsFromWishWithNullMetadata(t *testing.T) {
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	const hash = "09b23298cdfca94a9ecf332dff923941daf4b2c2f3fe2656398001f70236637e"
	wishID := "wish-" + hash
	wish := core.Contract{
		ContractID:          wishID,
		Title:               "Wish with null metadata",
		Status:              "active",
		CreatedAt:           time.Now().UTC(),
		TotalBudgetSats:     500,
		AvailableTasksCount: 0,
	}
	if err := store.UpsertContractWithTasks(ctx, wish, nil); err != nil {
		t.Fatalf("seed wish: %v", err)
	}
	if _, err := store.db.Exec(`UPDATE mcp_contracts SET metadata=? WHERE contract_id=?`, "null", wishID); err != nil {
		t.Fatalf("set null wish metadata: %v", err)
	}

	const txid = "6df5d5c0ec58aa3b000000000000000000000000000000000000000000000001"
	const height = 143438
	// Confirm via bare hash: must land on canonical wish- row (no bare twin).
	if err := store.ConfirmContract(ctx, hash, height, txid); err != nil {
		t.Fatalf("ConfirmContract bootstrap: %v", err)
	}

	got, err := store.GetContract(wishID)
	if err != nil {
		t.Fatalf("GetContract confirmed: %v", err)
	}
	if got.Status != "confirmed" {
		t.Fatalf("status: got %q want confirmed", got.Status)
	}
	if got.Metadata == nil || got.Metadata["confirmed_txid"] != txid {
		t.Fatalf("expected confirmed_txid in metadata, got %#v", got.Metadata)
	}
	// Bare-hash twin must not be a second confirmed listing.
	if bare, err := store.GetContract(hash); err == nil && bare.Status == "confirmed" {
		t.Fatalf("bare hash must not stay confirmed when wish- is canonical; got %#v", bare)
	}
}

// Regression: ensureMatchedContract confirms wish- then markIngestionConfirmed
// used to Confirm bare hash, bootstrapping a second confirmed row. Integrity
// hardening then blocked superseding the confirmed wish → duplicate on /contracts.
func TestSQLiteConfirmContractNoDuplicateWishAndBare(t *testing.T) {
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	const hash = "a72d3bcda257ff166b14393b96651a8a49bdc20d8ab7e8a8d239be662db21f59"
	wishID := "wish-" + hash

	if err := store.UpsertContractWithTasks(ctx, core.Contract{
		ContractID: wishID,
		Title:      "Production wish",
		Status:     "active",
		CreatedAt:  time.Now().UTC(),
	}, nil); err != nil {
		t.Fatalf("seed wish: %v", err)
	}

	const txid = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const height = 200000

	// Path A: ensureMatchedContract / stego reconcile confirms wish-
	if err := store.ConfirmContract(ctx, wishID, height, txid); err != nil {
		t.Fatalf("confirm wish: %v", err)
	}
	// Path B: markIngestionConfirmed used bare ingestion id
	if err := store.ConfirmContract(ctx, hash, height, txid); err != nil {
		t.Fatalf("confirm bare: %v", err)
	}

	list, err := store.ListContracts(core.ContractFilter{Status: "confirmed", Limit: 50})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var confirmedForWish int
	for _, c := range list {
		id := strings.TrimPrefix(c.ContractID, "wish-")
		if id == hash || c.ContractID == wishID || c.ContractID == hash {
			if c.Status == "confirmed" {
				confirmedForWish++
			}
		}
	}
	if confirmedForWish != 1 {
		t.Fatalf("expected exactly 1 confirmed row for wish, got %d: %+v", confirmedForWish, list)
	}
	wish, err := store.GetContract(wishID)
	if err != nil {
		t.Fatalf("get wish: %v", err)
	}
	if wish.Status != "confirmed" {
		t.Fatalf("canonical wish status=%q want confirmed", wish.Status)
	}
}

func TestSQLiteCreateProposalDoesNotSupersedeConfirmed(t *testing.T) {
	store := newTestSQLiteStore(t)
	ctx := context.Background()
	hash := strings.Repeat("ab", 32)
	wishID := "wish-" + hash

	if err := store.UpsertContractWithTasks(ctx, core.Contract{
		ContractID: wishID,
		Title:      "Confirmed wish",
		Status:     "confirmed",
	}, nil); err != nil {
		t.Fatalf("seed: %v", err)
	}

	err := store.CreateProposal(ctx, core.Proposal{
		ID:               "attacker-proposal",
		Title:            "Hijack",
		Status:           "approved",
		VisiblePixelHash: hash,
		CreatedAt:        time.Now(),
		Metadata: map[string]interface{}{
			"visible_pixel_hash": hash,
		},
	})
	if err != nil {
		t.Fatalf("create proposal: %v", err)
	}

	c, err := store.GetContract(wishID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if c.Status != "confirmed" {
		t.Fatalf("confirmed wish demoted to %s", c.Status)
	}
}

func TestSQLiteUpsertPreservesConfirmedFields(t *testing.T) {
	store := newTestSQLiteStore(t)
	ctx := context.Background()
	id := "wish-" + strings.Repeat("cd", 32)

	if err := store.UpsertContractWithTasks(ctx, core.Contract{
		ContractID:      id,
		Title:           "Original",
		TotalBudgetSats: 5000,
		Status:          "confirmed",
	}, nil); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := store.UpsertContractWithTasks(ctx, core.Contract{
		ContractID:      id,
		Title:           "Hijacked",
		TotalBudgetSats: 1,
		Status:          "funded",
		StegoImageURL:   "/stego/x.png",
	}, nil); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	c, err := store.GetContract(id)
	if err != nil {
		t.Fatal(err)
	}
	if c.Title != "Original" || c.TotalBudgetSats != 5000 || c.Status != "confirmed" {
		t.Fatalf("protected fields changed: %+v", c)
	}
	if c.StegoImageURL != "/stego/x.png" {
		t.Fatalf("expected stego url refresh, got %q", c.StegoImageURL)
	}
}
