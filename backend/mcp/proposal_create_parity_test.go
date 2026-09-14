package mcp

import (
	"context"
	"net/http"
	"testing"

	scservices "stargate-backend/app/smart_contract/services"
	"stargate-backend/core/smart_contract"
	scstore "stargate-backend/storage/smart_contract"
)

// create_proposal built the proposal itself and called store.CreateProposal
// directly, so it diverged from ProposalService.Create on the shape of
// contract_id, on emitting a proposal_create event at all, and on how the store's
// refusals were recognised (stargate-fhz). The whole suite passed before these
// tests existed, in both shapes, which is why the drift was never noticed.

const createWishBudget = int64(5000)

// seedCreatableWish creates the wish contract create_proposal proposes against,
// carrying a budget so wish_budget_sats is exercised.
func seedCreatableWish(t *testing.T, store scstore.Store) {
	t.Helper()
	if err := store.UpsertContractWithTasks(context.Background(), smart_contract.Contract{
		ContractID:      "wish-" + surfaceWishHash,
		Title:           "Wish",
		Status:          "pending",
		TotalBudgetSats: createWishBudget,
	}, nil); err != nil {
		t.Fatalf("seed wish contract: %v", err)
	}
}

func createProposalArgs(budgetSats int64) map[string]interface{} {
	args := map[string]interface{}{
		"title":              "Proposal",
		"description_md":     "- [ ] Do the thing",
		"visible_pixel_hash": surfaceWishHash,
	}
	if budgetSats > 0 {
		args["budget_sats"] = budgetSats
	}
	return args
}

// storedProposalFor returns the single proposal written for the fixture wish.
func storedProposalFor(t *testing.T, store scstore.Store) smart_contract.Proposal {
	t.Helper()
	proposals, err := store.ListProposals(context.Background(), smart_contract.ProposalFilter{})
	if err != nil {
		t.Fatalf("list proposals: %v", err)
	}
	if len(proposals) != 1 {
		t.Fatalf("expected exactly one stored proposal, got %d", len(proposals))
	}
	return proposals[0]
}

func TestMCPCreateProposalStoresContractIDAsTheBareHash(t *testing.T) {
	srv, store := surfaceFixture(t)
	seedCreatableWish(t, store)

	resp := callTool(t, srv, surfaceCreatorKey, "create_proposal", createProposalArgs(0))
	if !resp.Success {
		t.Fatalf("create_proposal failed: %s", resp.Error)
	}

	stored := storedProposalFor(t, store)
	contractID, _ := stored.Metadata["contract_id"].(string)

	// This surface stored "wish-"+hash here while REST stored the bare hash, for
	// the same field on the same kind of object. The service rejects a
	// contract_id that is not equal to visible_pixel_hash, so routing through it
	// settles the disagreement on the bare hash; the "wish-" prefix belongs to
	// the contract row id, which is unchanged. Proposals written before this keep
	// their prefixed value and are not migrated.
	if contractID != surfaceWishHash {
		t.Fatalf("metadata contract_id = %q, want the bare hash %q", contractID, surfaceWishHash)
	}
	if hash, _ := stored.Metadata["visible_pixel_hash"].(string); hash != surfaceWishHash {
		t.Fatalf("metadata visible_pixel_hash = %q, want %q", hash, surfaceWishHash)
	}
}

func TestMCPCreateProposalEmitsCreateEventNamingTheWallet(t *testing.T) {
	srv, store := surfaceFixture(t)
	seedCreatableWish(t, store)

	events := captureEvents(t)

	resp := callTool(t, srv, surfaceCreatorKey, "create_proposal", createProposalArgs(0))
	if !resp.Success {
		t.Fatalf("create_proposal failed: %s", resp.Error)
	}

	// Creating over this surface used to emit nothing, so a proposal could appear
	// with no record of who created it.
	stored := storedProposalFor(t, store)
	evt := awaitEvent(t, events, "proposal_create", stored.ID)
	if evt.Actor != surfaceCreatorWlt {
		t.Fatalf("event actor = %q, want the creating wallet %q", evt.Actor, surfaceCreatorWlt)
	}
}

func TestMCPCreateProposalKeepsItsResponseShape(t *testing.T) {
	srv, store := surfaceFixture(t)
	seedCreatableWish(t, store)

	resp := callTool(t, srv, surfaceCreatorKey, "create_proposal", createProposalArgs(0))
	if !resp.Success {
		t.Fatalf("create_proposal failed: %s", resp.Error)
	}

	// The service answers with counts, this surface with the proposal and its
	// allocation totals. Clients read these three fields, so they are preserved
	// as a wrapper rather than replaced by the service's payload.
	data, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("data = %T, want an object", resp.Result)
	}
	for _, field := range []string{"proposal", "task_count", "allocated_sats", "wish_budget_sats"} {
		if _, present := data[field]; !present {
			t.Fatalf("response is missing %q (got fields %v)", field, data)
		}
	}
	if got := int64(data["wish_budget_sats"].(float64)); got != createWishBudget {
		t.Fatalf("wish_budget_sats = %d, want %d", got, createWishBudget)
	}

	// allocated_sats and task_count describe the row that was written, so they
	// are read back from it rather than from the values used to build it.
	stored := storedProposalFor(t, store)
	var allocated int64
	for _, task := range stored.Tasks {
		allocated += task.BudgetSats
	}
	if got := int(data["task_count"].(float64)); got != len(stored.Tasks) {
		t.Fatalf("task_count = %d, want %d", got, len(stored.Tasks))
	}
	if got := int64(data["allocated_sats"].(float64)); got != allocated {
		t.Fatalf("allocated_sats = %d, want %d", got, allocated)
	}
}

