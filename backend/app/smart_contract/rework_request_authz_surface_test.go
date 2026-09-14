package smart_contract

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"stargate-backend/core/smart_contract"
	scstore "stargate-backend/storage/smart_contract"
)

// irl.8: filing a rework request required a wallet-bound key but never checked
// that the wallet was the wish creator, so any self-issued key could file
// against any contract and be stored as its creator. Requester is documented as
// the wish creator's address, and rework requests feed
// SubmissionService.maybeResolveRework, so the field is load-bearing.
//
// Both surfaces had the same hole. These cover the REST route; the MCP tool is
// covered in mcp/rework_request_authz_surface_test.go.

func seedReworkContract(t *testing.T, store scstore.Store) string {
	t.Helper()
	contractID := "wish-" + testWishHash
	if err := store.UpsertContractWithTasks(context.Background(), smart_contract.Contract{
		ContractID: contractID,
		Title:      "Wish with a creator",
		Status:     "active",
	}, nil); err != nil {
		t.Fatalf("seed contract: %v", err)
	}
	return contractID
}

func postReworkRequest(t *testing.T, srv *Server, apiKey, contractID, notes string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]string{"notes": notes})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/smart_contract/contracts/"+contractID+"/rework", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
	rec := httptest.NewRecorder()
	srv.handleContractRework(rec, req, contractID)
	return rec
}

func reworkRequestCount(t *testing.T, store scstore.Store, contractID string) int {
	t.Helper()
	reqs, err := store.GetContractReworkRequests(context.Background(), contractID)
	if err != nil {
		t.Fatalf("get rework requests: %v", err)
	}
	return len(reqs)
}

func TestRESTReworkRequestRejectsStranger(t *testing.T) {
	srv, store := authzFixture(t)
	contractID := seedReworkContract(t, store)

	// A stranger's key is legitimate and wallet-bound. It is simply not the
	// creator's, which is the whole distinction this check exists to make.
	rec := postReworkRequest(t, srv, testStrangerKey, contractID, "please redo the header")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d (body: %s)", rec.Code, http.StatusForbidden, rec.Body.String())
	}
	if n := reworkRequestCount(t, store, contractID); n != 0 {
		t.Fatalf("a refused request wrote %d rework requests", n)
	}
}

func TestRESTReworkRequestAllowsCreator(t *testing.T) {
	srv, store := authzFixture(t)
	contractID := seedReworkContract(t, store)

	rec := postReworkRequest(t, srv, testCreatorKey, contractID, "please redo the header")
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body: %s)", rec.Code, http.StatusCreated, rec.Body.String())
	}

	reqs, err := store.GetContractReworkRequests(context.Background(), contractID)
	if err != nil {
		t.Fatalf("get rework requests: %v", err)
	}
	if len(reqs) != 1 {
		t.Fatalf("stored %d rework requests, want 1", len(reqs))
	}
	// Requester is the wallet authorization accepted, not one the handler
	// resolved for itself alongside the check.
	if reqs[0].Requester != testCreatorWlt {
		t.Fatalf("requester = %q, want the authorized wallet %q", reqs[0].Requester, testCreatorWlt)
	}
}

func TestRESTReworkRequestRejectsKeylessCaller(t *testing.T) {
	srv, store := authzFixture(t)
	contractID := seedReworkContract(t, store)

	rec := postReworkRequest(t, srv, "", contractID, "please redo the header")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d (body: %s)", rec.Code, http.StatusForbidden, rec.Body.String())
	}
	if n := reworkRequestCount(t, store, contractID); n != 0 {
		t.Fatalf("a refused request wrote %d rework requests", n)
	}
}

// TestRESTReworkRequestDeniesUnknownWishCreator pins the fail-closed direction:
// a contract whose wish has no establishable creator is refused rather than
// filed against. This is the same choice irl.2 made for proposals.
func TestRESTReworkRequestDeniesUnknownWishCreator(t *testing.T) {
	srv, store := authzFixture(t)

	unknown := "wish-" + "f00dbabe" + testWishHash[8:]
	if err := store.UpsertContractWithTasks(context.Background(), smart_contract.Contract{
		ContractID: unknown,
		Title:      "Wish with no ingest record",
		Status:     "active",
	}, nil); err != nil {
		t.Fatalf("seed contract: %v", err)
	}

	rec := postReworkRequest(t, srv, testCreatorKey, unknown, "please redo the header")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d (body: %s)", rec.Code, http.StatusForbidden, rec.Body.String())
	}
	if n := reworkRequestCount(t, store, unknown); n != 0 {
		t.Fatalf("a refused request wrote %d rework requests", n)
	}
}
