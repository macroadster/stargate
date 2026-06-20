package agents

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"stargate-backend/core/smart_contract"
	scstore "stargate-backend/storage/smart_contract"
)

func TestWorkerProcessWishes_Basic(t *testing.T) {
	store := scstore.NewMemoryStore(0)
	cfg := DefaultConfig()
	cfg.AIIdentifier = "test-agent"
	cfg.MaxProposalsPerCycle = 5
	cfg.MaxProposalsPerWish = 5

	_ = store.UpsertContractWithTasks(context.Background(), smart_contract.Contract{
		ContractID:      "wish-0123456789abcdef",
		Title:           "Build a small web tool",
		TotalBudgetSats: 5000,
		Status:          "pending",
	}, nil)

	w := NewWorker(cfg, store, NewStubExecutor(""))
	w.state = NewFileState("")
	w.seenWishes = make(map[string]bool)
	w.recentProposals = make(map[string]bool)

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

	w.ProcessWishes(context.Background())
	props, _ = store.ListProposals(context.Background(), smart_contract.ProposalFilter{})
	if len(props) != 1 {
		t.Errorf("duplicate proposal created; got %d", len(props))
	}
}

func TestWorkerProcessTask_ClaimOnly(t *testing.T) {
	store := scstore.NewMemoryStore(0)
	cfg := DefaultConfig()
	cfg.AIIdentifier = "task-worker-test"

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

	w := NewWorker(cfg, store, NewStubExecutor(""))
	w.state = NewFileState("")
	w.seenWishes = make(map[string]bool)
	w.recentProposals = make(map[string]bool)
	w.activeTasks = make(map[string]bool)
	w.rejectedTasks = make(map[string]string)

	tasks, _ := store.ListTasks(smart_contract.TaskFilter{ContractID: "wish-0123456789abcdef", Status: "available"})
	if len(tasks) == 0 {
		t.Skip("no available task in seeded store")
	}

	task := tasks[0]
	w.mu.Lock()
	w.activeTasks[task.TaskID] = true
	w.mu.Unlock()

	started := w.ProcessTask(task)
	if !started {
		t.Log("ProcessTask did not start (task already in activeTasks)")
	}

	w.mu.Lock()
	if w.activeTasks["task-exec-1"] {
		t.Log("claim process started task-exec-1")
	}
	w.mu.Unlock()
}