func TestMCPCreateProposalStillRefusesBudgetOverWish(t *testing.T) {
	srv, store := surfaceFixture(t)
	seedCreatableWish(t, store)

	resp := callTool(t, srv, surfaceCreatorKey, "create_proposal", createProposalArgs(createWishBudget+1))
	if resp.Success {
		t.Fatal("a proposal budget over the wish budget must be refused")
	}
	// The cap is enforced by the service now, but the code this surface answers
	// with is unchanged.
	if want := "CREATE_PROPOSAL_BUDGET_EXCEEDED"; resp.ErrorCode != want {
		t.Fatalf("error_code = %q, want %q (body: %s)", resp.ErrorCode, want, resp.Error)
	}
	if proposals, err := store.ListProposals(context.Background(), smart_contract.ProposalFilter{}); err != nil {
		t.Fatalf("list proposals: %v", err)
	} else if len(proposals) != 0 {
		t.Fatalf("a refused create wrote %d proposals", len(proposals))
	}
}

func TestMCPCreateProposalStillReportsLimitReached(t *testing.T) {
	srv, store := surfaceFixture(t)
	seedCreatableWish(t, store)

	for i := 0; i < scstore.MaxProposalsPerWish; i++ {
		resp := callTool(t, srv, surfaceCreatorKey, "create_proposal", createProposalArgs(0))
		if !resp.Success {
			t.Fatalf("create %d of %d failed: %s", i+1, scstore.MaxProposalsPerWish, resp.Error)
		}
	}

	resp := callTool(t, srv, surfaceCreatorKey, "create_proposal", createProposalArgs(0))
	if resp.Success {
		t.Fatalf("create %d must be refused by the per-wish cap", scstore.MaxProposalsPerWish+1)
	}
	// Recovered from the store's sentinel rather than from its message text, so
	// rewording the store cannot silently downgrade this to an internal error.
	if want := "CREATE_PROPOSAL_LIMIT_REACHED"; resp.ErrorCode != want {
		t.Fatalf("error_code = %q, want %q (body: %s)", resp.ErrorCode, want, resp.Error)
	}
}

func TestMCPCreateProposalStillReportsAlreadyFinalized(t *testing.T) {
	srv, store := surfaceFixture(t)
	seedCreatableWish(t, store)

	if err := store.CreateProposal(context.Background(), smart_contract.Proposal{
		ID:               "mcp-prop-approved",
		Title:            "Already approved",
		VisiblePixelHash: surfaceWishHash,
		BudgetSats:       createWishBudget,
		Status:           "approved",
		Metadata:         map[string]interface{}{"visible_pixel_hash": surfaceWishHash},
	}); err != nil {
		t.Fatalf("seed approved proposal: %v", err)
	}

	resp := callTool(t, srv, surfaceCreatorKey, "create_proposal", createProposalArgs(0))
	if resp.Success {
		t.Fatal("a wish with an approved proposal must not accept another")
	}
	if want := "CREATE_PROPOSAL_ALREADY_FINALIZED"; resp.ErrorCode != want {
		t.Fatalf("error_code = %q, want %q (body: %s)", resp.ErrorCode, want, resp.Error)
	}
}

// TestCreateProposalErrorMapsEveryKind covers the mapping directly because one of
// the four codes cannot be provoked through this surface: create_proposal builds
// its tasks from markdown, so their budgets always sum, and only a caller
// supplying its own task budgets reaches the store's mismatch refusal. REST can.
// The code is still part of this surface's contract, so the mapping is pinned
// even where the path is not reachable from here.
func TestCreateProposalErrorMapsEveryKind(t *testing.T) {
	srv, _ := surfaceFixture(t)

	cases := []struct {
		kind scservices.Kind
		want string
	}{
		{scservices.KindBudgetExceeded, "CREATE_PROPOSAL_BUDGET_EXCEEDED"},
		{scservices.KindBudgetMismatch, "CREATE_PROPOSAL_BUDGET_MISMATCH"},
		{scservices.KindProposalLimitReached, "CREATE_PROPOSAL_LIMIT_REACHED"},
		{scservices.KindProposalAlreadyFinalized, "CREATE_PROPOSAL_ALREADY_FINALIZED"},
		{scservices.KindWishNotFound, ErrCodeNotFound},
	}

	for _, tc := range cases {
		t.Run(string(tc.kind), func(t *testing.T) {
			err := srv.createProposalError(surfaceWishHash,
				scservices.FailKind(http.StatusBadRequest, tc.kind, "refused"))
			toolErr, ok := err.(*ToolError)
			if !ok {
				t.Fatalf("error is %T, want *ToolError", err)
			}
			if toolErr.Code != tc.want {
				t.Fatalf("code = %q, want %q", toolErr.Code, tc.want)
			}
		})
	}
}

func TestMCPCreateProposalReportsMissingWish(t *testing.T) {
	srv, _ := surfaceFixture(t)

	// No wish contract is seeded. The service answers KindWishNotFound and this
	// surface keeps reporting it as an absent wish, not as a bad request.
	resp := callTool(t, srv, surfaceCreatorKey, "create_proposal", createProposalArgs(0))
	if resp.Success {
		t.Fatal("proposing against a wish that does not exist must fail")
	}
	if resp.ErrorCode != ErrCodeNotFound {
		t.Fatalf("error_code = %q, want %q (body: %s)", resp.ErrorCode, ErrCodeNotFound, resp.Error)
	}
}
