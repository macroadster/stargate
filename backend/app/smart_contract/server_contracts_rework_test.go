package smart_contract

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"stargate-backend/core/smart_contract"
)

// REST GET /contracts/{id}/rework regressed: the frontend Rework tab polls it,
// but the route was never wired (the handler was dead code until it was
// deleted in 84176ae), so the tab always saw 404 and the console logged the
// failure. These cover the restored route and the pixel-alias handling the
// frontend depends on for wish-<hash> ids.

func getReworkRequestsViaRoute(t *testing.T, srv *Server, id string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/smart_contract/contracts/"+id+"/rework", nil)
	rec := httptest.NewRecorder()
	srv.handleContracts(rec, req)
	return rec
}

func decodeReworkRequests(t *testing.T, rec *httptest.ResponseRecorder) []smart_contract.ContractReworkRequest {
	t.Helper()
	var body struct {
		ReworkRequests []smart_contract.ContractReworkRequest `json:"rework_requests"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}
	return body.ReworkRequests
}

func TestRESTGetReworkRequestsReturnsSeededList(t *testing.T) {
	srv, store := authzFixture(t)
	contractID := "wish-" + testWishHash
	if err := store.UpsertContractWithTasks(context.Background(), smart_contract.Contract{
		ContractID: contractID,
		Title:      "Wish with rework",
		Status:     "active",
	}, nil); err != nil {
		t.Fatalf("seed contract: %v", err)
	}
	if _, err := store.CreateContractReworkRequest(context.Background(), contractID, testCreatorWlt, "please redo the header"); err != nil {
		t.Fatalf("seed rework request: %v", err)
	}

	rec := getReworkRequestsViaRoute(t, srv, contractID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s, want 200", rec.Code, rec.Body.String())
	}
	reqs := decodeReworkRequests(t, rec)
	if len(reqs) != 1 {
		t.Fatalf("rework_requests=%d, want 1", len(reqs))
	}
	if reqs[0].Notes != "please redo the header" {
		t.Fatalf("notes=%q", reqs[0].Notes)
	}
	if reqs[0].Requester != testCreatorWlt {
		t.Fatalf("requester=%q, want %q", reqs[0].Requester, testCreatorWlt)
	}
}

func TestRESTGetReworkRequestsResolvesPixelAlias(t *testing.T) {
	srv, store := authzFixture(t)
	// Row stored under the bare hash; the wish-<hash> id the UI carries must
	// resolve onto it instead of 404ing.
	if err := store.UpsertContractWithTasks(context.Background(), smart_contract.Contract{
		ContractID: testWishHash,
		Title:      "Wish stored bare",
		Status:     "confirmed",
	}, nil); err != nil {
		t.Fatalf("seed contract: %v", err)
	}
	if _, err := store.CreateContractReworkRequest(context.Background(), testWishHash, testCreatorWlt, "tighten the loop"); err != nil {
		t.Fatalf("seed rework request: %v", err)
	}

	rec := getReworkRequestsViaRoute(t, srv, "wish-"+testWishHash)
	if rec.Code != http.StatusOK {
		t.Fatalf("alias status=%d body=%s, want 200", rec.Code, rec.Body.String())
	}
	reqs := decodeReworkRequests(t, rec)
	if len(reqs) != 1 || reqs[0].Notes != "tighten the loop" {
		t.Fatalf("alias lookup returned %+v", reqs)
	}
}

func TestRESTGetReworkRequestsUnknownContract(t *testing.T) {
	srv, _ := authzFixture(t)
	rec := getReworkRequestsViaRoute(t, srv, "wish-"+testWishHash)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s, want 404", rec.Code, rec.Body.String())
	}
}

func TestRESTGetReworkRequestsEmptyForContractWithoutRequests(t *testing.T) {
	srv, store := authzFixture(t)
	contractID := "wish-" + testWishHash
	if err := store.UpsertContractWithTasks(context.Background(), smart_contract.Contract{
		ContractID: contractID,
		Title:      "Wish with no rework",
		Status:     "active",
	}, nil); err != nil {
		t.Fatalf("seed contract: %v", err)
	}
	rec := getReworkRequestsViaRoute(t, srv, contractID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s, want 200", rec.Code, rec.Body.String())
	}
	decodeReworkRequests(t, rec)
}