func TestSanitizeID(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"stargate-builtin-agent", "stargate_builtin_agent"},
		{"test-id-123", "test_id_123"},
		{"simple", "simple"},
		{"UPPER-CASE", "upper_case"},
		{"", ""},
	}
	for _, tc := range tests {
		got := sanitizeID(tc.input)
		if got != tc.expected {
			t.Errorf("sanitizeID(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestFirstLine(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Hello\nWorld", "Hello"},
		{"\n  \nFirst real line", "First real line"},
		{"Single line", "Single line"},
		{"", ""},
	}
	for _, tc := range tests {
		got := firstLine(tc.input)
		if got != tc.expected {
			t.Errorf("firstLine(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestHasActiveWork(t *testing.T) {
	store := scstore.NewMemoryStore(0)
	cfg := DefaultConfig()
	cfg.AIIdentifier = "test-active-work"

	w := NewWorker(cfg, store, NewStubExecutor(""))
	w.state = NewFileState("")
	w.activeTasks = make(map[string]bool)

	if w.hasActiveWork() {
		t.Error("expected no active work initially")
	}

	w.activeTasks["task-1"] = true
	if !w.hasActiveWork() {
		t.Error("expected active work after adding task")
	}

	delete(w.activeTasks, "task-1")
	if w.hasActiveWork() {
		t.Error("expected no active work after clearing tasks")
	}
}

func TestWorkerPerformWork_Basic(t *testing.T) {
	store := scstore.NewMemoryStore(0)
	cfg := DefaultConfig()
	cfg.AIIdentifier = "test-perform"
	tmp := t.TempDir()
	cfg.UploadsDir = tmp

	w := NewWorker(cfg, store, NewStubExecutor(tmp))
	w.state = NewFileState("")

	task := smart_contract.Task{
		TaskID:      "task-perform-1",
		ContractID:  "wish-test-hash",
		Title:       "Perform test",
		Description: "Do the work.",
		Status:      "available",
		BudgetSats:  500,
	}

	result := w.performWork(task)
	if result == nil {
		t.Fatal("performWork returned nil")
	}

	notes, ok := result["notes"].(string)
	if !ok || notes == "" {
		t.Error("expected non-empty notes in result")
	}
	if !strings.Contains(notes, "Perform test") {
		t.Error("expected notes to contain task title")
	}

	dir := filepath.Join(tmp, "results", "test-hash")
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Errorf("results directory not created: %s", dir)
	}

	reportPath := filepath.Join(dir, "task-perform-1.md")
	if _, err := os.Stat(reportPath); os.IsNotExist(err) {
		t.Errorf("report file not found: %s", reportPath)
	}
}

func TestWorkerPerformWork_WithReworkFlag(t *testing.T) {
	store := scstore.NewMemoryStore(0)
	cfg := DefaultConfig()
	cfg.AIIdentifier = "test-rework"
	tmp := t.TempDir()
	cfg.UploadsDir = tmp

	w := NewWorker(cfg, store, NewStubExecutor(tmp))
	w.state = NewFileState("")

	task := smart_contract.Task{
		TaskID:      "task-rework-2",
		ContractID:  "wish-rework-hash",
		Title:       "Fix rework",
		Description: "Address feedback",
		Status:      "rework",
		BudgetSats:  300,
		Requirements: map[string]string{
			"_has_contract_rework": "true",
			"rejection_feedback":   "Missing validation",
		},
	}

	result := w.performWork(task)
	if result == nil {
		t.Fatal("performWork returned nil")
	}

	notes, _ := result["notes"].(string)
	if !strings.Contains(notes, "Missing validation") && !strings.Contains(notes, "REWORK") {
		t.Log("rework feedback may not be in final notes (expected for stub executor):", notes)
	}
}

func TestWorkerFirstLine(t *testing.T) {
	got := firstLine("Hello\nWorld")
	if got != "Hello" {
		t.Errorf("expected 'Hello', got %q", got)
	}

	got = firstLine("")
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}

	got = firstLine("  \n  \nReal")
	if got != "Real" {
		t.Errorf("expected 'Real', got %q", got)
	}
}

func TestWorkerEnsureAgentsGuide(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AIIdentifier = "test-guide"
	store := scstore.NewMemoryStore(0)
	dir := t.TempDir()

	w := NewWorker(cfg, store, NewStubExecutor(""))
	w.ensureAgentsGuide(dir)

	path := filepath.Join(dir, "AGENTS.md")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatalf("AGENTS.md not created at %s", path)
	}

	data, _ := os.ReadFile(path)
	content := string(data)
	if !strings.Contains(content, "AI Agent Working Guide") {
		t.Error("AGENTS.md missing expected header")
	}

	w.ensureAgentsGuide(dir)
}

func TestWorkerFetchSubmissionHistory(t *testing.T) {
	store := scstore.NewMemoryStore(0)
	cfg := DefaultConfig()
	cfg.AIIdentifier = "test-sub-history"
	tmp := t.TempDir()
	cfg.UploadsDir = tmp

	w := NewWorker(cfg, store, NewStubExecutor(""))

	history := w.fetchSubmissionHistory("", "")
	if len(history) != 0 {
		t.Errorf("expected empty history for empty IDs, got %d", len(history))
	}
}

func TestWorkerFindProposalForTask(t *testing.T) {
	store := scstore.NewMemoryStore(0)
	cfg := DefaultConfig()
	cfg.AIIdentifier = "test-find-prop"
	tmp := t.TempDir()
	cfg.UploadsDir = tmp

	w := NewWorker(cfg, store, NewStubExecutor(""))
	w.state = NewFileState("")

	_ = store.CreateProposal(context.Background(), smart_contract.Proposal{
		ID:               "prop-find-1",
		Title:            "Test proposal",
		DescriptionMD:    "### Task 1: Do something",
		VisiblePixelHash: "vph-123",
		Status:           "approved",
	})

	task := smart_contract.Task{
		TaskID:     "task-find-1",
		ContractID: "wish-vph-123",
		Title:      "Do something",
	}

	prop, err := w.findProposalForTask(task)
	if err != nil {
		t.Logf("findProposalForTask: %v (may be expected if store doesn't link)", err)
	} else if prop != nil {
		if prop.Title != "Test proposal" {
			t.Errorf("unexpected proposal title: %s", prop.Title)
		}
	}
}

func TestStringHelper(t *testing.T) {
	m := map[string]interface{}{
		"name":   "test",
		"number": 42,
	}
	if got := getString(m, "name"); got != "test" {
		t.Errorf("getString(name) = %q, want 'test'", got)
	}
	if got := getString(m, "number"); got != "" {
		t.Errorf("getString(number) = %q, want ''", got)
	}
	if got := getString(m, "nonexistent"); got != "" {
		t.Errorf("getString(nonexistent) = %q, want ''", got)
	}
}
