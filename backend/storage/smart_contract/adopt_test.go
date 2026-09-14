package smart_contract

import (
	"context"
	"strings"
	"testing"
	"time"

	core "stargate-backend/core/smart_contract"
)

func TestPlanConfirmContractIDsCanonicalIsBare(t *testing.T) {
	hash := strings.Repeat("ab", 32)
	plan := PlanConfirmContractIDs("wish-" + hash)
	if !plan.IsPixelHash {
		t.Fatal("expected pixel hash")
	}
	if plan.Canonical != hash {
		t.Fatalf("canonical=%q want bare %q", plan.Canonical, hash)
	}
	if len(plan.Aliases) != 1 || plan.Aliases[0] != "wish-"+hash {
		t.Fatalf("aliases=%v want [wish-%s]", plan.Aliases, hash)
	}
}

func TestLookupContractPrefersLiveBareOverStaleWish(t *testing.T) {
	store := NewMemoryStore(time.Hour)
	ctx := context.Background()
	hash := strings.Repeat("cd", 32)
	wishID := "wish-" + hash
	if err := store.UpsertContractWithTasks(ctx, core.Contract{
		ContractID: wishID, Title: "Stale", Status: "pending", CreatedAt: time.Now(),
	}, nil); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertContractWithTasks(ctx, core.Contract{
		ContractID: hash, Title: "Live", Status: "active", CreatedAt: time.Now(),
	}, nil); err != nil {
		t.Fatal(err)
	}
	got, err := LookupContract(store, wishID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ContractID != hash || got.Status != "active" {
		t.Fatalf("lookup by wish id returned %#v", got)
	}
	gotBare, err := LookupContract(store, hash)
	if err != nil {
		t.Fatal(err)
	}
	if gotBare.ContractID != hash {
		t.Fatalf("lookup by bare returned %#v", gotBare)
	}
}

func TestAdoptPixelHashToCanonicalWishOnly(t *testing.T) {
	store := NewMemoryStore(time.Hour)
	ctx := context.Background()
	hash := strings.Repeat("ef", 32)
	wishID := "wish-" + hash
	task := core.Task{TaskID: "t1", ContractID: wishID, Title: "A", Status: "available"}
	if err := store.UpsertContractWithTasks(ctx, core.Contract{
		ContractID: wishID, Title: "Open", Status: "pending", CreatedAt: time.Now(),
	}, []core.Task{task}); err != nil {
		t.Fatal(err)
	}
	got, err := AdoptPixelHashToCanonical(ctx, store, wishID)
	if err != nil {
		t.Fatal(err)
	}
	if got != hash {
		t.Fatalf("adopted id=%q want %q", got, hash)
	}
	live, err := store.GetContract(hash)
	if err != nil {
		t.Fatal(err)
	}
	if live.Status != "pending" || live.Title != "Open" {
		t.Fatalf("adopted row %#v", live)
	}
	if leftover, err := store.GetContract(wishID); err == nil && leftover.Status != "superseded" {
		t.Fatalf("wish- leftover status=%q", leftover.Status)
	}
	tasks := ListSiblingTasks(store, hash)
	if len(tasks) != 1 || tasks[0].ContractID != hash {
		t.Fatalf("tasks after adopt: %+v", tasks)
	}
}

func TestCollapsePixelHashTwinsPrefersBareActive(t *testing.T) {
	hash := strings.Repeat("11", 32)
	in := []core.Contract{
		{ContractID: "wish-" + hash, Status: "pending", Title: "stale"},
		{ContractID: hash, Status: "active", Title: "live"},
		{ContractID: "other", Status: "pending", Title: "keep"},
	}
	out := CollapsePixelHashTwins(in)
	if len(out) != 2 {
		t.Fatalf("got %d: %+v", len(out), out)
	}
	var sawBare bool
	for _, c := range out {
		if c.ContractID == "wish-"+hash {
			t.Fatal("stale wish- should be dropped")
		}
		if c.ContractID == hash {
			sawBare = true
		}
	}
	if !sawBare {
		t.Fatalf("missing bare: %+v", out)
	}
}

func TestSQLiteConfirmAdoptsWishOnlyOntoBare(t *testing.T) {
	store := newTestSQLiteStore(t)
	ctx := context.Background()
	hash := strings.Repeat("22", 32)
	wishID := "wish-" + hash
	if err := store.UpsertContractWithTasks(ctx, core.Contract{
		ContractID: wishID, Title: "Only wish", Status: "active", CreatedAt: time.Now(),
	}, nil); err != nil {
		t.Fatal(err)
	}
	if err := store.ConfirmContract(ctx, wishID, 10, strings.Repeat("aa", 32)); err != nil {
		t.Fatal(err)
	}
	live, err := store.GetContract(hash)
	if err != nil {
		t.Fatal(err)
	}
	if live.Status != "confirmed" {
		t.Fatalf("bare status=%q", live.Status)
	}
	if leftover, err := store.GetContract(wishID); err == nil && leftover.Status == "confirmed" {
		t.Fatalf("wish- still confirmed: %#v", leftover)
	}
}
