package smart_contract

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"stargate-backend/core/smart_contract"
)

// irl.7: PATCH and publish reached the store with no authorization at all. Both
// go through the REST route here rather than the service, because that route was
// the hole: publish sat twelve lines below an approve path that did check.

// seedOwnedProposal seeds a proposal against the wish whose creator is
// testCreatorWlt, so a denial can only come from the actor check.
//
// status matters: update requires pending and publish requires approved, so each
// test seeds the state where its request would otherwise succeed. A refusal then
// isolates authorization instead of colliding with the status gate.
func seedOwnedProposal(t *testing.T, store Store, proposalID, status string) {
	t.Helper()
	ctx := context.Background()
	if err := store.UpsertContractWithTasks(ctx, smart_contract.Contract{
		ContractID: "wish-" + testWishHash,
		Title:      "Wish with a creator",
		Status:     "pending",
	}, nil); err != nil {
		t.Fatalf("seed wish contract: %v", err)
	}
	if err := store.CreateProposal(ctx, smart_contract.Proposal{
		ID:               proposalID,
		Title:            "Original title",
		DescriptionMD:    "details",
		VisiblePixelHash: testWishHash,
		BudgetSats:       1000,
		Status:           status,
		Metadata:         map[string]interface{}{"visible_pixel_hash": testWishHash},
	}); err != nil {
		t.Fatalf("seed proposal: %v", err)
	}
}

func patchProposal(t *testing.T, srv *Server, apiKey, proposalID, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPatch, "/api/smart_contract/proposals/"+proposalID, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
	rec := httptest.NewRecorder()
	srv.handleProposals(rec, req)
	return rec
}

func publishProposal(t *testing.T, srv *Server, apiKey, proposalID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/smart_contract/proposals/"+proposalID+"/publish", nil)
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
	rec := httptest.NewRecorder()
	srv.handleProposals(rec, req)
	return rec
}

// A stranger's key is legitimate and wallet-bound; it is simply not the creator's.
// budget_sats is the field under test on purpose: rewriting it is money-adjacent,
// and being pending was previously the only thing the handler asked about.
func TestRESTProposalUpdateRejectsStranger(t *testing.T) {
	srv, store := authzFixture(t)
	seedOwnedProposal(t, store, "rest-prop-update", "pending")

	rec := patchProposal(t, srv, testStrangerKey, "rest-prop-update", `{"title":"Hijacked","budget_sats":999999}`)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for a non-creator update, got %d: %s", rec.Code, rec.Body.String())
	}

	// The status code alone would not prove the write was stopped.
	prop, err := store.GetProposal(context.Background(), "rest-prop-update")
	if err != nil {
		t.Fatalf("get proposal: %v", err)
	}
	if prop.Title != "Original title" {
		t.Fatalf("title was rewritten despite the 403: %q", prop.Title)
	}
	if prop.BudgetSats != 1000 {
		t.Fatalf("budget_sats was rewritten despite the 403: %d", prop.BudgetSats)
	}
}

func TestRESTProposalUpdateStillAllowsCreator(t *testing.T) {
	srv, store := authzFixture(t)
	seedOwnedProposal(t, store, "rest-prop-update-ok", "pending")

	rec := patchProposal(t, srv, testCreatorKey, "rest-prop-update-ok", `{"title":"Revised by creator"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("the creator must still be able to update, got %d: %s", rec.Code, rec.Body.String())
	}
	prop, err := store.GetProposal(context.Background(), "rest-prop-update-ok")
	if err != nil {
		t.Fatalf("get proposal: %v", err)
	}
	if prop.Title != "Revised by creator" {
		t.Fatalf("creator's update did not persist, title is %q", prop.Title)
	}
}

func TestRESTProposalPublishRejectsStranger(t *testing.T) {
	srv, store := authzFixture(t)
	seedOwnedProposal(t, store, "rest-prop-publish", "approved")

	rec := publishProposal(t, srv, testStrangerKey, "rest-prop-publish")

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for a non-creator publish, got %d: %s", rec.Code, rec.Body.String())
	}
	prop, err := store.GetProposal(context.Background(), "rest-prop-publish")
	if err != nil {
		t.Fatalf("get proposal: %v", err)
	}
	if strings.EqualFold(prop.Status, "published") {
		t.Fatal("proposal was published despite the 403")
	}
}

func TestRESTProposalPublishStillAllowsCreator(t *testing.T) {
	srv, store := authzFixture(t)
	seedOwnedProposal(t, store, "rest-prop-publish-ok", "approved")

	rec := publishProposal(t, srv, testCreatorKey, "rest-prop-publish-ok")

	if rec.Code != http.StatusOK {
		t.Fatalf("the creator must still be able to publish, got %d: %s", rec.Code, rec.Body.String())
	}
}

// A wish with no creator on record denies here too, matching irl.2's rule rather
// than introducing a softer one for edits.
func TestRESTProposalEditDeniesMissingCreator(t *testing.T) {
	srv, store := authzFixture(t)
	seedProposalWithoutCreator(t, store, "rest-prop-edit-nocreator")

	rec := patchProposal(t, srv, testCreatorKey, "rest-prop-edit-nocreator", `{"title":"Whoever"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when no creator wallet is recorded, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "no creator wallet recorded") {
		t.Fatalf("expected the denial to name the missing creator, got: %s", rec.Body.String())
	}
}
