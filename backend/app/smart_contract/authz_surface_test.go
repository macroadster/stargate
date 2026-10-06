package smart_contract

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"stargate-backend/core/smart_contract"
	"stargate-backend/storage/ingestion"
)

// These exercise the REST route rather than AuthorizeSubmissionReview directly.
// The helper tests passed while the MCP surface still bypassed the service, so
// the rule has to be checked where a caller actually reaches it.

// postReview drives POST /api/smart_contract/submissions/<id>/review through the
// dispatcher, the way an HTTP client would.
func postReview(t *testing.T, srv *Server, apiKey, submissionID, action string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost,
		"/api/smart_contract/submissions/"+submissionID+"/review",
		strings.NewReader(`{"action":"`+action+`"}`))
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
	rec := httptest.NewRecorder()
	srv.handleSubmissions(rec, req)
	return rec
}

func TestRESTSubmissionReviewRejectsStranger(t *testing.T) {
	srv, store := authzFixture(t)
	seedSubmission(t, store, "rest-sub-1", "rest-task-1")

	rec := postReview(t, srv, testStrangerKey, "rest-sub-1", "approve")

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for a wallet that does not own the wish, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "does not match wish creator") {
		t.Fatalf("expected a creator mismatch message, got: %s", rec.Body.String())
	}

	// The denial must be real, not just a status code: status is unchanged.
	sub, err := store.GetSubmission(context.Background(), "rest-sub-1")
	if err != nil {
		t.Fatalf("get submission: %v", err)
	}
	if sub.Status == "approved" {
		t.Fatal("submission was approved despite the 403")
	}
}

func TestRESTSubmissionReviewAllowsWishCreator(t *testing.T) {
	srv, store := authzFixture(t)
	seedSubmission(t, store, "rest-sub-2", "rest-task-2")

	rec := postReview(t, srv, testCreatorKey, "rest-sub-2", "approve")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for the wish creator, got %d: %s", rec.Code, rec.Body.String())
	}
	sub, err := store.GetSubmission(context.Background(), "rest-sub-2")
	if err != nil {
		t.Fatalf("get submission: %v", err)
	}
	if sub.Status != "approved" {
		t.Fatalf("submission status = %q, want approved", sub.Status)
	}
}

// A replicated wish records no creator_wallet, so review must be refused rather
// than allowed through the proposal-compatibility branch.
func TestRESTSubmissionReviewDeniesReplicatedWish(t *testing.T) {
	srv, store := authzFixture(t)
	ctx := context.Background()

	const replicated = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	if err := srv.ingestionSvc.Create(ingestion.IngestionRecord{
		ID:        replicated,
		Filename:  "stego.png",
		Status:    "verified",
		CreatedAt: time.Now(),
		Metadata:  map[string]interface{}{"stego_replicated": true},
	}); err != nil {
		t.Fatalf("seed replicated ingestion: %v", err)
	}

	contractID := "wish-" + replicated
	if err := store.UpsertContractWithTasks(ctx,
		smart_contract.Contract{ContractID: contractID, Title: "replicated wish", Status: "active"},
		[]smart_contract.Task{{TaskID: "rep-task", ContractID: contractID, Title: "task", Status: "submitted"}},
	); err != nil {
		t.Fatalf("seed replicated contract: %v", err)
	}
	if err := store.SyncSubmission(ctx, smart_contract.Submission{
		SubmissionID: "rest-sub-replicated",
		TaskID:       "rep-task",
		Status:       "pending_review",
		CreatedAt:    time.Now(),
	}); err != nil {
		t.Fatalf("seed submission: %v", err)
	}

	rec := postReview(t, srv, testCreatorKey, "rest-sub-replicated", "approve")

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on a replicated wish, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "replicated from another node") {
		t.Fatalf("expected the replica cause to be named, got: %s", rec.Body.String())
	}
}
