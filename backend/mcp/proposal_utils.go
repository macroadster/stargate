package mcp

import (
	"fmt"
	"strings"
)

// BuildTasksFromMarkdown derives tasks from a markdown wish. Uses bullets as tasks; fallback single task.
func BuildTasksFromMarkdown(proposalID, markdown string, visibleHash string, budget int64, fundingAddress string) []Task {
	md := strings.TrimSpace(markdown)
	lines := strings.Split(md, "\n")
	var bullets []string
	for _, l := range lines {
		trim := strings.TrimSpace(l)
		if strings.HasPrefix(trim, "- ") || strings.HasPrefix(trim, "* ") || strings.HasPrefix(trim, "+ ") {
			bullets = append(bullets, strings.TrimSpace(trim[2:]))
		}
	}
	if len(bullets) == 0 {
		bullets = []string{"Fulfill the wish described in the markdown"}
	}
	perTaskBudget := int64(0)
	if len(bullets) > 0 && budget > 0 {
		perTaskBudget = budget / int64(len(bullets))
	}

	var tasks []Task
	for i, b := range bullets {
		taskID := fmt.Sprintf("%s-task-%d", proposalID, i+1)
		tasks = append(tasks, Task{
			TaskID:      taskID,
			ContractID:  proposalID,
			GoalID:      "wish",
			Title:       b,
			Description: md,
			BudgetSats:  perTaskBudget,
			Skills:      []string{"planning", "manual-review", "proposal-discovery"},
			Status:      "available",
			MerkleProof: &MerkleProof{
				VisiblePixelHash:   visibleHash,
				FundedAmountSats:   perTaskBudget,
				FundingAddress:     fundingAddress,
				ConfirmationStatus: "provisional",
			},
		})
	}
	return tasks
}
