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

	cursor := page1[0].CreatedAt
	viaCursor, err := store.ListSubmissions(ctx, core.SubmissionFilter{
		Limit: 1, CursorDate: &cursor, CursorID: page1[0].SubmissionID, CursorType: "before",
	})
	if err != nil {
		t.Fatalf("cursor page: %v", err)
	}
	if len(viaCursor) != 1 || viaCursor[0].SubmissionID != "sub-b" {
		t.Fatalf("cursor page = %v", submissionIDs(viaCursor))
	}
}

func TestListSubmissionsPendingReviewNullRejectionFields(t *testing.T) {
	store := newTestSQLiteStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	c1 := core.Contract{ContractID: "wish-null-reject", Title: "C1", Status: "active", CreatedAt: now}
	t1 := core.Task{TaskID: "task-null-reject", ContractID: c1.ContractID, Title: "T1", Status: "submitted"}
	if err := store.UpsertContractWithTasks(ctx, c1, []core.Task{t1}); err != nil {
		t.Fatalf("seed contract: %v", err)
	}
	if err := store.SyncClaim(ctx, core.Claim{ClaimID: "claim-null-reject", TaskID: t1.TaskID, Status: "submitted", CreatedAt: now}); err != nil {
		t.Fatalf("sync claim: %v", err)
	}

	// Production pending_review rows leave rejection_reason/type NULL. SyncSubmission
	// writes empty strings, so insert the live shape directly.
	if _, err := store.db.ExecContext(ctx, `
INSERT INTO mcp_submissions (submission_id, claim_id, task_id, status, deliverables, completion_proof, rejection_reason, rejection_type, rejected_at, created_at)
VALUES (?, ?, ?, 'pending_review', '{}', '{}', NULL, NULL, NULL, ?)
`, "sub-null-reject", "claim-null-reject", t1.TaskID, now.Format(time.RFC3339)); err != nil {
		t.Fatalf("insert pending_review with NULL rejection fields: %v", err)
	}

	got, err := store.ListSubmissions(ctx, core.SubmissionFilter{Status: "pending_review"})
	if err != nil {
		t.Fatalf("list pending_review: %v", err)
	}
	if len(got) != 1 || got[0].SubmissionID != "sub-null-reject" {
		t.Fatalf("pending_review rows = %+v", got)
	}
	if got[0].RejectionReason != "" || got[0].RejectionType != "" {
		t.Fatalf("expected empty rejection fields, got reason=%q type=%q", got[0].RejectionReason, got[0].RejectionType)
	}

	one, err := store.GetSubmission(ctx, "sub-null-reject")
	if err != nil {
		t.Fatalf("get submission: %v", err)
	}
	if one.SubmissionID != "sub-null-reject" || one.Status != "pending_review" {
		t.Fatalf("get = %+v", one)
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
