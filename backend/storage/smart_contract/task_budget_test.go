package smart_contract

import (
	"context"
	"strings"
	"testing"
	"time"

	"stargate-backend/core/smart_contract"
)

func TestAllocateTaskBudgetsNeverExceedsTotal(t *testing.T) {
	titles := []string{"Planning analysis", "Implement API", "Implement UI", "Testing QA", "Documentation guide"}
	got := AllocateTaskBudgets(titles, nil, 1000)
	var sum int64
	for _, n := range got {
		sum += n
	}
	if sum != 1000 {
		t.Fatalf("sum=%d want 1000 amounts=%v", sum, got)
	}
}

func TestAllocateTaskBudgetsHonorsExplicitAndScalesOverflow(t *testing.T) {
	titles := []string{"Implement A", "Implement B"}
	got := AllocateTaskBudgets(titles, []int64{800, 800}, 1000)
	var sum int64
	for _, n := range got {
		if n <= 0 {
			t.Fatalf("expected positive shares, got %v", got)
		}
		sum += n
	}
	if sum != 1000 {
		t.Fatalf("overflow explicit sum=%d want 1000 amounts=%v", sum, got)
	}
}

func TestParseExplicitTaskSats(t *testing.T) {
	if n := parseExplicitTaskSats("Build UI (400 sats)", ""); n != 400 {
		t.Fatalf("title paren: got %d", n)
	}
	if n := parseExplicitTaskSats("Build UI", "Budget: 250 sats\nDo the work"); n != 250 {
		t.Fatalf("budget line: got %d", n)
	}
	if n := parseExplicitTaskSats("No amount here", "just a description"); n != 0 {
		t.Fatalf("expected 0, got %d", n)
	}
}

func TestBuildTasksFromMarkdownRespectsWishBudget(t *testing.T) {
	md := strings.Join([]string{
		"### Task 1: Planning analysis",
		"Scope the work",
		"### Task 2: Implement API",
		"Write the service",
		"### Task 3: Testing QA",
		"Cover the paths",
		"### Task 4: Documentation guide",
		"Write the README",
	}, "\n")
	tasks := BuildTasksFromMarkdown("prop-1", md, "ab", 1000, "")
	if len(tasks) != 4 {
		t.Fatalf("tasks=%d", len(tasks))
	}
	var sum int64
	for _, task := range tasks {
		sum += task.BudgetSats
	}
	if sum != 1000 {
		t.Fatalf("task budget sum=%d want 1000", sum)
	}
}

func TestBuildTasksFromMarkdownExplicitSats(t *testing.T) {
	md := "### Task 1: Implement API\nBudget: 600 sats\n### Task 2: Testing QA\nBudget: 400 sats\n"
	tasks := BuildTasksFromMarkdown("prop-2", md, "cd", 1000, "")
	if len(tasks) != 2 {
		t.Fatalf("tasks=%d", len(tasks))
	}
	if tasks[0].BudgetSats != 600 || tasks[1].BudgetSats != 400 {
		t.Fatalf("got %d + %d", tasks[0].BudgetSats, tasks[1].BudgetSats)
	}
}

func TestApplyTaskAmountRespectsWishCap(t *testing.T) {
	store := NewMemoryStore(time.Hour)
	ctx := context.Background()
	contract := smart_contract.Contract{
		ContractID:      "wish-abcd",
		Title:           "Wish",
		TotalBudgetSats: 1000,
		Status:          "pending",
	}
	tasks := []smart_contract.Task{
		{TaskID: "t1", ContractID: "wish-abcd", Title: "A", BudgetSats: 400, Status: "available"},
		{TaskID: "t2", ContractID: "wish-abcd", Title: "B", BudgetSats: 400, Status: "available"},
	}
	if err := store.UpsertContractWithTasks(ctx, contract, tasks); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyTaskAmount(ctx, store, "t1", 600); err != nil {
		t.Fatalf("600 should fit claimable 600: %v", err)
	}
	if _, err := ApplyTaskAmount(ctx, store, "t1", 600); err != nil {
		t.Fatalf("idempotent 600: %v", err)
	}
	if _, err := ApplyTaskAmount(ctx, store, "t2", 400); err != nil {
		t.Fatalf("t2 keep 400: %v", err)
	}
	if _, err := ApplyTaskAmount(ctx, store, "t2", 401); err == nil {
		t.Fatal("expected t2 401 to exceed remaining 400")
	}
	got, err := store.GetTask("t1")
	if err != nil {
		t.Fatal(err)
	}
	if got.BudgetSats != 600 {
		t.Fatalf("t1 budget=%d", got.BudgetSats)
	}
}

func TestSumTaskBudgetsIgnoresRejected(t *testing.T) {
	tasks := []smart_contract.Task{
		{TaskID: "a", BudgetSats: 400, Status: "approved"},
		{TaskID: "b", BudgetSats: 1600, Status: "rejected"},
		{TaskID: "c", BudgetSats: 1, Status: "cancelled"},
	}
	if got := SumTaskBudgets(tasks, ""); got != 400 {
		t.Fatalf("sum=%d want 400", got)
	}
}

