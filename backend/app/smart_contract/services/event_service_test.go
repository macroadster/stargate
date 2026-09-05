package services

import (
	"context"
	"testing"
	"time"

	"stargate-backend/core/smart_contract"
	scstore "stargate-backend/storage/smart_contract"
)

// hookStore overrides GetProposal so we can publish tasks that would fail
// CreateProposal validation (BudgetSats <= 0) — the leftover wish/N path.
type hookStore struct {
	*scstore.MemoryStore
	p smart_contract.Proposal
}

func (s *hookStore) GetProposal(ctx context.Context, id string) (smart_contract.Proposal, error) {
	return s.p, nil
}

func TestPublishProposalTasksDoesNotSplitWishByN(t *testing.T) {
	mem := scstore.NewMemoryStore(time.Hour)
	store := &hookStore{
		MemoryStore: mem,
		p: smart_contract.Proposal{
			ID:               "prop-zero",
			Title:            "Zero leftover",
			BudgetSats:       3000,
			VisiblePixelHash: "aa",
			Tasks: []smart_contract.Task{
				{Title: "Implement API", BudgetSats: 2000},
				{Title: "Write tests", BudgetSats: 0},
				{Title: "Write the guide", BudgetSats: 0},
			},
			Metadata: map[string]interface{}{"contract_id": "wish-aa"},
		},
	}
	svc := NewEventService(store, nil)
	if err := svc.PublishProposalTasks(context.Background(), "prop-zero"); err != nil {
		t.Fatal(err)
	}
	tasks, err := mem.ListTasks(smart_contract.TaskFilter{ContractID: "wish-aa"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 3 {
		t.Fatalf("got %d tasks, want 3", len(tasks))
	}
	var sum, funded int64
	for _, task := range tasks {
		if task.BudgetSats <= 0 {
			t.Fatalf("task %q still has budget %d", task.Title, task.BudgetSats)
		}
		if task.MerkleProof == nil {
			t.Fatalf("task %q missing merkle proof", task.Title)
		}
		if task.MerkleProof.FundedAmountSats != task.BudgetSats {
			t.Fatalf("task %q funded %d != budget %d", task.Title, task.MerkleProof.FundedAmountSats, task.BudgetSats)
		}
		sum += task.BudgetSats
		funded += task.MerkleProof.FundedAmountSats
	}
	// Old scar: explicit 2000 + wish/3 + wish/3 = 4000 on a 3000 wish.
	if sum != 3000 {
		t.Fatalf("published budgets sum to %d, want 3000 (old wish/N leftover was 4000)", sum)
	}
	if funded != 3000 {
		t.Fatalf("merkle funded sum %d, want 3000", funded)
	}
}

func TestPublishProposalTasksScalesExplicitOverflow(t *testing.T) {
	mem := scstore.NewMemoryStore(time.Hour)
	store := &hookStore{
		MemoryStore: mem,
		p: smart_contract.Proposal{
			ID:               "prop-overflow",
			Title:            "Overflow",
			BudgetSats:       1000,
			VisiblePixelHash: "bb",
			Tasks: []smart_contract.Task{
				{Title: "Core", BudgetSats: 1000},
				{Title: "Ship", BudgetSats: 500},
				{Title: "Audio", BudgetSats: 250},
			},
			Metadata: map[string]interface{}{"contract_id": "wish-bb"},
		},
	}
	svc := NewEventService(store, nil)
	if err := svc.PublishProposalTasks(context.Background(), "prop-overflow"); err != nil {
		t.Fatal(err)
	}
	tasks, err := mem.ListTasks(smart_contract.TaskFilter{ContractID: "wish-bb"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 3 {
		t.Fatalf("got %d tasks", len(tasks))
	}
	var sum int64
	for _, task := range tasks {
		if task.BudgetSats <= 0 {
			t.Fatalf("task %q budget %d", task.Title, task.BudgetSats)
		}
		sum += task.BudgetSats
	}
	// Old publish path kept the explicit 1000+500+250=1750 because it only
	// filled unset budgets. Allocator must now scale the set to 1000.
	if sum != 1000 {
		t.Fatalf("published sum=%d want 1000 (old leftover was 1750)", sum)
	}
}
