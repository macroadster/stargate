package services

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	core "stargate-backend/core/smart_contract"
	scstore "stargate-backend/storage/smart_contract"
)

// hs2: Rework had no authorization and no source-state gate. These call Rework
// directly, since that is where both were missing; the claimant rule itself is
// covered by authz_rework_test.go in the parent package.

type stubReworkAuthorizer struct {
	wallet string
	err    error

	calls  int
	gotKey string
	gotSub string
}

func (s *stubReworkAuthorizer) AuthorizeSubmissionRework(_ context.Context, apiKey, submissionID string) (string, error) {
	s.calls++
	s.gotKey, s.gotSub = apiKey, submissionID
	if s.err != nil {
		return "", s.err
	}
	return s.wallet, nil
}

func seedReworkSubmission(t *testing.T, store scstore.Store, submissionID, status string) {
	t.Helper()
	if err := store.SyncSubmission(context.Background(), core.Submission{
		SubmissionID: submissionID,
		ClaimID:      "claim-" + submissionID,
		TaskID:       "task-" + submissionID,
		Status:       status,
		Deliverables: map[string]interface{}{"url": "original-work"},
		CreatedAt:    time.Now(),
	}); err != nil {
		t.Fatalf("seed submission: %v", err)
	}
}

func reworkService(store scstore.Store, record EventRecorder, authz SubmissionReworkAuthorizer) *SubmissionService {
	return NewSubmissionService(store, record, nil, authz)
}

func assertReworkUntouched(t *testing.T, store scstore.Store, submissionID, wantStatus string) {
	t.Helper()
	sub, err := store.GetSubmission(context.Background(), submissionID)
	if err != nil {
		t.Fatalf("get submission: %v", err)
	}
	if sub.Status != wantStatus {
		t.Fatalf("status = %q, want %q", sub.Status, wantStatus)
	}
	if got, _ := sub.Deliverables["url"].(string); got != "original-work" {
		t.Fatalf("deliverables were overwritten: url = %q", got)
	}
}

func TestReworkRefusesWhenAuthorizerMissing(t *testing.T) {
	store := scstore.NewMemoryStore(time.Hour)
	seedReworkSubmission(t, store, "sub-rework-noauthz", "pending_review")
	svc := reworkService(store, nil, nil)

	_, err := svc.Rework(context.Background(), "sub-rework-noauthz",
		SubmissionReworkInput{Notes: "hijack"}, ReworkActor{APIKey: "any-key"})
	if err == nil {
		t.Fatal("expected a service with no rework authorizer to refuse, got nil")
	}
	if se := AsStatus(err); se == nil || se.Status != http.StatusInternalServerError {
		t.Fatalf("expected 500 for a wiring failure, got %v", err)
	}
	assertReworkUntouched(t, store, "sub-rework-noauthz", "pending_review")
}

// Wiring the review authorizer must not be mistaken for wiring this one, which is
// why they are separate parameters.
func TestReworkIgnoresTheReviewAuthorizer(t *testing.T) {
	store := scstore.NewMemoryStore(time.Hour)
	seedReworkSubmission(t, store, "sub-rework-wrongauthz", "pending_review")
	svc := NewSubmissionService(store, nil, &stubReviewAuthorizer{wallet: "bc1qcreator"}, nil)

	_, err := svc.Rework(context.Background(), "sub-rework-wrongauthz",
		SubmissionReworkInput{Notes: "hijack"}, ReworkActor{APIKey: "any-key"})
	if se := AsStatus(err); se == nil || se.Status != http.StatusInternalServerError {
		t.Fatalf("a review authorizer must not satisfy rework, got %v", err)
	}
	assertReworkUntouched(t, store, "sub-rework-wrongauthz", "pending_review")
}

