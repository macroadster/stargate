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

// These call Update and Publish directly, with no handler involved, for the same
// reason as the review tests: a future caller that forgets to authorize should be
// refused by the service rather than trusted. The wish creator rule itself lives
// in authz_test.go in the parent package.

// The store validates the hash, so it has to be real hex; which wish it names is
// irrelevant here because the authorizer is stubbed.
const editWishHash = "d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4e5"

type stubProposalAuthorizer struct {
	wallet string
	err    error

	calls   int
	gotKey  string
	gotProp string
}

func (s *stubProposalAuthorizer) AuthorizeProposalEdit(_ context.Context, apiKey, proposalID string) (string, error) {
	s.calls++
	s.gotKey, s.gotProp = apiKey, proposalID
	if s.err != nil {
		return "", s.err
	}
	return s.wallet, nil
}

func seedEditProposal(t *testing.T, store scstore.Store, proposalID, status string) {
	t.Helper()
	if err := store.CreateProposal(context.Background(), core.Proposal{
		ID:               proposalID,
		Title:            "Original title",
		DescriptionMD:    "details",
		VisiblePixelHash: editWishHash,
		BudgetSats:       1000,
		Status:           status,
		CreatedAt:        time.Now(),
	}); err != nil {
		t.Fatalf("seed proposal: %v", err)
	}
}

func editService(store scstore.Store, record EventRecorder, authz ProposalEditAuthorizer) *ProposalService {
	return NewProposalService(store, nil, nil, record, authz, nil, nil)
}

func assertProposalTitle(t *testing.T, store scstore.Store, proposalID, want string) {
	t.Helper()
	prop, err := store.GetProposal(context.Background(), proposalID)
	if err != nil {
		t.Fatalf("get proposal: %v", err)
	}
	if prop.Title != want {
		t.Fatalf("title = %q, want %q", prop.Title, want)
	}
}

func TestProposalUpdateRefusesWhenAuthorizerMissing(t *testing.T) {
	store := scstore.NewMemoryStore(time.Hour)
	seedEditProposal(t, store, "prop-noauthz", "pending")
	svc := editService(store, nil, nil)

	_, err := svc.Update(context.Background(), "prop-noauthz", ProposalUpdateInput{}, ProposalActor{APIKey: "any-key"})
	if err == nil {
		t.Fatal("expected a service with no authorizer to refuse the update, got nil")
	}
	if se := AsStatus(err); se == nil || se.Status != http.StatusInternalServerError {
		t.Fatalf("expected 500 for a wiring failure, got %v", err)
	}
	assertProposalTitle(t, store, "prop-noauthz", "Original title")
}

func TestProposalPublishRefusesWhenAuthorizerMissing(t *testing.T) {
	store := scstore.NewMemoryStore(time.Hour)
	seedEditProposal(t, store, "prop-pub-noauthz", "approved")
	svc := editService(store, nil, nil)

	_, err := svc.Publish(context.Background(), "prop-pub-noauthz", ProposalActor{APIKey: "any-key"})
	if err == nil {
		t.Fatal("expected a service with no authorizer to refuse the publish, got nil")
	}
	if se := AsStatus(err); se == nil || se.Status != http.StatusInternalServerError {
		t.Fatalf("expected 500 for a wiring failure, got %v", err)
	}
}

// The denial must precede the write, not be reported after it.
func TestProposalUpdateDeniesBeforeMutating(t *testing.T) {
	store := scstore.NewMemoryStore(time.Hour)
	seedEditProposal(t, store, "prop-denied", "pending")

	var events []core.Event
	newTitle := "Hijacked"
	var newBudget int64 = 999999
	svc := editService(store, func(evt core.Event) { events = append(events, evt) },
		&stubProposalAuthorizer{err: errors.New("approver wallet bc1qstranger does not match wish creator")})

	_, err := svc.Update(context.Background(), "prop-denied",
		ProposalUpdateInput{Title: &newTitle, BudgetSats: &newBudget}, ProposalActor{APIKey: "stranger-key"})
	if err == nil {
		t.Fatal("expected a refused authorization to deny the update, got nil")
	}
	if se := AsStatus(err); se == nil || se.Status != http.StatusForbidden {
		t.Fatalf("expected 403, got %v", err)
	}

	assertProposalTitle(t, store, "prop-denied", "Original title")
	prop, err := store.GetProposal(context.Background(), "prop-denied")
	if err != nil {
		t.Fatalf("get proposal: %v", err)
	}
	if prop.BudgetSats != 1000 {
		t.Fatalf("budget_sats = %d, want the seeded 1000", prop.BudgetSats)
	}
	if len(events) != 0 {
		t.Fatalf("a denied update must not emit events, got %+v", events)
	}
}

func TestProposalPublishDeniesBeforeMutating(t *testing.T) {
	store := scstore.NewMemoryStore(time.Hour)
	seedEditProposal(t, store, "prop-pub-denied", "approved")

	var events []core.Event
	svc := editService(store, func(evt core.Event) { events = append(events, evt) },
		&stubProposalAuthorizer{err: errors.New("approver wallet bc1qstranger does not match wish creator")})

	_, err := svc.Publish(context.Background(), "prop-pub-denied", ProposalActor{APIKey: "stranger-key"})
	if err == nil {
		t.Fatal("expected a refused authorization to deny the publish, got nil")
	}
	if se := AsStatus(err); se == nil || se.Status != http.StatusForbidden {
		t.Fatalf("expected 403, got %v", err)
	}

	prop, err := store.GetProposal(context.Background(), "prop-pub-denied")
	if err != nil {
		t.Fatalf("get proposal: %v", err)
	}
	if prop.Status == "published" {
		t.Fatal("proposal was published despite the denial")
	}
	if len(events) != 0 {
		t.Fatalf("a denied publish must not emit events, got %+v", events)
	}
}

