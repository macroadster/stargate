package agents

import (
	"context"
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
