package services

import (
	"context"
	"net/http"
	"testing"

	core "stargate-backend/core/smart_contract"
	scstore "stargate-backend/storage/smart_contract"
)

// The store's create refusals carry sentinels so callers can tell them apart
// without matching message text. Both stores then wrapped the validation error
// with %v, which severs the chain: the sentinel's text was still in the message,
// so this looked correct, while errors.Is returned false and the Kind came back
// empty. Reported in review of stargate-fhz.

const createKindWishHash = "b7c8d9e0f1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8"

func seedCreateKindWish(t *testing.T, store scstore.Store, budgetSats int64) {
	t.Helper()
	if err := store.UpsertContractWithTasks(context.Background(), core.Contract{
		ContractID:      "wish-" + createKindWishHash,
		Title:           "Wish",
		Status:          "pending",
		TotalBudgetSats: budgetSats,
	}, nil); err != nil {
		t.Fatalf("seed wish contract: %v", err)
	}
}

// TestCreateUnderAllocatedTasksCarryBudgetMismatchKind exercises the whole Create
// path rather than the store alone, because the wrap that broke this sits between
// them.
func TestCreateUnderAllocatedTasksCarryBudgetMismatchKind(t *testing.T) {
	store := scstore.NewMemoryStore(72 * 60 * 60)
	seedCreateKindWish(t, store, 1000)
	svc := editService(store, nil, nil)

	// Every task carries an explicit budget, so AllocateTaskBudgets returns them
	// unchanged through its len(unset)==0 path and never distributes the
	// remainder. 100+200 is under 1000, which is what the store refuses. This is
	// the reachable route to that refusal: a caller supplying task budgets that
	// do not add up.
	_, _, err := svc.Create(context.Background(), ProposalCreateInput{
		ID:               "create-kind-under",
		Title:            "Under-allocated",
		VisiblePixelHash: createKindWishHash,
		BudgetSats:       1000,
		Tasks: []core.Task{
			{TaskID: "t1", Title: "A", BudgetSats: 100, Status: "available"},
			{TaskID: "t2", Title: "B", BudgetSats: 200, Status: "available"},
		},
	})
	if err == nil {
		t.Fatal("expected task budgets that do not sum to the proposal budget to be refused")
	}

	se := AsStatus(err)
	if se == nil {
		t.Fatalf("error is not a StatusError: %v", err)
	}
	if se.Status != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", se.Status, http.StatusBadRequest)
	}
	// The Kind is the point. Without it a caller has only the status, which this
	// shares with every malformed request.
	if se.Kind != KindBudgetMismatch {
		t.Fatalf("kind = %q, want %q (message: %s)", se.Kind, KindBudgetMismatch, se.Message)
	}
}

// TestCreateStoreErrorClassifiesEverySentinel pins the classifier against the
// store's own errors rather than against constructed ones, so a future wrap that
// severs a chain fails here even if no surface test happens to reach it.
func TestCreateStoreErrorClassifiesEverySentinel(t *testing.T) {
	ctx := context.Background()

	newStoreWithWish := func(t *testing.T) scstore.Store {
		t.Helper()
		store := scstore.NewMemoryStore(72 * 60 * 60)
		seedCreateKindWish(t, store, 1000)
		return store
	}

	proposal := func(id string, tasks []core.Task) core.Proposal {
		return core.Proposal{
			ID:               id,
			Title:            "P",
			VisiblePixelHash: createKindWishHash,
			BudgetSats:       1000,
			Status:           "pending",
			Tasks:            tasks,
			Metadata: map[string]interface{}{
				"contract_id":        createKindWishHash,
				"visible_pixel_hash": createKindWishHash,
			},
		}
	}

	cases := []struct {
		name string
		want Kind
		// provoke returns the store error to classify.
		provoke func(t *testing.T) error
	}{
		{
			name: "budget mismatch",
			want: KindBudgetMismatch,
			provoke: func(t *testing.T) error {
				store := newStoreWithWish(t)
				return store.CreateProposal(ctx, proposal("mismatch", []core.Task{
					{TaskID: "t1", ContractID: "mismatch", Title: "A", BudgetSats: 100, Status: "available"},
				}))
			},
		},
		{
			name: "limit reached",
			want: KindProposalLimitReached,
			provoke: func(t *testing.T) error {
				store := newStoreWithWish(t)
				for i := 0; i < scstore.MaxProposalsPerWish; i++ {
					if err := store.CreateProposal(ctx, proposal("filler-"+string(rune('a'+i)), nil)); err != nil {
						t.Fatalf("seed proposal %d: %v", i, err)
					}
				}
				return store.CreateProposal(ctx, proposal("over-cap", nil))
			},
		},
		{
			name: "already finalized",
			want: KindProposalAlreadyFinalized,
			provoke: func(t *testing.T) error {
				store := newStoreWithWish(t)
				approved := proposal("approved", nil)
				approved.Status = "approved"
				if err := store.CreateProposal(ctx, approved); err != nil {
					t.Fatalf("seed approved proposal: %v", err)
				}
				return store.CreateProposal(ctx, proposal("late", nil))
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.provoke(t)
			if err == nil {
				t.Fatal("expected the store to refuse this create")
			}
			se := AsStatus(createStoreError(err))
			if se == nil {
				t.Fatalf("classified error is not a StatusError: %v", err)
			}
			if se.Kind != tc.want {
				t.Fatalf("kind = %q, want %q (store said: %v)", se.Kind, tc.want, err)
			}
		})
	}
}
