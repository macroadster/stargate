package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"stargate-backend/core/smart_contract"
	scstore "stargate-backend/storage/smart_contract"
)

func TestClaimTaskWithAmountRespectsWishBudget(t *testing.T) {
	store := scstore.NewMemoryStore(time.Hour)
	ctx := context.Background()
	if err := store.UpsertContractWithTasks(ctx, smart_contract.Contract{
		ContractID:      "wish-claim",
		Title:           "Wish",
		TotalBudgetSats: 1000,
		Status:          "active",
	}, []smart_contract.Task{
		{TaskID: "t-a", ContractID: "wish-claim", Title: "A", BudgetSats: 400, Status: "available"},
		{TaskID: "t-b", ContractID: "wish-claim", Title: "B", BudgetSats: 400, Status: "available"},
	}); err != nil {
		t.Fatal(err)
	}
	svc := NewClaimService(store)
	amount := int64(600)
	res, err := svc.ClaimTaskWithAmount(ctx, "t-a", "tb1qwallet", nil, &amount)
	if err != nil {
		t.Fatalf("claim 600: %v", err)
	}
	if res.AmountSats != 600 {
		t.Fatalf("amount=%d", res.AmountSats)
	}
	if res.WishBudgetSats != 1000 {
		t.Fatalf("wish=%d", res.WishBudgetSats)
	}
	tooMuch := int64(401)
	if _, err := svc.ClaimTaskWithAmount(ctx, "t-b", "tb1qother", nil, &tooMuch); err == nil {
		t.Fatal("expected 401 on t-b to exceed remaining 400")
	} else if !strings.Contains(err.Error(), "exceeds remaining wish budget") {
		t.Fatalf("unexpected error: %v", err)
	}
	ok := int64(400)
	resB, err := svc.ClaimTaskWithAmount(ctx, "t-b", "tb1qother", nil, &ok)
	if err != nil {
		t.Fatalf("claim t-b 400: %v", err)
	}
	if resB.RemainingSats != 0 {
		t.Fatalf("remaining=%d", resB.RemainingSats)
	}
}
