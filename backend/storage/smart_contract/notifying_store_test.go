package smart_contract

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	core "stargate-backend/core/smart_contract"
)

func TestNotifyingStoreCallsOnMutation(t *testing.T) {
	inner := newTestSQLiteStore(t)
	ns := NewNotifyingStore(inner)

	var calls atomic.Int32
	ns.SetOnMutation(func() { calls.Add(1) })

	ctx := context.Background()
	contract := core.Contract{
		ContractID: "c-notify-1",
		Title:      "Notify Me",
		Status:     "active",
		CreatedAt:  time.Now().UTC(),
	}
	if err := ns.UpsertContractWithTasks(ctx, contract, nil); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("after upsert want 1 call, got %d", got)
	}

	if err := ns.UpdateContractStatus(ctx, contract.ContractID, "funded"); err != nil {
		t.Fatalf("status: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("after status want 2 calls, got %d", got)
	}

	if err := ns.ConfirmContract(ctx, contract.ContractID, 100, "txid-1"); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("after confirm want 3 calls, got %d", got)
	}
}

func TestNotifyingStoreNoCallbackIsSafe(t *testing.T) {
	inner := newTestSQLiteStore(t)
	ns := NewNotifyingStore(inner)
	ctx := context.Background()
	if err := ns.UpdateContractStatus(ctx, "missing", "active"); err != nil {
		t.Fatalf("status without callback: %v", err)
	}
}