func TestRebalanceTasksToWishBudgetScalesOverflow(t *testing.T) {
	store := NewMemoryStore(time.Hour)
	ctx := context.Background()
	contract := smart_contract.Contract{
		ContractID:      "wish-galaxian",
		Title:           "Galaxian",
		TotalBudgetSats: 1000,
		Status:          "active",
	}
	tasks := []smart_contract.Task{
		{TaskID: "t1", ContractID: "wish-galaxian", Title: "Core", BudgetSats: 1000, Status: "approved"},
		{TaskID: "t2", ContractID: "wish-galaxian", Title: "Ship", BudgetSats: 500, Status: "approved"},
		{TaskID: "t3", ContractID: "wish-galaxian", Title: "Audio", BudgetSats: 250, Status: "approved"},
		{TaskID: "t4", ContractID: "wish-galaxian", Title: "Rejected leftover", BudgetSats: 900, Status: "rejected"},
	}
	if err := store.UpsertContractWithTasks(ctx, contract, tasks); err != nil {
		t.Fatal(err)
	}
	plan, err := RebalanceTasksToWishBudget(ctx, store, "wish-galaxian", true)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Changed || plan.AllocatedBefore != 1750 || plan.AllocatedAfter != 1000 {
		t.Fatalf("dry-run plan=%+v", plan)
	}
	got, err := store.GetTask("t1")
	if err != nil {
		t.Fatal(err)
	}
	if got.BudgetSats != 1000 {
		t.Fatalf("dry-run must not write, t1=%d", got.BudgetSats)
	}

	res, err := RebalanceTasksToWishBudget(ctx, store, "wish-galaxian", false)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.AllocatedAfter != 1000 {
		t.Fatalf("rebalance=%+v", res)
	}
	var sum int64
	for _, id := range []string{"t1", "t2", "t3"} {
		got, err := store.GetTask(id)
		if err != nil {
			t.Fatal(err)
		}
		if got.BudgetSats <= 0 {
			t.Fatalf("%s budget=%d", id, got.BudgetSats)
		}
		sum += got.BudgetSats
	}
	if sum != 1000 {
		t.Fatalf("payable sum=%d want 1000", sum)
	}
	rejected, err := store.GetTask("t4")
	if err != nil {
		t.Fatal(err)
	}
	if rejected.BudgetSats != 900 {
		t.Fatalf("rejected should be left alone, got %d", rejected.BudgetSats)
	}

	again, err := RebalanceTasksToWishBudget(ctx, store, "wish-galaxian", false)
	if err != nil {
		t.Fatal(err)
	}
	if again.Changed {
		t.Fatalf("second pass should be a no-op: %+v", again)
	}
}

func TestRebalanceOpenOverBudgetSkipsSuperseded(t *testing.T) {
	store := NewMemoryStore(time.Hour)
	ctx := context.Background()
	if err := store.UpsertContractWithTasks(ctx, smart_contract.Contract{
		ContractID: "wish-open", Title: "Open", TotalBudgetSats: 1000, Status: "active",
	}, []smart_contract.Task{
		{TaskID: "open-1", ContractID: "wish-open", Title: "A", BudgetSats: 800, Status: "approved"},
		{TaskID: "open-2", ContractID: "wish-open", Title: "B", BudgetSats: 800, Status: "approved"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertContractWithTasks(ctx, smart_contract.Contract{
		ContractID: "wish-old", Title: "Old", TotalBudgetSats: 1000, Status: "superseded",
	}, []smart_contract.Task{
		{TaskID: "old-1", ContractID: "wish-old", Title: "A", BudgetSats: 2000, Status: "approved"},
	}); err != nil {
		t.Fatal(err)
	}
	results, err := RebalanceOpenOverBudget(ctx, store, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].ContractID != "wish-open" || !results[0].Changed {
		t.Fatalf("results=%+v", results)
	}
	old, err := store.GetTask("old-1")
	if err != nil {
		t.Fatal(err)
	}
	if old.BudgetSats != 2000 {
		t.Fatalf("superseded rewritten: %d", old.BudgetSats)
	}
}

func TestCreateMissingTasksLastEatsRemainder(t *testing.T) {
	store := NewMemoryStore(time.Hour)
	ctx := context.Background()
	if err := store.UpsertContractWithTasks(ctx, smart_contract.Contract{
		ContractID:          "wish-missing",
		Title:               "Missing",
		TotalBudgetSats:     1000,
		AvailableTasksCount: 3,
		Status:              "active",
	}, nil); err != nil {
		t.Fatal(err)
	}
	tasks, err := store.ListTasks(smart_contract.TaskFilter{ContractID: "wish-missing"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 3 {
		t.Fatalf("got %d tasks, want 3", len(tasks))
	}
	var sum int64
	for _, task := range tasks {
		if task.BudgetSats <= 0 {
			t.Fatalf("task %q budget %d", task.Title, task.BudgetSats)
		}
		sum += task.BudgetSats
	}
	if sum != 1000 {
		t.Fatalf("missing-task split sum=%d want 1000 (old TotalBudgetSats/N dropped remainder)", sum)
	}
}

func TestValidateNewTaskBudget(t *testing.T) {
	store := NewMemoryStore(time.Hour)
	ctx := context.Background()
	if err := store.UpsertContractWithTasks(ctx, smart_contract.Contract{
		ContractID: "wish-ef", Title: "W", TotalBudgetSats: 500, Status: "pending",
	}, []smart_contract.Task{
		{TaskID: "existing", ContractID: "wish-ef", Title: "A", BudgetSats: 400, Status: "available"},
	}); err != nil {
		t.Fatal(err)
	}
	snap, err := LoadBudgetSnapshotForContract(store, "wish-ef")
	if err != nil {
		t.Fatal(err)
	}
	if err := snap.ValidateNewTaskBudget(100); err != nil {
		t.Fatalf("100 should fit: %v", err)
	}
	if err := snap.ValidateNewTaskBudget(101); err == nil {
		t.Fatal("expected 101 to exceed remaining 100")
	}
}
