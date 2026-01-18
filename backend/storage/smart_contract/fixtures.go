package smart_contract

import (
	"time"

	"stargate-backend/core/smart_contract"
)

// SeedData returns in-memory demo contracts/tasks/proofs.
func SeedData() ([]smart_contract.Contract, []smart_contract.Task) {
	seen := time.Now().Add(-10 * time.Minute)
	confirm := time.Now().Add(-5 * time.Minute)
	proof := &smart_contract.MerkleProof{
		TxID:                  "a1b2c3d4e5f6",
		BlockHeight:           830001,
		BlockHeaderMerkleRoot: "99887766554433221100",
		ProofPath: []smart_contract.ProofNode{
			{Hash: "112233", Direction: "right"},
			{Hash: "445566", Direction: "left"},
		},
		FundedAmountSats:   5_000_000,
		ConfirmationStatus: "confirmed",
		SeenAt:             seen,
		ConfirmedAt:        &confirm,
	}

	contracts := []smart_contract.Contract{
		{
			ContractID:          "CONTRACT-550e8400",
			Title:               "Trading Bot Development",
			TotalBudgetSats:     50_000_000,
			GoalsCount:          5,
			AvailableTasksCount: 2,
			Status:              "active",
			Skills:              []string{"python", "api-integration", "finance"},
			StegoImageURL:       "", // No stego image URL for seed data
		},
	}

	tasks := []smart_contract.Task{
		{
			TaskID:         "TASK-7f3b9c2a",
			ContractID:     "CONTRACT-550e8400",
			GoalID:         "GOAL-001",
			Title:          "Implement Bollinger Bands Strategy",
			Description:    "Code BB indicator with entry/exit signals",
			BudgetSats:     5_000_000,
			Skills:         []string{"python", "finance", "testing"},
			Status:         "available",
			Difficulty:     "intermediate",
			EstimatedHours: 8,
			Requirements: map[string]string{
				"deliverables": "strategy.py,test_strategy.py,docs.md",
				"validation":   "pytest_suite",
			},
			MerkleProof: proof,
		},
		{
			TaskID:         "TASK-9d8e7f6a",
			ContractID:     "CONTRACT-550e8400",
			GoalID:         "GOAL-001",
			Title:          "Design execution risk controls",
			Description:    "Add risk guardrails to the trading bot",
			BudgetSats:     3_000_000,
			Skills:         []string{"python", "risk", "monitoring"},
			Status:         "available",
			Difficulty:     "beginner",
			EstimatedHours: 5,
			MerkleProof: &smart_contract.MerkleProof{
				TxID:                  "b2c3d4e5f6a1",
				BlockHeight:           830002,
				BlockHeaderMerkleRoot: "aa9988776655",
				ProofPath: []smart_contract.ProofNode{
					{Hash: "778899", Direction: "left"},
				},
				FundedAmountSats:   3_000_000,
				ConfirmationStatus: "provisional",
				SeenAt:             seen,
			},
		},
		{
			TaskID:         "TASK-get-pending-txs",
			ContractID:     "CONTRACT-550e8400",
			GoalID:         "GOAL-002",
			Title:          "Get Pending Transactions",
			Description:    "Get a list of pending transactions (inscriptions/smart contracts).",
			BudgetSats:     1_000_000,
			Skills:         []string{"api", "get_open_contracts"},
			Status:         "available",
			Difficulty:     "beginner",
			EstimatedHours: 1,
		},
	}

	return contracts, tasks
}
