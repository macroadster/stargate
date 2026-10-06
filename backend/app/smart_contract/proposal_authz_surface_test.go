package smart_contract

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"stargate-backend/core/smart_contract"
)

// irl.2 closed proposal approval's fail-open allowance. The unit test covers the
// rule; this covers the REST route, because the allowance lived in the handler
// and a caller has to be refused where it actually arrives.

// unknownWishHash has no ingestion record in authzFixture, which is how a wish
// with missing or partial creator metadata presents itself.
const unknownWishHash = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"

// seedProposalWithoutCreator seeds a proposal whose wish exists as a contract but
// has no creator on record. The contract is seeded deliberately: without it a
// denial could be mistaken for the missing-wish rejection.
func seedProposalWithoutCreator(t *testing.T, store Store, proposalID string) {
	t.Helper()
	ctx := context.Background()
	if err := store.UpsertContractWithTasks(ctx, smart_contract.Contract{
		ContractID: "wish-" + unknownWishHash,
		Title:      "Wish with no creator on record",
		Status:     "pending",
	}, nil); err != nil {
		t.Fatalf("seed wish contract: %v", err)
	}
	if err := store.CreateProposal(ctx, smart_contract.Proposal{
		ID:               proposalID,
		Title:            "Proposal against a creatorless wish",
		DescriptionMD:    "details",
		VisiblePixelHash: unknownWishHash,
		BudgetSats:       1000,
		Status:           "pending",
		Tasks: []smart_contract.Task{{
			TaskID:     proposalID + "-task-1",
			ContractID: proposalID,
			Title:      "Do work",
			BudgetSats: 1000,
			Status:     "available",
		}},
		Metadata: map[string]interface{}{"visible_pixel_hash": unknownWishHash},
	}); err != nil {
		t.Fatalf("seed proposal: %v", err)
	}
}

func postProposalApproval(t *testing.T, srv *Server, apiKey, proposalID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/smart_contract/proposals/"+proposalID+"/approve", nil)
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
	rec := httptest.NewRecorder()
	srv.handleProposals(rec, req)
	return rec
}

// The key here is legitimate and wallet-bound. It is refused because the wish has
// no creator to match, which is exactly the case that used to be waved through.
func TestRESTProposalApprovalDeniesMissingCreator(t *testing.T) {
	srv, store := authzFixture(t)
	seedProposalWithoutCreator(t, store, "rest-prop-nocreator")

	rec := postProposalApproval(t, srv, testCreatorKey, "rest-prop-nocreator")

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when no creator wallet is recorded, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "no creator wallet recorded") {
		t.Fatalf("expected the denial to name the missing creator, got: %s", rec.Body.String())
	}

	// A status code alone would not prove the approval was stopped.
	prop, err := store.GetProposal(context.Background(), "rest-prop-nocreator")
	if err != nil {
		t.Fatalf("get proposal: %v", err)
	}
	if prop.Status == "approved" {
		t.Fatal("proposal was approved despite the 403")
	}
}

// Failing closed must not deny everyone: a wish with a creator on record still
// approves for that creator through the same route.
func TestRESTProposalApprovalStillAllowsCreator(t *testing.T) {
	srv, store := authzFixture(t)
	ctx := context.Background()
	if err := store.UpsertContractWithTasks(ctx, smart_contract.Contract{
		ContractID: "wish-" + testWishHash,
		Title:      "Wish with a creator",
		Status:     "pending",
	}, nil); err != nil {
		t.Fatalf("seed wish contract: %v", err)
	}
	if err := store.CreateProposal(ctx, smart_contract.Proposal{
		ID:               "rest-prop-creator",
		Title:            "Proposal against an owned wish",
		DescriptionMD:    "details",
		VisiblePixelHash: testWishHash,
		BudgetSats:       1000,
		Status:           "pending",
		Tasks: []smart_contract.Task{{
			TaskID:     "rest-prop-creator-task-1",
			ContractID: "rest-prop-creator",
			Title:      "Do work",
			BudgetSats: 1000,
			Status:     "available",
		}},
		Metadata: map[string]interface{}{"visible_pixel_hash": testWishHash},
	}); err != nil {
		t.Fatalf("seed proposal: %v", err)
	}

	rec := postProposalApproval(t, srv, testCreatorKey, "rest-prop-creator")

	if rec.Code == http.StatusForbidden {
		t.Fatalf("the wish creator must still be able to approve, got 403: %s", rec.Body.String())
	}
}
