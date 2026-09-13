package services

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	core "stargate-backend/core/smart_contract"
	scstore "stargate-backend/storage/smart_contract"
)

// These tests call Review directly, with no handler involved. That is the point:
// authorization used to live in the REST and MCP handlers, so the service would
// approve a payout for anyone who called it. A stub authorizer keeps the subject
// here to what Review does with an authorization result; the wish creator rule
// itself is covered by authz_test.go in the parent package.

type stubReviewAuthorizer struct {
	wallet string
	err    error

	calls  int
	gotKey string
	gotSub string
}

func (s *stubReviewAuthorizer) AuthorizeSubmissionReview(_ context.Context, apiKey, submissionID string) (string, error) {
	s.calls++
	s.gotKey, s.gotSub = apiKey, submissionID
	if s.err != nil {
		return "", s.err
	}
	return s.wallet, nil
}

func seedReviewSubmission(t *testing.T, store scstore.Store, submissionID string) {
	t.Helper()
	if err := store.SyncSubmission(context.Background(), core.Submission{
		SubmissionID: submissionID,
		TaskID:       "task-" + submissionID,
		Status:       "pending_review",
		CreatedAt:    time.Now(),
	}); err != nil {
		t.Fatalf("seed submission: %v", err)
	}
}

func assertSubmissionStatus(t *testing.T, store scstore.Store, submissionID, want string) {
	t.Helper()
	sub, err := store.GetSubmission(context.Background(), submissionID)
	if err != nil {
		t.Fatalf("get submission: %v", err)
	}
	if sub.Status != want {
		t.Fatalf("status = %q, want %q", sub.Status, want)
	}
}

// An unwired authorizer must refuse rather than fall through to the mutation:
// that difference is a guarded payout versus an open one.
func TestReviewRefusesWhenAuthorizerMissing(t *testing.T) {
	store := scstore.NewMemoryStore(time.Hour)
	seedReviewSubmission(t, store, "sub-noauthz")
	svc := NewSubmissionService(store, nil, nil, nil)

	_, err := svc.Review(context.Background(), "sub-noauthz", SubmissionReviewInput{Action: "approve"}, ReviewActor{APIKey: "any-key"})
	if err == nil {
		t.Fatal("expected a service with no authorizer to refuse the review, got nil")
	}
	if se := AsStatus(err); se == nil || se.Status != http.StatusInternalServerError {
		t.Fatalf("expected 500 for a wiring failure, got %v", err)
	}
	assertSubmissionStatus(t, store, "sub-noauthz", "pending_review")
}

func TestReviewDeniesWhenAuthorizerRefuses(t *testing.T) {
	store := scstore.NewMemoryStore(time.Hour)
	seedReviewSubmission(t, store, "sub-denied")

	var events []core.Event
	svc := NewSubmissionService(store, func(evt core.Event) { events = append(events, evt) },
		&stubReviewAuthorizer{err: errors.New("approver wallet bc1qstranger does not match wish creator")}, nil)

	_, err := svc.Review(context.Background(), "sub-denied", SubmissionReviewInput{Action: "approve"}, ReviewActor{APIKey: "stranger-key"})
	if err == nil {
		t.Fatal("expected a refused authorization to deny the review, got nil")
	}
	if se := AsStatus(err); se == nil || se.Status != http.StatusForbidden {
		t.Fatalf("expected 403, got %v", err)
	}

	// The denial has to precede the mutation, not merely be reported after it.
	assertSubmissionStatus(t, store, "sub-denied", "pending_review")
	if len(events) != 0 {
		t.Fatalf("a denied review must not emit events, got %+v", events)
	}
}

// Gating only "approve" would leave reject and review open to strangers, so
// every action must go through the authorizer.
func TestReviewAuthorizesEveryAction(t *testing.T) {
	for _, action := range []string{"review", "approve", "reject"} {
		t.Run(action, func(t *testing.T) {
			store := scstore.NewMemoryStore(time.Hour)
			seedReviewSubmission(t, store, "sub-"+action)
			stub := &stubReviewAuthorizer{err: errors.New("denied")}
			svc := NewSubmissionService(store, nil, stub, nil)

			_, err := svc.Review(context.Background(), "sub-"+action, SubmissionReviewInput{Action: action}, ReviewActor{APIKey: "stranger-key"})
			if err == nil {
				t.Fatalf("action %q was not authorized", action)
			}
			if stub.calls != 1 {
				t.Fatalf("authorizer called %d times for %q, want 1", stub.calls, action)
			}
			assertSubmissionStatus(t, store, "sub-"+action, "pending_review")
		})
	}
}

// Approving releases funds, so the event has to name the wallet that was
// authorized. It previously recorded the literal "reviewer" for every reviewer.
func TestReviewRecordsAuthorizedWalletAsActor(t *testing.T) {
	const wallet = "bc1qcreatorwallet"
	store := scstore.NewMemoryStore(time.Hour)
	seedReviewSubmission(t, store, "sub-actor")

	var events []core.Event
	svc := NewSubmissionService(store, func(evt core.Event) { events = append(events, evt) },
		&stubReviewAuthorizer{wallet: wallet}, nil)

	if _, err := svc.Review(context.Background(), "sub-actor", SubmissionReviewInput{Action: "approve"}, ReviewActor{APIKey: "creator-key"}); err != nil {
		t.Fatalf("expected an authorized review to succeed, got %v", err)
	}

	var review *core.Event
	for i := range events {
		if events[i].Type == "review" {
			review = &events[i]
		}
	}
	if review == nil {
		t.Fatalf("no review event emitted, got %+v", events)
	}
	if review.Actor != wallet {
		t.Fatalf("event actor = %q, want the authorized wallet %q", review.Actor, wallet)
	}
	assertSubmissionStatus(t, store, "sub-actor", "approved")
}

// The key the caller supplies must reach the authorizer, and the wallet in the
// event must come from the authorizer's answer rather than from the caller.
func TestReviewPassesActorKeyToAuthorizer(t *testing.T) {
	store := scstore.NewMemoryStore(time.Hour)
	seedReviewSubmission(t, store, "sub-threaded")
	stub := &stubReviewAuthorizer{wallet: "bc1qauthorized"}
	svc := NewSubmissionService(store, nil, stub, nil)

	if _, err := svc.Review(context.Background(), "sub-threaded", SubmissionReviewInput{Action: "approve"}, ReviewActor{APIKey: "creator-key"}); err != nil {
		t.Fatalf("review: %v", err)
	}
	if stub.gotKey != "creator-key" {
		t.Fatalf("authorizer saw api key %q, want %q", stub.gotKey, "creator-key")
	}
	if stub.gotSub != "sub-threaded" {
		t.Fatalf("authorizer saw submission %q, want %q", stub.gotSub, "sub-threaded")
	}
}
