package smart_contract

import (
	"strings"
	"testing"
)

func TestBuildTasksFromMarkdownThreeTaskSplitSumsToWish(t *testing.T) {
	const budget int64 = 3000
	md := strings.Join([]string{
		"### Task 1: Implement the API",
		"Build the service.",
		"",
		"### Task 2: Write tests",
		"Cover the happy path.",
		"",
		"### Task 3: Write the guide",
		"Document setup.",
	}, "\n")

	tasks := BuildTasksFromMarkdown("proposal-split", md, "aa", budget, "tb1qtest")
	if len(tasks) != 3 {
		t.Fatalf("got %d tasks, want 3", len(tasks))
	}

	var sum int64
	for i, task := range tasks {
		if task.BudgetSats <= 0 {
			t.Fatalf("task %d %q has budget %d", i+1, task.Title, task.BudgetSats)
		}
		if task.MerkleProof == nil {
			t.Fatalf("task %d missing merkle proof", i+1)
		}
		if task.MerkleProof.FundedAmountSats != task.BudgetSats {
			t.Fatalf("task %d funded %d != budget %d", i+1, task.MerkleProof.FundedAmountSats, task.BudgetSats)
		}
		sum += task.BudgetSats
	}
	if sum != budget {
		t.Fatalf("live task budgets sum to %d, want exactly %d (old split was budget/1+budget/2+budget/3=%d)",
			sum, budget, budget+budget/2+budget/3)
	}
}

func TestBuildTasksFromMarkdownGenericTitlesEqualSplit(t *testing.T) {
	const budget int64 = 1000
	md := strings.Join([]string{
		"### Task 1: First slice of work",
		"one",
		"### Task 2: Second slice of work",
		"two",
		"### Task 3: Third slice of work",
		"three",
	}, "\n")

	tasks := BuildTasksFromMarkdown("proposal-eq", md, "bb", budget, "")
	if len(tasks) != 3 {
		t.Fatalf("got %d tasks", len(tasks))
	}
	var sum int64
	for _, task := range tasks {
		sum += task.BudgetSats
	}
	if sum != budget {
		t.Fatalf("sum=%d want %d amounts=%d,%d,%d", sum, budget, tasks[0].BudgetSats, tasks[1].BudgetSats, tasks[2].BudgetSats)
	}
}

func TestAllocateTaskBudgetsDoesNotOvershoot(t *testing.T) {
	// Reproduce the old live scar: pricing task i as total/i while the
	// list is still growing yields 3000+1500+1000=5500 on a 3000 wish.
	old := func(total int64, n int) int64 {
		var sum int64
		for i := 1; i <= n; i++ {
			sum += total / int64(i)
		}
		return sum
	}
	if got := old(3000, 3); got != 5500 {
		t.Fatalf("sanity: old split should be 5500, got %d", got)
	}

	tasks := BuildTasksFromMarkdown("proposal-overshoot", strings.Join([]string{
		"### Task 1: Alpha work item",
		"a",
		"### Task 2: Beta work item",
		"b",
		"### Task 3: Gamma work item",
		"c",
	}, "\n"), "cc", 3000, "")
	var sum int64
	for _, task := range tasks {
		sum += task.BudgetSats
	}
	if sum != 3000 {
		t.Fatalf("allocator still overshoots: sum=%d (old scar was %d)", sum, old(3000, 3))
	}
}

func TestBuildTasksFromMarkdownSingleTaskKeepsFullBudget(t *testing.T) {
	tasks := BuildTasksFromMarkdown("proposal-one", "no headings here", "dd", 5000, "")
	if len(tasks) != 1 {
		t.Fatalf("got %d tasks", len(tasks))
	}
	if tasks[0].BudgetSats != 5000 {
		t.Fatalf("single task budget %d", tasks[0].BudgetSats)
	}
}
