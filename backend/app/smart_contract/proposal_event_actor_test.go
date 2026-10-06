package smart_contract

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"stargate-backend/core/smart_contract"
)

// az6: the approve event said "approver" and proposal_create said "creator", no
// matter who acted. These go through the REST routes with a real authorizer, so
// they pin the identity the gate actually accepted rather than one a stub was
// told to return.

// recordedEvents copies the server's event ring. Reading the field directly beats
// RegisterEventSink, which is process-global and cannot be unregistered.
func recordedEvents(srv *Server) []smart_contract.Event {
	srv.eventsMu.Lock()
	defer srv.eventsMu.Unlock()
	return append([]smart_contract.Event(nil), srv.events...)
}

func eventActorFor(t *testing.T, srv *Server, eventType string) string {
	t.Helper()
	events := recordedEvents(srv)
	for _, evt := range events {
		if evt.Type == eventType {
			return evt.Actor
		}
	}
	t.Fatalf("no %s event was recorded, got %+v", eventType, events)
	return ""
}

func TestApproveEventRecordsAuthorizedWallet(t *testing.T) {
	srv, store := authzFixture(t)
	seedOwnedProposal(t, store, "evt-approve", "pending")

	rec := postProposalApproval(t, srv, testCreatorKey, "evt-approve")
	if rec.Code != http.StatusOK {
		t.Fatalf("approval failed: %d %s", rec.Code, rec.Body.String())
	}

	if actor := eventActorFor(t, srv, "approve"); actor != testCreatorWlt {
		t.Fatalf("approve event actor = %q, want the authorized wallet %q", actor, testCreatorWlt)
	}
}

// Creation is open to keys with no wallet binding, so the actor is the bound
// wallet when there is one and empty when there is not. Empty is deliberate: the
// events filter treats a blank actor as nothing to match on, whereas the literal
// "creator" it replaced looked like an identity and never was one.
func TestProposalCreateEventRecordsBoundWallet(t *testing.T) {
	srv, store := authzFixture(t)
	if err := store.UpsertContractWithTasks(context.Background(), smart_contract.Contract{
		ContractID: "wish-" + testWishHash,
		Title:      "Wish with a creator",
		Status:     "pending",
	}, nil); err != nil {
		t.Fatalf("seed wish contract: %v", err)
	}

	body := `{"id":"evt-create","title":"Created by a bound key","description_md":"details",` +
		`"visible_pixel_hash":"` + testWishHash + `","contract_id":"` + testWishHash + `","budget_sats":1000}`
	req := httptest.NewRequest(http.MethodPost, "/api/smart_contract/proposals", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", testCreatorKey)
	rec := httptest.NewRecorder()
	srv.handleProposals(rec, req)
	if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
		t.Fatalf("create failed: %d %s", rec.Code, rec.Body.String())
	}

	if actor := eventActorFor(t, srv, "proposal_create"); actor != testCreatorWlt {
		t.Fatalf("proposal_create actor = %q, want the wallet bound to the creating key %q", actor, testCreatorWlt)
	}
}
