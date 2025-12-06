package mcp

import "time"

// SeedData returns in-memory demo contracts/tasks/proofs.
func SeedData() ([]Contract, []Task) {
	seen := time.Now().Add(-10 * time.Minute)
	confirm := time.Now().Add(-5 * time.Minute)
	proof := &MerkleProof{
		TxID:                  "a1b2c3d4e5f6",
		BlockHeight:           830001,
		BlockHeaderMerkleRoot: "99887766554433221100",
		ProofPath: []ProofNode{
			{Hash: "112233", Direction: "right"},
			{Hash: "445566", Direction: "left"},
		},
		FundedAmountSats:   5_000_000,
		ConfirmationStatus: "confirmed",
		SeenAt:             seen,
		ConfirmedAt:        &confirm,
	}

	contracts := []Contract{
		{
			ContractID:          "CONTRACT-550e8400",
			Title:               "Trading Bot Development",
			TotalBudgetSats:     50_000_000,
			GoalsCount:          5,
			AvailableTasksCount: 2,
			Status:              "active",
			Skills:              []string{"python", "api-integration", "finance"},
		},
	}

	tasks := []Task{
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
			MerkleProof: &MerkleProof{
				TxID:                  "b2c3d4e5f6a1",
				BlockHeight:           830002,
				BlockHeaderMerkleRoot: "aa9988776655",
				ProofPath: []ProofNode{
					{Hash: "778899", Direction: "left"},
				},
				FundedAmountSats:   3_000_000,
				ConfirmationStatus: "provisional",
				SeenAt:             seen,
			},
		},
	}

	return contracts, tasks
}
