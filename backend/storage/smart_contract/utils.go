package smart_contract

import (
	"os"
	"strconv"
	"strings"

	"stargate-backend/core/identity"
)

// DefaultBudgetSats returns a default budget for proposals/tasks.
func DefaultBudgetSats() int64 {
	if raw := os.Getenv("STARGATE_DEFAULT_BUDGET_SATS"); raw != "" {
		if v, err := strconv.ParseInt(raw, 10, 64); err == nil && v > 0 {
			return v
		}
	}
	return 1000
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

func NormalizeContractID(id string) string {
	return identity.Normalize(id)
}

// ToWishID converts a hash to the standard wish ID format
func ToWishID(hash string) string {
	return identity.ToWishID(hash)
}

// IsValidHash checks if a string looks like a valid hash (64 hex chars)
