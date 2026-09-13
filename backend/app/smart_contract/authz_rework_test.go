package smart_contract

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"stargate-backend/core/smart_contract"
	scstore "stargate-backend/storage/smart_contract"
)

// hs2: rework had no authorization and reset any status to pending_review. The
// service tests cover what Rework does with an authorization result; these cover
// the claimant rule itself and the REST route that reaches it.

// seedClaimedSubmission creates a task, a claim owned by claimant, and a
// submission on it. The claim is what records who may rework.
func seedClaimedSubmission(t *testing.T, store scstore.Store, submissionID, claimant, status string) {
	t.Helper()
	ctx := context.Background()
	contractID := "wish-" + testWishHash
	taskID := "task-" + submissionID
	claimID := "claim-" + submissionID

	if err := store.UpsertContractWithTasks(ctx,
		smart_contract.Contract{ContractID: contractID, Title: "wish", Status: "active"},
		[]smart_contract.Task{{TaskID: taskID, ContractID: contractID, Title: "task", Status: "submitted"}},
	); err != nil {
		t.Fatalf("seed contract: %v", err)
	}
	if err := store.SyncClaim(ctx, smart_contract.Claim{
		ClaimID:      claimID,
		TaskID:       taskID,
		AiIdentifier: claimant,
		Status:       "submitted",
		CreatedAt:    time.Now(),
	}); err != nil {
		t.Fatalf("seed claim: %v", err)
	}
	if err := store.SyncSubmission(ctx, smart_contract.Submission{
		SubmissionID: submissionID,
		ClaimID:      claimID,
		TaskID:       taskID,
		Status:       status,
		Deliverables: map[string]interface{}{"url": "original-work"},
		CreatedAt:    time.Now(),
	}); err != nil {
		t.Fatalf("seed submission: %v", err)
	}
}

func TestReworkGateAllowsOnlyTheClaimant(t *testing.T) {
	srv, store := authzFixture(t)
	// testStrangerWlt holds the claim, so the creator key is the outsider here:
	// being the wish creator is authority to review, not to edit the work.
	seedClaimedSubmission(t, store, "sub-gate", testStrangerWlt, "pending_review")
	gate := srv.reworkGate()

	wallet, err := gate.AuthorizeSubmissionRework(context.Background(), testStrangerKey, "sub-gate")
	if err != nil {
		t.Fatalf("the claimant must be authorized, got %v", err)
	}
	if wallet != testStrangerWlt {
		t.Fatalf("authorized wallet = %q, want the claimant %q", wallet, testStrangerWlt)
	}

	if _, err := gate.AuthorizeSubmissionRework(context.Background(), testCreatorKey, "sub-gate"); err == nil {
		t.Fatal("the wish creator is not the claimant and must not be authorized to rework")
	}
}

func TestReworkGateDeniesWhenClaimantCannotBeEstablished(t *testing.T) {
	srv, store := authzFixture(t)
	if err := store.SyncSubmission(context.Background(), smart_contract.Submission{
		SubmissionID: "sub-noclaim",
		Status:       "pending_review",
		CreatedAt:    time.Now(),
	}); err != nil {
		t.Fatalf("seed submission: %v", err)
	}

	_, err := srv.reworkGate().AuthorizeSubmissionRework(context.Background(), testCreatorKey, "sub-noclaim")
	if err == nil {
		t.Fatal("a submission with no claim has no claimant, so rework must be refused")
	}
	if !strings.Contains(err.Error(), "no claim") {
		t.Fatalf("expected the refusal to name the missing claim, got %v", err)
	}
}

func postRework(t *testing.T, srv *Server, apiKey, submissionID, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost,
		"/api/smart_contract/submissions/"+submissionID+"/rework", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
	rec := httptest.NewRecorder()
	srv.handleSubmissions(rec, req)
	return rec
}

func TestRESTReworkRejectsNonClaimant(t *testing.T) {
	srv, store := authzFixture(t)
	seedClaimedSubmission(t, store, "sub-rest-rework", testStrangerWlt, "pending_review")

	rec := postRework(t, srv, testCreatorKey, "sub-rest-rework",
		`{"deliverables":{"url":"attacker-work"}}`)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for a non-claimant rework, got %d: %s", rec.Code, rec.Body.String())
	}
	sub, err := store.GetSubmission(context.Background(), "sub-rest-rework")
	if err != nil {
		t.Fatalf("get submission: %v", err)
	}
	if got, _ := sub.Deliverables["url"].(string); got != "original-work" {
		t.Fatalf("deliverables were overwritten despite the 403: %q", got)
	}
}

func TestRESTReworkStillAllowsClaimant(t *testing.T) {
	srv, store := authzFixture(t)
	seedClaimedSubmission(t, store, "sub-rest-rework-ok", testStrangerWlt, "pending_review")

	rec := postRework(t, srv, testStrangerKey, "sub-rest-rework-ok",
		`{"deliverables":{"url":"revised-work"}}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("the claimant must still be able to rework, got %d: %s", rec.Code, rec.Body.String())
	}
	sub, err := store.GetSubmission(context.Background(), "sub-rest-rework-ok")
	if err != nil {
		t.Fatalf("get submission: %v", err)
	}
	if got, _ := sub.Deliverables["url"].(string); got != "revised-work" {
		t.Fatalf("the claimant's rework did not persist, url = %q", got)
	}
}

// The headline of hs2: an approved submission could be reset to pending_review by
// anyone. Even its own claimant cannot reopen it now.
func TestRESTReworkCannotReopenApprovedSubmission(t *testing.T) {
	srv, store := authzFixture(t)
	seedClaimedSubmission(t, store, "sub-rest-approved", testStrangerWlt, "approved")

	rec := postRework(t, srv, testStrangerKey, "sub-rest-approved", `{"notes":"reopen please"}`)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 when reworking an approved submission, got %d: %s", rec.Code, rec.Body.String())
	}
	sub, err := store.GetSubmission(context.Background(), "sub-rest-approved")
	if err != nil {
		t.Fatalf("get submission: %v", err)
	}
	if sub.Status != "approved" {
		t.Fatalf("an approved submission was moved to %q", sub.Status)
	}
}
