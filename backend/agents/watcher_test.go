package agents

import (
	"context"
	"strings"
	"testing"

	"stargate-backend/core/smart_contract"
	scstore "stargate-backend/storage/smart_contract"
)

func TestWatcherAuditsAndAutoApproves(t *testing.T) {
	store := scstore.NewMemoryStore(0)
	cfg := DefaultConfig()
	cfg.AIIdentifier = "test-watcher"
	cfg.DonationAddress = "bc1qtestdonationaddressforautonomousagent"
	cfg.MaxProposalsPerWish = 10

	// Create a wish
	_ = store.UpsertContractWithTasks(context.Background(), smart_contract.Contract{
		ContractID:      "wish-0123456789abcdef",
		Title:           "Implement a todo list app with tests",
		TotalBudgetSats: 10000,
		Status:          "pending",
	}, nil)

	// Create a reasonable proposal
	prop := smart_contract.Proposal{
		ID:               "prop-1",
		Title:            "Build todo app",
		DescriptionMD:    "Detailed plan:\n\n### Task 1: Scaffold\nCreate React app with routing and state.\n\n### Task 2: Core features\nImplement add/edit/delete with persistence.",
		VisiblePixelHash: "0123456789abcdef",
		BudgetSats:       2000,
		Status:           "pending",
	}
	_ = store.CreateProposal(context.Background(), prop)

	w := NewWatcher(cfg, store)
	w.state = NewFileState("") // isolate

	tasks := w.RunOnce(context.Background())

	// After auto-approve + publish, we should have tasks surfaced
	if len(tasks) == 0 {
		t.Errorf("expected at least 1 task after auto-approve, got 0")
	}
	props, _ := store.ListProposals(context.Background(), smart_contract.ProposalFilter{ProposalID: "prop-1"})
	if len(props) > 0 && props[0].Status == "pending" {
		t.Errorf("expected proposal to be approved or published, still '%s'", props[0].Status)
	}
}

func TestWatcherValidateBudgetSanity(t *testing.T) {
	cfg := DefaultConfig()
	store := scstore.NewMemoryStore(0)
	w := NewWatcher(cfg, store)

	ok, reason := w.validateBudgetSanity(smart_contract.Proposal{BudgetSats: 0}, 1000)
	if ok || reason == "" {
		t.Error("expected rejection for zero budget")
	}

	ok, reason = w.validateBudgetSanity(smart_contract.Proposal{BudgetSats: 101}, 10)
	if ok || reason == "" {
		t.Errorf("expected rejection for budget > 10x contract budget, got ok=%v reason=%s", ok, reason)
	}

	ok, reason = w.validateBudgetSanity(smart_contract.Proposal{BudgetSats: 1_000_000_000}, 1000)
	if ok || reason == "" {
		t.Error("expected rejection for extremely high budget")
	}

	ok, reason = w.validateBudgetSanity(smart_contract.Proposal{BudgetSats: 500}, 1000)
	if !ok {
		t.Errorf("expected approval for reasonable budget, got reason: %s", reason)
	}
}

func TestWatcherShouldAutoApprove(t *testing.T) {
	w1 := NewWatcher(DefaultConfig(), scstore.NewMemoryStore(0))
	if w1.shouldAutoApprove() {
		t.Error("should not auto-approve without donation address")
	}

	cfg2 := DefaultConfig()
	cfg2.DonationAddress = "bc1qtest"
	w2 := NewWatcher(cfg2, scstore.NewMemoryStore(0))
	if !w2.shouldAutoApprove() {
		t.Error("should auto-approve with donation address")
	}
}

func TestWatcherAuditProposal(t *testing.T) {
	cfg := DefaultConfig()
	store := scstore.NewMemoryStore(0)
	w := NewWatcher(cfg, store)

	ok, reason := w.auditProposal(context.Background(), smart_contract.Proposal{
		Title:         "Short",
		DescriptionMD: "Too short",
	})
	if ok {
		t.Error("expected rejection for short description")
	}
	if !strings.Contains(reason, "detail") {
		t.Errorf("expected reason about detail, got: %s", reason)
	}

	ok, reason = w.auditProposal(context.Background(), smart_contract.Proposal{
		Title:         "This is a joke proposal for fun",
		DescriptionMD: "### Task 1: Do something\nThis is a joke test with sufficient length to pass the minimum check.",
	})
	if ok {
		t.Error("expected rejection for joke content")
	}
}

func TestWatcherIsRecursiveProposal(t *testing.T) {
	cfg := DefaultConfig()
	store := scstore.NewMemoryStore(0)
	w := NewWatcher(cfg, store)

	if !w.isRecursiveProposal(smart_contract.Proposal{
		Title:         "Create a proposal for the proposal",
		DescriptionMD: "I will create another proposal.",
	}) {
		t.Error("expected recursive detection")
	}

	if w.isRecursiveProposal(smart_contract.Proposal{
		Title:         "Build a todo app",
		DescriptionMD: "### Task 1: Implement features",
	}) {
		t.Error("non-recursive proposal flagged as recursive")
	}
}

func TestWatcherCheckResources(t *testing.T) {
	cfg := DefaultConfig()
	store := scstore.NewMemoryStore(0)
	w := NewWatcher(cfg, store)
	w.checkResources()
}

func TestWatcherRejectsBadProposal(t *testing.T) {
	store := scstore.NewMemoryStore(0)
	cfg := DefaultConfig()
	cfg.AIIdentifier = "test-watcher2"
	cfg.DonationAddress = "bc1qdonate"

	_ = store.UpsertContractWithTasks(context.Background(), smart_contract.Contract{
		ContractID:      "wish-abcdef0123456789",
		Title:           "Real feature request",
		TotalBudgetSats: 5000,
		Status:          "pending",
	}, nil)

	bad := smart_contract.Proposal{
		ID:               "bad-prop",
		Title:            "Proposal for the proposal",
		DescriptionMD:    "I will create another proposal to solve this.",
		VisiblePixelHash: "abcdef0123456789",
		BudgetSats:       100,
		Status:           "pending",
	}
	_ = store.CreateProposal(context.Background(), bad)

	w := NewWatcher(cfg, store)
	w.state = NewFileState("")
	w.RunOnce(context.Background())

	props, _ := store.ListProposals(context.Background(), smart_contract.ProposalFilter{ProposalID: "bad-prop"})
	if len(props) > 0 {
		if props[0].Status != "rejected" {
			t.Errorf("expected proposal status 'rejected', got '%s'", props[0].Status)
		}
	} else {
		t.Error("expected proposal to exist after audit")
	}
}
