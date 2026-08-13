package services

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	core "stargate-backend/core/smart_contract"
	scstore "stargate-backend/storage/smart_contract"
)

func TestSubmissionFilterFromArgs(t *testing.T) {
	filter := SubmissionFilterFromArgs(map[string]interface{}{
		"contract_id": "contract-123",
		"task_id":     "task-1",
		"task_ids":    "task-2, task-3",
		"status":      "pending_review",
		"limit":       float64(10),
		"offset":      json.Number("5"),
	})
	if filter.ContractID != "contract-123" || filter.TaskID != "task-1" || filter.Status != "pending_review" {
		t.Fatalf("unexpected filter: %+v", filter)
	}
	if filter.Limit != 10 || filter.Offset != 5 {
		t.Fatalf("pagination = limit %d offset %d", filter.Limit, filter.Offset)
	}
	if len(filter.TaskIDs) != 2 {
		t.Fatalf("task_ids = %v", filter.TaskIDs)
	}
}

func TestSubmissionServiceListPagination(t *testing.T) {
	store := scstore.NewMemoryStore(time.Hour)
	svc := NewSubmissionService(store, nil)
	ctx := context.Background()
	seedListSubmissions(t, store)

	result, err := svc.List(ctx, core.SubmissionFilter{ContractID: "wish-sublist1", Limit: 1, Offset: 0})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if result.Total != 2 || result.Limit != 1 || result.Offset != 0 || !result.HasMore {
		t.Fatalf("unexpected page meta: %+v", result)
	}
	if len(result.Submissions) != 1 || result.Submissions[0].SubmissionID != "sub-a" {
		t.Fatalf("page = %+v", result.Submissions)
	}

	page2, err := svc.List(ctx, core.SubmissionFilter{ContractID: "wish-sublist1", Limit: 1, Offset: 1})
	if err != nil {
		t.Fatalf("list page2: %v", err)
	}
	if page2.HasMore || len(page2.Submissions) != 1 || page2.Submissions[0].SubmissionID != "sub-b" {
		t.Fatalf("page2 = %+v has_more=%v", page2.Submissions, page2.HasMore)
	}
}

func seedListSubmissions(t *testing.T, store scstore.Store) {
	t.Helper()
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
}
