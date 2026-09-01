package smart_contract

import (
	"context"
	"fmt"
	"testing"
	"time"

	core "stargate-backend/core/smart_contract"
)

func TestListTasksCursorAndLimit(t *testing.T) {
	t.Parallel()
	t.Run("memory", func(t *testing.T) {
		runListTasksCursorAndLimit(t, NewMemoryStore(time.Hour))
	})
	t.Run("sqlite", func(t *testing.T) {
		runListTasksCursorAndLimit(t, newTestSQLiteStore(t))
	})
}

func runListTasksCursorAndLimit(t *testing.T, store Store) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	c := core.Contract{ContractID: "wish-taskpage", Title: "C", Status: "active", CreatedAt: now}
	tasks := []core.Task{
		{TaskID: "task-c", ContractID: c.ContractID, Title: "C", Status: "available"},
		{TaskID: "task-b", ContractID: c.ContractID, Title: "B", Status: "available"},
		{TaskID: "task-a", ContractID: c.ContractID, Title: "A", Status: "available"},
	}
	if err := store.UpsertContractWithTasks(ctx, c, tasks); err != nil {
		t.Fatalf("seed: %v", err)
	}

	page1, err := store.ListTasks(core.TaskFilter{ContractID: c.ContractID, Limit: 2})
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(page1) != 2 {
		t.Fatalf("page1 len=%d", len(page1))
	}
	if page1[0].TaskID != "task-c" || page1[1].TaskID != "task-b" {
		t.Fatalf("page1 order %s,%s", page1[0].TaskID, page1[1].TaskID)
	}

	page2, err := store.ListTasks(core.TaskFilter{
		ContractID: c.ContractID, Limit: 2, CursorID: page1[1].TaskID, CursorType: "before",
	})
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2) != 1 || page2[0].TaskID != "task-a" {
		ids := make([]string, len(page2))
		for i, tk := range page2 {
			ids[i] = tk.TaskID
		}
		t.Fatalf("page2 want [task-a], got %v", ids)
	}
}

func TestListProposalsCursorAndLimit(t *testing.T) {
	t.Parallel()
	t.Run("memory", func(t *testing.T) {
		runListProposalsCursorAndLimit(t, NewMemoryStore(time.Hour))
	})
	t.Run("sqlite", func(t *testing.T) {
		runListProposalsCursorAndLimit(t, newTestSQLiteStore(t))
	})
}

func runListProposalsCursorAndLimit(t *testing.T, store Store) {
	t.Helper()
	ctx := context.Background()
	base := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	for i, id := range []string{"prop-a", "prop-b", "prop-c"} {
		p := core.Proposal{
			ID: id, Title: id, DescriptionMD: "ok " + id, Status: "pending",
			VisiblePixelHash: fmt.Sprintf("%064d", i+1),
			CreatedAt:        base.Add(time.Duration(i) * time.Hour),
		}
		if err := store.CreateProposal(ctx, p); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}

	page1, err := store.ListProposals(ctx, core.ProposalFilter{Limit: 2})
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(page1) != 2 {
		t.Fatalf("page1 len=%d", len(page1))
	}
	if page1[0].ID != "prop-c" || page1[1].ID != "prop-b" {
		t.Fatalf("page1 order %s,%s", page1[0].ID, page1[1].ID)
	}

	cursor := page1[1].CreatedAt
	page2, err := store.ListProposals(ctx, core.ProposalFilter{
		Limit: 2, CursorDate: &cursor, CursorID: page1[1].ID, CursorType: "before",
	})
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2) != 1 || page2[0].ID != "prop-a" {
		ids := make([]string, len(page2))
		for i, p := range page2 {
			ids[i] = p.ID
		}
		t.Fatalf("page2 want [prop-a], got %v", ids)
	}
}

func TestApplyOffsetLimitAndDateIDCursor(t *testing.T) {
	items := []int{1, 2, 3, 4}
	got := applyOffsetLimit(items, 1, 2)
	if len(got) != 2 || got[0] != 2 || got[1] != 3 {
		t.Fatalf("%v", got)
	}
	ts := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	later := ts.Add(time.Hour)
	if !matchDateIDCursor(ts, "b", &later, "a", "before") {
		t.Fatal("older date should match before")
	}
	if matchDateIDCursor(later, "b", &later, "a", "before") && !("b" < "a") {
		t.Fatal("same date must use id tie-break")
	}
	if !matchDateIDCursor(later, "a", &later, "b", "before") {
		t.Fatal("same date id a < b")
	}
}
