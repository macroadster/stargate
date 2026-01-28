package smart_contract

import (
	"os"
	"strconv"
	"strings"
)

// DefaultBudgetSats returns a default budget for proposals/tasks.
func DefaultBudgetSats() int64 {
	if raw := os.Getenv("STARGATE_DEFAULT_BUDGET_SATS"); raw != "" {
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
	if v := os.Getenv("STARGATE_DEFAULT_FUNDING_ADDRESS"); strings.TrimSpace(v) != "" {
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

func NormalizeContractID(id string) string {
	id = strings.TrimSpace(id)

	// Remove common prefixes to get the canonical hash
	prefixes := []string{"wish-", "proposal-", "task-"}
	for _, prefix := range prefixes {
		if strings.HasPrefix(id, prefix) {
			id = strings.TrimPrefix(id, prefix)
			break // Only remove one prefix to avoid issues with compound prefixes
		}
	}

	return strings.TrimSpace(id)
}

// ToWishID converts a hash to the standard wish ID format
func ToWishID(hash string) string {
	normalized := NormalizeContractID(hash)
	if normalized == "" {
		return ""
	}
	return "wish-" + normalized
}

// IsValidHash checks if a string looks like a valid hash (64 hex chars)
func IsValidHash(hash string) bool {
	normalized := NormalizeContractID(hash)
	if len(normalized) != 64 {
		return false
	}
	// Basic hex validation
	for _, c := range normalized {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}