// The events said "editor" and "approver" no matter who acted, which is the same
// defect ugs removed from review events: an audit trail that cannot name the key
// that changed a budget is not an audit trail.
func TestProposalEditEventsRecordAuthorizedWallet(t *testing.T) {
	const wallet = "bc1qcreatorwallet"

	t.Run("update", func(t *testing.T) {
		store := scstore.NewMemoryStore(time.Hour)
		seedEditProposal(t, store, "prop-actor-update", "pending")

		var events []core.Event
		newTitle := "Revised"
		stub := &stubProposalAuthorizer{wallet: wallet}
		svc := editService(store, func(evt core.Event) { events = append(events, evt) }, stub)

		if _, err := svc.Update(context.Background(), "prop-actor-update",
			ProposalUpdateInput{Title: &newTitle}, ProposalActor{APIKey: "creator-key"}); err != nil {
			t.Fatalf("update: %v", err)
		}

		if stub.gotKey != "creator-key" || stub.gotProp != "prop-actor-update" {
			t.Fatalf("authorizer got (%q, %q), want (creator-key, prop-actor-update)", stub.gotKey, stub.gotProp)
		}
		assertEventActor(t, events, "update", wallet)
	})

	t.Run("publish", func(t *testing.T) {
		store := scstore.NewMemoryStore(time.Hour)
		seedEditProposal(t, store, "prop-actor-publish", "approved")

		var events []core.Event
		stub := &stubProposalAuthorizer{wallet: wallet}
		svc := editService(store, func(evt core.Event) { events = append(events, evt) }, stub)

		if _, err := svc.Publish(context.Background(), "prop-actor-publish", ProposalActor{APIKey: "creator-key"}); err != nil {
			t.Fatalf("publish: %v", err)
		}
		assertEventActor(t, events, "publish", wallet)
	})
}

// az6: Approve used to take creatorOK, so a caller could authorize itself by
// passing true. These call Approve directly, which is where that argument was
// trusted.
func TestProposalApproveRefusesWhenAuthorizerMissing(t *testing.T) {
	store := scstore.NewMemoryStore(time.Hour)
	seedEditProposal(t, store, "prop-approve-noauthz", "pending")
	svc := editService(store, nil, nil)

	_, err := svc.Approve(context.Background(), "prop-approve-noauthz", ProposalActor{APIKey: "any-key"})
	if err == nil {
		t.Fatal("expected a service with no authorizer to refuse the approval, got nil")
	}
	if se := AsStatus(err); se == nil || se.Status != http.StatusInternalServerError {
		t.Fatalf("expected 500 for a wiring failure, got %v", err)
	}
	assertProposalStatusNot(t, store, "prop-approve-noauthz", "approved")
}

func TestProposalApproveDeniesBeforeMutating(t *testing.T) {
	store := scstore.NewMemoryStore(time.Hour)
	seedEditProposal(t, store, "prop-approve-denied", "pending")

	var events []core.Event
	svc := editService(store, func(evt core.Event) { events = append(events, evt) },
		&stubProposalAuthorizer{err: errors.New("approver wallet bc1qstranger does not match wish creator")})

	_, err := svc.Approve(context.Background(), "prop-approve-denied", ProposalActor{APIKey: "stranger-key"})
	if err == nil {
		t.Fatal("expected a refused authorization to deny the approval, got nil")
	}
	if se := AsStatus(err); se == nil || se.Status != http.StatusForbidden {
		t.Fatalf("expected 403, got %v", err)
	}

	assertProposalStatusNot(t, store, "prop-approve-denied", "approved")
	if len(events) != 0 {
		t.Fatalf("a denied approval must not emit events, got %+v", events)
	}
}

// There is no longer a parameter a caller can set to skip the check: the only way
// through Approve is the authorizer saying yes.
func TestProposalApproveConsultsAuthorizerWithCallerKey(t *testing.T) {
	store := scstore.NewMemoryStore(time.Hour)
	seedEditProposal(t, store, "prop-approve-actor", "pending")

	stub := &stubProposalAuthorizer{err: errors.New("denied")}
	svc := editService(store, nil, stub)

	if _, err := svc.Approve(context.Background(), "prop-approve-actor", ProposalActor{APIKey: "caller-key"}); err == nil {
		t.Fatal("approval was not authorized")
	}
	if stub.calls != 1 {
		t.Fatalf("authorizer called %d times, want 1", stub.calls)
	}
	if stub.gotKey != "caller-key" || stub.gotProp != "prop-approve-actor" {
		t.Fatalf("authorizer got (%q, %q), want (caller-key, prop-approve-actor)", stub.gotKey, stub.gotProp)
	}
}

func assertProposalStatusNot(t *testing.T, store scstore.Store, proposalID, notWant string) {
	t.Helper()
	prop, err := store.GetProposal(context.Background(), proposalID)
	if err != nil {
		t.Fatalf("get proposal: %v", err)
	}
	if strings.EqualFold(prop.Status, notWant) {
		t.Fatalf("proposal %s is %q despite the refusal", proposalID, prop.Status)
	}
}

func assertEventActor(t *testing.T, events []core.Event, eventType, wantActor string) {
	t.Helper()
	for _, evt := range events {
		if evt.Type != eventType {
			continue
		}
		if evt.Actor != wantActor {
			t.Fatalf("%s event actor = %q, want the authorized wallet %q", eventType, evt.Actor, wantActor)
		}
		return
	}
	t.Fatalf("no %s event was emitted, got %+v", eventType, events)
}
