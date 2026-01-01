package smart_contract

import (
	"os"
	"strconv"
	"strings"
)

// DefaultBudgetSats returns a default budget for proposals/tasks.
func DefaultBudgetSats() int64 {
	if raw := os.Getenv("MCP_DEFAULT_BUDGET_SATS"); raw != "" {
		if v, err := strconv.ParseInt(raw, 10, 64); err == nil && v > 0 {
			return v
		}
	}
	// default mock budget for simulations
	return 100_000
}

// FundingAddressFromMeta extracts funding address from metadata.
func FundingAddressFromMeta(meta map[string]interface{}) string {
	if meta != nil {
		if v, ok := meta["funding_address"].(string); ok && strings.TrimSpace(v) != "" {
			return v
		}
		if v, ok := meta["address"].(string); ok && strings.TrimSpace(v) != "" {
			return v
		}
	}
	if v := os.Getenv("MCP_DEFAULT_FUNDING_ADDRESS"); strings.TrimSpace(v) != "" {
		return v
	}
	return ""
}

// budgetFromMeta extracts budget from metadata.
func budgetFromMeta(meta map[string]interface{}) int64 {
	if budget, ok := meta["budget_sats"].(int64); ok && budget > 0 {
		return budget
	}
	if budget, ok := meta["budget_sats"].(float64); ok && budget > 0 {
		return int64(budget)
	}
	if budgetStr, ok := meta["budget_sats"].(string); ok {
		if budget, err := strconv.ParseInt(budgetStr, 10, 64); err == nil && budget > 0 {
			return budget
		}
	}
	return DefaultBudgetSats()
}

func normalizeContractID(id string) string {
	id = strings.TrimSpace(id)
	if strings.HasPrefix(id, "wish-") {
		id = strings.TrimPrefix(id, "wish-")
	}
	return strings.TrimSpace(id)
}
