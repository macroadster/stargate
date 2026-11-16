package smart_contract

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	core "stargate-backend/core/smart_contract"
	scstore "stargate-backend/storage/smart_contract"
)

type stubFundingProvider struct {
	proof *core.MerkleProof
	err   error
	calls int
	tasks []string
}

func (s *stubFundingProvider) FetchProof(ctx context.Context, task core.Task) (*core.MerkleProof, error) {
	s.calls++
	s.tasks = append(s.tasks, task.TaskID)
	if s.err != nil {
		return nil, s.err
	}
	if s.proof == nil {
		return nil, errors.New("no proof")
	}
	p := *s.proof
	return &p, nil
}

func seedTaskWithProof(t *testing.T, store *scstore.MemoryStore, taskID string, proof *core.MerkleProof) {
	t.Helper()
	ctx := context.Background()
	contract := core.Contract{
		ContractID:          "contract-" + taskID,
		Status:              "funded",
		TotalBudgetSats:     100,
		AvailableTasksCount: 1,
	}
	task := core.Task{
		TaskID:      taskID,
		ContractID:  contract.ContractID,
		Title:       "Task " + taskID,
		BudgetSats:  100,
		Status:      "claimed",
		MerkleProof: proof,
	}
	if err := store.UpsertContractWithTasks(ctx, contract, []core.Task{task}); err != nil {
		t.Fatalf("seed contract and task: %v", err)
	}
}

func TestRefreshProofsPromotesAtSettlementDepth(t *testing.T) {
	t.Setenv("CHAIN_SETTLEMENT_CONFIRMATIONS", "1")

	store := scstore.NewMemoryStore(0)
	proof := &core.MerkleProof{
		TxID:               "tx-promote-1",
		BlockHeight:        100,
		ConfirmationStatus: "provisional",
	}
	seedTaskWithProof(t, store, "task-promote-1", proof)

	provider := &stubFundingProvider{}
	err := refreshProofs(context.Background(), store, provider, nil, func() int64 { return 100 })
	if err != nil {
		t.Fatalf("refreshProofs: %v", err)
	}

	got, err := store.GetTask("task-promote-1")
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if got.MerkleProof == nil {
		t.Fatal("expected merkle_proof")
	}
	if got.MerkleProof.ConfirmationStatus != "confirmed" {
		t.Fatalf("status = %q, want confirmed", got.MerkleProof.ConfirmationStatus)
	}
	if got.MerkleProof.ConfirmedAt == nil {
		t.Fatal("ConfirmedAt not set on promotion")
	}
	if got.MerkleProof.SweepStatus != "direct" {
		t.Fatalf("SweepStatus = %q, want direct", got.MerkleProof.SweepStatus)
	}
	for _, id := range provider.tasks {
		if id == "task-promote-1" {
			t.Fatal("provider consulted for local settlement promotion, want 0 calls for this task")
		}
	}
}

func TestRefreshProofsNeverConfirmsOnProviderAlone(t *testing.T) {
	t.Setenv("CHAIN_SETTLEMENT_CONFIRMATIONS", "20")

	store := scstore.NewMemoryStore(0)
	proof := &core.MerkleProof{
		TxID:               "tx-gate-1",
		BlockHeight:        100,
		ConfirmationStatus: "provisional",
		SeenAt:             time.Now(),
	}
	seedTaskWithProof(t, store, "task-gate-1", proof)

	// Provider claims the tx is confirmed at height 100 (single confirmation
	// style); the local tip is only at 105, short of the 20-block settlement depth.
	provider := &stubFundingProvider{proof: &core.MerkleProof{
		TxID:               "tx-gate-1",
		BlockHeight:        100,
		ConfirmationStatus: "confirmed",
		ConfirmedAt:        ptrTime(time.Now()),
		SeenAt:             time.Now(),
	}}
	err := refreshProofs(context.Background(), store, provider, nil, func() int64 { return 105 })
	if err != nil {
		t.Fatalf("refreshProofs: %v", err)
	}
	got, err := store.GetTask("task-gate-1")
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if got.MerkleProof.ConfirmationStatus != "provisional" {
		t.Fatalf("status = %q at tip=105 with 20 confs required, want provisional", got.MerkleProof.ConfirmationStatus)
	}
	if got.MerkleProof.ConfirmedAt != nil {
		t.Fatal("ConfirmedAt set before settlement depth")
	}

	// Once the local tip reaches settlement depth, the same provider claim promotes.
	err = refreshProofs(context.Background(), store, provider, nil, func() int64 { return 119 })
	if err != nil {
		t.Fatalf("refreshProofs at depth: %v", err)
	}
	got, err = store.GetTask("task-gate-1")
	if err != nil {
		t.Fatalf("get task at depth: %v", err)
	}
	if got.MerkleProof.ConfirmationStatus != "confirmed" {
		t.Fatalf("status = %q at tip=119 with 20 confs required, want confirmed", got.MerkleProof.ConfirmationStatus)
	}
}

