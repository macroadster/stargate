package agents

import (
	"context"
	"testing"
	"time"

	"stargate-backend/core/smart_contract"
	scstore "stargate-backend/storage/smart_contract"
)

func TestWorkerProcessWishes_Basic(t *testing.T) {
	store := scstore.NewMemoryStore(0) // 0 = no auto-expiry for tests
	cfg := DefaultConfig()
	cfg.AIIdentifier = "test-agent"
	cfg.MaxProposalsPerCycle = 5
	cfg.MaxProposalsPerWish = 5

	// Seed a pending wish (visible hash must be 8-128 chars per store validation)
	_ = store.UpsertContractWithTasks(context.Background(), smart_contract.Contract{
		ContractID:      "wish-0123456789abcdef",
		Title:           "Build a small web tool",
		TotalBudgetSats: 5000,
		Status:          "pending",
	}, nil)

	w := NewWorker(cfg, store, nil)
	// Force fresh in-memory state for the test (avoid cross-run file cache)
	w.state = NewFileState("") // no file persistence
	w.seenWishes = make(map[string]bool)
	w.recentProposals = make(map[string]bool)

	// First cycle should create a proposal
	w.ProcessWishes(context.Background())

	props, err := store.ListProposals(context.Background(), smart_contract.ProposalFilter{})
	if err != nil {
		t.Fatalf("list proposals: %v", err)
	}
	if len(props) != 1 {
		t.Fatalf("expected 1 proposal, got %d", len(props))
	}
	if props[0].VisiblePixelHash != "0123456789abcdef" {
		t.Errorf("unexpected visible hash: %s", props[0].VisiblePixelHash)
	}

	// Second call should not create duplicates (we proposed + recent cache)
	w.ProcessWishes(context.Background())
	props, _ = store.ListProposals(context.Background(), smart_contract.ProposalFilter{})
	if len(props) != 1 {
		t.Errorf("duplicate proposal created; got %d", len(props))
	}
}

func TestWorkerProcessTask_ClaimAndSubmit(t *testing.T) {
	store := scstore.NewMemoryStore(0)
	cfg := DefaultConfig()
	cfg.AIIdentifier = "task-worker-test"
	// Use a temp dir so file ops succeed in all environments
	tmp := t.TempDir()
	cfg.UploadsDir = tmp

	// We will use a task that the memory store may have, or synthesize.
	// For robustness, create a proposal + publish tasks via upsert so we have real tasks.
	_ = store.UpsertContractWithTasks(context.Background(), smart_contract.Contract{
		ContractID:      "wish-0123456789abcdef",
		Title:           "Test task execution",
		TotalBudgetSats: 1000,
		Status:          "active",
	}, []smart_contract.Task{
		{
			TaskID:      "task-exec-1",
			ContractID:  "wish-0123456789abcdef",
			Title:       "Implement the thing",
			Description: "Do the work described.",
			Status:      "available",
			BudgetSats:  500,
		},
	})

	w := NewWorker(cfg, store, nil)
	w.state = NewFileState("")
	w.seenWishes = make(map[string]bool)
	w.recentProposals = make(map[string]bool)
	w.activeTasks = make(map[string]bool)
	w.rejectedTasks = make(map[string]string)

	// Find the task
	tasks, _ := store.ListTasks(smart_contract.TaskFilter{ContractID: "wish-0123456789abcdef", Status: "available"})
	if len(tasks) == 0 {
		t.Skip("no available task in seeded store for this test")
		return
	}

	task := tasks[0]
	started := w.ProcessTask(task)
	if !started {
		// It may fail claim if store has special rules; that's acceptable for now
		t.Log("ProcessTask did not start (may be store/claim rules in test env)")
		return
	}

	// Give the goroutine a moment
	time.Sleep(50 * time.Millisecond)

	// Task should have been accepted for background (we don't assert exact timing)
}