func TestReworkDeniesBeforeMutating(t *testing.T) {
	store := scstore.NewMemoryStore(time.Hour)
	seedReworkSubmission(t, store, "sub-rework-denied", "pending_review")

	var events []core.Event
	svc := reworkService(store, func(evt core.Event) { events = append(events, evt) },
		&stubReworkAuthorizer{err: errors.New("wallet bc1qstranger is not the claimant of submission sub-rework-denied")})

	_, err := svc.Rework(context.Background(), "sub-rework-denied",
		SubmissionReworkInput{Deliverables: map[string]interface{}{"url": "attacker-work"}},
		ReworkActor{APIKey: "stranger-key"})
	if err == nil {
		t.Fatal("expected a non-claimant to be denied, got nil")
	}
	if se := AsStatus(err); se == nil || se.Status != http.StatusForbidden {
		t.Fatalf("expected 403, got %v", err)
	}

	assertReworkUntouched(t, store, "sub-rework-denied", "pending_review")
	if len(events) != 0 {
		t.Fatalf("a denied rework must not emit events, got %+v", events)
	}
}

// The states are the product rule maya specified: pending_review and reviewed may
// be reworked, rejected continues through submit_work, and approved never
// reopens. An authorized claimant is used throughout so a refusal is the state
// gate and nothing else.
func TestReworkSourceStateGate(t *testing.T) {
	const claimant = "bc1qclaimant"
	cases := []struct {
		status     string
		wantStatus int
	}{
		{"pending_review", 0},
		{"reviewed", 0},
		{"rejected", http.StatusConflict},
		{"approved", http.StatusConflict},
	}

	for _, tc := range cases {
		t.Run(tc.status, func(t *testing.T) {
			store := scstore.NewMemoryStore(time.Hour)
			seedReworkSubmission(t, store, "sub-state-"+tc.status, tc.status)
			svc := reworkService(store, nil, &stubReworkAuthorizer{wallet: claimant})

			_, err := svc.Rework(context.Background(), "sub-state-"+tc.status,
				SubmissionReworkInput{Notes: "revised"}, ReworkActor{APIKey: "claimant-key"})

			if tc.wantStatus == 0 {
				if err != nil {
					t.Fatalf("a claimant must be able to rework from %s, got %v", tc.status, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("rework from %s must be refused", tc.status)
			}
			if se := AsStatus(err); se == nil || se.Status != tc.wantStatus {
				t.Fatalf("expected %d for %s, got %v", tc.wantStatus, tc.status, err)
			}
			assertReworkUntouched(t, store, "sub-state-"+tc.status, tc.status)
		})
	}
}

// The rejected refusal has to name submit_work: rejected work is expected to
// continue, so a bare refusal would send the claimant looking for a dead end.
func TestReworkRejectionNamesTheRouteBack(t *testing.T) {
	store := scstore.NewMemoryStore(time.Hour)
	seedReworkSubmission(t, store, "sub-state-msg", "rejected")
	svc := reworkService(store, nil, &stubReworkAuthorizer{wallet: "bc1qclaimant"})

	_, err := svc.Rework(context.Background(), "sub-state-msg",
		SubmissionReworkInput{Notes: "revised"}, ReworkActor{APIKey: "claimant-key"})
	se := AsStatus(err)
	if se == nil {
		t.Fatalf("expected a status error, got %v", err)
	}
	if !strings.Contains(se.Message, "submit new work") {
		t.Fatalf("refusal should point at submit_work, got %q", se.Message)
	}
}

func TestReworkEventRecordsAuthorizedClaimant(t *testing.T) {
	const claimant = "bc1qclaimant"
	store := scstore.NewMemoryStore(time.Hour)
	seedReworkSubmission(t, store, "sub-rework-actor", "pending_review")

	var events []core.Event
	stub := &stubReworkAuthorizer{wallet: claimant}
	svc := reworkService(store, func(evt core.Event) { events = append(events, evt) }, stub)

	if _, err := svc.Rework(context.Background(), "sub-rework-actor",
		SubmissionReworkInput{Notes: "revised"}, ReworkActor{APIKey: "claimant-key"}); err != nil {
		t.Fatalf("rework: %v", err)
	}

	if stub.gotKey != "claimant-key" || stub.gotSub != "sub-rework-actor" {
		t.Fatalf("authorizer got (%q, %q), want (claimant-key, sub-rework-actor)", stub.gotKey, stub.gotSub)
	}
	for _, evt := range events {
		if evt.Type != "rework" {
			continue
		}
		if evt.Actor != claimant {
			t.Fatalf("rework event actor = %q, want the authorized claimant %q", evt.Actor, claimant)
		}
		return
	}
	t.Fatalf("no rework event was emitted, got %+v", events)
}