func TestRefreshProofsUnknownTipNeverConfirms(t *testing.T) {
	t.Setenv("CHAIN_SETTLEMENT_CONFIRMATIONS", "1")

	store := scstore.NewMemoryStore(0)
	proof := &core.MerkleProof{
		TxID:               "tx-unknown-1",
		BlockHeight:        100,
		ConfirmationStatus: "provisional",
	}
	seedTaskWithProof(t, store, "task-unknown-1", proof)

	provider := &stubFundingProvider{}
	if err := refreshProofs(context.Background(), store, provider, nil, func() int64 { return 0 }); err != nil {
		t.Fatalf("refreshProofs: %v", err)
	}
	got, err := store.GetTask("task-unknown-1")
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if got.MerkleProof.ConfirmationStatus != "provisional" {
		t.Fatalf("status = %q with unknown tip, want provisional", got.MerkleProof.ConfirmationStatus)
	}
}

func TestMockFundingProviderDoesNotManufactureConfirmations(t *testing.T) {
	p := NewMockFundingProvider()

	provisional, err := p.FetchProof(context.Background(), core.Task{
		TaskID:      "task-mock-1",
		MerkleProof: &core.MerkleProof{TxID: "tx-mock", ConfirmationStatus: "provisional"},
	})
	if err != nil {
		t.Fatalf("provisional fetch: %v", err)
	}
	if provisional.ConfirmationStatus != "provisional" {
		t.Fatalf("mock flipped provisional to %q, want provisional", provisional.ConfirmationStatus)
	}

	confirmed, err := p.FetchProof(context.Background(), core.Task{
		TaskID:      "task-mock-2",
		MerkleProof: &core.MerkleProof{TxID: "tx-mock", ConfirmationStatus: "confirmed"},
	})
	if err != nil {
		t.Fatalf("confirmed fetch: %v", err)
	}
	if confirmed.ConfirmationStatus != "confirmed" {
		t.Fatalf("mock lost prior confirmation: %q", confirmed.ConfirmationStatus)
	}
}

func ptrTime(t time.Time) *time.Time { return &t }

func TestGetTaskMerkleProofExposesConfirmationStatus(t *testing.T) {
	t.Setenv("CHAIN_SETTLEMENT_CONFIRMATIONS", "1")

	store := scstore.NewMemoryStore(0)
	confirmedAt := time.Now().UTC().Truncate(time.Second)
	proof := &core.MerkleProof{
		TxID:               "tx-api-1",
		BlockHeight:        100,
		FundedAmountSats:   42000,
		VisiblePixelHash:   "abc123",
		FundingAddress:     "tb1qfunding",
		ConfirmationStatus: "confirmed",
		ConfirmedAt:        &confirmedAt,
		SweepStatus:        "direct",
	}
	seedTaskWithProof(t, store, "task-api-1", proof)

	srv := NewServer(store, nil, nil)

	// REST merkle-proof endpoint returns the confirmed proof.
	req := httptest.NewRequest(http.MethodGet, "/api/smart_contract/tasks/task-api-1/merkle-proof", nil)
	rec := httptest.NewRecorder()
	srv.handleTasks(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("merkle-proof status=%d body=%s, want 200", rec.Code, rec.Body.String())
	}
	var got core.MerkleProof
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode merkle-proof: %v", err)
	}
	if got.ConfirmationStatus != "confirmed" {
		t.Fatalf("confirmation_status = %q, want confirmed", got.ConfirmationStatus)
	}
	if got.SweepStatus != "direct" {
		t.Fatalf("sweep_status = %q, want direct", got.SweepStatus)
	}
	if got.FundedAmountSats != 42000 || got.VisiblePixelHash != "abc123" {
		t.Fatalf("unexpected proof payload: %+v", got)
	}

	// List endpoint exposes the same proof on the task payload.
	req = httptest.NewRequest(http.MethodGet, "/api/smart_contract/tasks?contract_id=contract-task-api-1", nil)
	rec = httptest.NewRecorder()
	srv.handleTasks(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list tasks status=%d body=%s, want 200", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"confirmation_status":"confirmed"`) {
		t.Fatalf("list tasks did not expose confirmation_status=confirmed: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"visible_pixel_hash":"abc123"`) {
		t.Fatalf("list tasks did not expose visible_pixel_hash: %s", rec.Body.String())
	}
}

func TestGetTaskMerkleProofUnknownTask(t *testing.T) {
	store := scstore.NewMemoryStore(0)
	srv := NewServer(store, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/smart_contract/tasks/no-such-task/merkle-proof", nil)
	rec := httptest.NewRecorder()
	srv.handleTasks(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("merkle-proof status=%d body=%s, want 404", rec.Code, rec.Body.String())
	}
}
