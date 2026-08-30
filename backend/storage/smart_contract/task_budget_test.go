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
