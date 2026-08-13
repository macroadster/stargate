package smart_contract

import (
	"context"
	"testing"
	"time"

	core "stargate-backend/core/smart_contract"
)

func TestListSubmissionsFilterAndPage(t *testing.T) {
	t.Parallel()
	t.Run("memory", func(t *testing.T) {
		runListSubmissionsFilterAndPage(t, NewMemoryStore(time.Hour))
	})
	t.Run("sqlite", func(t *testing.T) {
		runListSubmissionsFilterAndPage(t, newTestSQLiteStore(t))
	})
}

func runListSubmissionsFilterAndPage(t *testing.T, store Store) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	c1 := core.Contract{ContractID: "wish-sublist1", Title: "C1", Status: "active", CreatedAt: now}
	c2 := core.Contract{ContractID: "contract-sublist2", Title: "C2", Status: "active", CreatedAt: now}
	t1 := core.Task{TaskID: "task-sub-1", ContractID: c1.ContractID, Title: "T1", Status: "submitted"}
	t2 := core.Task{TaskID: "task-sub-2", ContractID: c1.ContractID, Title: "T2", Status: "submitted"}
	t3 := core.Task{TaskID: "task-sub-3", ContractID: c2.ContractID, Title: "T3", Status: "submitted"}
	if err := store.UpsertContractWithTasks(ctx, c1, []core.Task{t1, t2}); err != nil {
		t.Fatalf("seed c1: %v", err)
	}
	if err := store.UpsertContractWithTasks(ctx, c2, []core.Task{t3}); err != nil {
		t.Fatalf("seed c2: %v", err)
	}

	claims := []core.Claim{
		{ClaimID: "claim-sub-1", TaskID: t1.TaskID, Status: "submitted", CreatedAt: now},
		{ClaimID: "claim-sub-2", TaskID: t2.TaskID, Status: "submitted", CreatedAt: now},
		{ClaimID: "claim-sub-3", TaskID: t3.TaskID, Status: "submitted", CreatedAt: now},
	}
	for _, c := range claims {
		if err := store.SyncClaim(ctx, c); err != nil {
			t.Fatalf("sync claim %s: %v", c.ClaimID, err)
		}
	}

	subs := []core.Submission{
		{SubmissionID: "sub-a", ClaimID: "claim-sub-1", TaskID: t1.TaskID, Status: "pending_review", CreatedAt: now.Add(3 * time.Second)},
		{SubmissionID: "sub-b", ClaimID: "claim-sub-2", TaskID: t2.TaskID, Status: "approved", CreatedAt: now.Add(2 * time.Second)},
		{SubmissionID: "sub-c", ClaimID: "claim-sub-3", TaskID: t3.TaskID, Status: "pending_review", CreatedAt: now.Add(1 * time.Second)},
	}
	for _, sub := range subs {
		if err := store.SyncSubmission(ctx, sub); err != nil {
			t.Fatalf("sync submission %s: %v", sub.SubmissionID, err)
		}
	}

	all, err := store.ListSubmissions(ctx, core.SubmissionFilter{})
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if got := submissionIDs(all); !sameIDs(got, []string{"sub-a", "sub-b", "sub-c"}) {
		t.Fatalf("list all ids = %v", got)
	}
	if len(all) >= 2 && !all[0].CreatedAt.After(all[1].CreatedAt) && all[0].SubmissionID < all[1].SubmissionID {
		t.Fatalf("expected newest-first order, got %v", submissionIDs(all))
	}

	byContract, err := store.ListSubmissions(ctx, core.SubmissionFilter{ContractID: "sublist1"})
	if err != nil {
		t.Fatalf("list by contract: %v", err)
	}
	if got := submissionIDs(byContract); !sameIDs(got, []string{"sub-a", "sub-b"}) {
		t.Fatalf("contract filter ids = %v", got)
	}

	byTask, err := store.ListSubmissions(ctx, core.SubmissionFilter{TaskID: t1.TaskID})
	if err != nil {
		t.Fatalf("list by task: %v", err)
	}
	if got := submissionIDs(byTask); !sameIDs(got, []string{"sub-a"}) {
		t.Fatalf("task filter ids = %v", got)
	}

	byStatus, err := store.ListSubmissions(ctx, core.SubmissionFilter{Status: "approved"})
	if err != nil {
		t.Fatalf("list by status: %v", err)
	}
	if got := submissionIDs(byStatus); !sameIDs(got, []string{"sub-b"}) {
		t.Fatalf("status filter ids = %v", got)
	}

	page1, err := store.ListSubmissions(ctx, core.SubmissionFilter{Limit: 1, Offset: 0})
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(page1) != 1 || page1[0].SubmissionID != "sub-a" {
		t.Fatalf("page1 = %v", submissionIDs(page1))
	}
	page2, err := store.ListSubmissions(ctx, core.SubmissionFilter{Limit: 1, Offset: 1})
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2) != 1 || page2[0].SubmissionID != "sub-b" {
		t.Fatalf("page2 = %v", submissionIDs(page2))
	}
}

func TestSubmissionContractIDs(t *testing.T) {
	got := submissionContractIDs("wish-abc")
	if !containsFold(got, "wish-abc") || !containsFold(got, "abc") {
		t.Fatalf("variants = %v", got)
	}
}

func submissionIDs(subs []core.Submission) []string {
	out := make([]string, len(subs))
	for i, s := range subs {
		out[i] = s.SubmissionID
	}
	return out
}

func sameIDs(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	set := make(map[string]struct{}, len(want))
	for _, id := range want {
		set[id] = struct{}{}
	}
	for _, id := range got {
		if _, ok := set[id]; !ok {
			return false
		}
	}
	return true
}

func containsFold(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}
