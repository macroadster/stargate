package smart_contract

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	scservices "stargate-backend/app/smart_contract/services"
	core "stargate-backend/core/smart_contract"
	auth "stargate-backend/storage/auth"
	scstore "stargate-backend/storage/smart_contract"
)

func TestRESTSubmissionListSharesServiceQuery(t *testing.T) {
	store := scstore.NewMemoryStore(time.Hour)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	c1 := core.Contract{ContractID: "wish-sublist1", Title: "C1", Status: "active", CreatedAt: now}
	t1 := core.Task{TaskID: "task-sub-1", ContractID: c1.ContractID, Title: "T1", Status: "submitted"}
	t2 := core.Task{TaskID: "task-sub-2", ContractID: c1.ContractID, Title: "T2", Status: "submitted"}
	if err := store.UpsertContractWithTasks(ctx, c1, []core.Task{t1, t2}); err != nil {
		t.Fatalf("seed contract: %v", err)
	}
	for _, c := range []core.Claim{
		{ClaimID: "claim-sub-1", TaskID: t1.TaskID, Status: "submitted", CreatedAt: now},
		{ClaimID: "claim-sub-2", TaskID: t2.TaskID, Status: "submitted", CreatedAt: now},
	} {
		if err := store.SyncClaim(ctx, c); err != nil {
			t.Fatalf("sync claim: %v", err)
		}
	}
	for _, sub := range []core.Submission{
		{SubmissionID: "sub-a", ClaimID: "claim-sub-1", TaskID: t1.TaskID, Status: "pending_review", CreatedAt: now.Add(2 * time.Second)},
		{SubmissionID: "sub-b", ClaimID: "claim-sub-2", TaskID: t2.TaskID, Status: "approved", CreatedAt: now.Add(time.Second)},
	} {
		if err := store.SyncSubmission(ctx, sub); err != nil {
			t.Fatalf("sync submission: %v", err)
		}
	}

	apiKey := "sub-list-key"
	server := NewServer(store, &mockAPIKeyStore{keys: map[string]auth.APIKey{apiKey: {Key: apiKey}}}, nil)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/smart_contract/submissions?contract_id=sublist1&status=pending_review&limit=10&offset=0", nil)
	req.Header.Set("X-API-Key", apiKey)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("REST status %d: %s", w.Code, w.Body.String())
	}

	var rest scservices.SubmissionListResult
	if err := json.Unmarshal(w.Body.Bytes(), &rest); err != nil {
		t.Fatalf("decode REST: %v", err)
	}
	svc := scservices.NewSubmissionService(store, nil)
	got, err := svc.List(ctx, scservices.SubmissionFilterFromArgs(map[string]interface{}{
		"contract_id": "sublist1",
		"status":      "pending_review",
		"limit":       10,
		"offset":      0,
	}))
	if err != nil {
		t.Fatalf("service list: %v", err)
	}
	if rest.Total != got.Total || rest.HasMore != got.HasMore || len(rest.Submissions) != len(got.Submissions) {
		t.Fatalf("REST %+v != service %+v", rest, got)
	}
	if len(rest.Submissions) != 1 || rest.Submissions[0].SubmissionID != "sub-a" {
		t.Fatalf("expected pending_review sub-a, got %+v", rest.Submissions)
	}
}
