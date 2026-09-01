package smart_contract

import (
	"testing"
)

func TestDefaultBudgetSatsIsOneThousand(t *testing.T) {
	t.Setenv("STARGATE_DEFAULT_BUDGET_SATS", "")
	if got := DefaultBudgetSats(); got != 1000 {
		t.Fatalf("DefaultBudgetSats()=%d want 1000", got)
	}
}

func TestDefaultBudgetSatsHonorsEnv(t *testing.T) {
	t.Setenv("STARGATE_DEFAULT_BUDGET_SATS", "2500")
	if got := DefaultBudgetSats(); got != 2500 {
		t.Fatalf("DefaultBudgetSats()=%d want 2500", got)
	}
}
