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

func TestFoldContractPrefixTwinOntoBareHash(t *testing.T) {
	store := NewMemoryStore(time.Hour)
	ctx := context.Background()
	hash := strings.Repeat("ab", 32)
	alias := "contract-" + hash
	bareCreated := time.Date(2026, 10, 4, 23, 58, 22, 0, time.UTC)
	if err := store.UpsertContractWithTasks(ctx, core.Contract{
		ContractID: hash, Title: "Shell", Status: "pending", TotalBudgetSats: 1000,
		GoalsCount: 0, CreatedAt: bareCreated,
	}, nil); err != nil {
		t.Fatal(err)
	}
	// Upsert now refuses to create this alias. The fold still has to collapse
	// a row that was stored before that guard, so plant it directly.
	claimed := time.Date(2026, 10, 5, 0, 9, 0, 0, time.UTC)
	store.mu.Lock()
	store.contracts[alias] = core.Contract{
		ContractID: alias, Title: "Game", Status: "active", TotalBudgetSats: 1000,
		GoalsCount: 1, CreatedAt: bareCreated.Add(time.Hour),
	}
	store.tasks[hash+"-task-1"] = core.Task{
		TaskID: hash + "-task-1", ContractID: alias, GoalID: "wish", Title: "Shelf",
		Status: "submitted", BudgetSats: 1000, ClaimedBy: "tb1qkeeper",
		ClaimedAt:   &claimed,
		MerkleProof: &core.MerkleProof{VisiblePixelHash: hash},
	}
	store.mu.Unlock()
	if err := store.UpsertContractWithTasks(ctx, core.Contract{
		ContractID: "contract-001", Title: "Keep", Status: "active", CreatedAt: bareCreated,
	}, nil); err != nil {
		t.Fatal(err)
	}

	n, err := FoldContractPrefixTwins(ctx, store)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("folded %d want 1", n)
	}
	live, err := store.GetContract(hash)
	if err != nil {
		t.Fatal(err)
	}
	if live.Status != "active" || live.GoalsCount != 1 || live.Title != "Game" {
		t.Fatalf("bare row after fold: %#v", live)
	}
	if !live.CreatedAt.Equal(bareCreated) {
		t.Fatalf("created_at=%s want %s", live.CreatedAt, bareCreated)
	}
	leftover, err := store.GetContract(alias)
	if err != nil || leftover.Status != "superseded" {
		t.Fatalf("alias after fold: %#v err=%v", leftover, err)
	}
	tasks := ListSiblingTasks(store, hash)
	if len(tasks) != 1 || tasks[0].ContractID != hash || tasks[0].Status != "submitted" || tasks[0].ClaimedBy != "tb1qkeeper" {
		t.Fatalf("tasks after fold: %+v", tasks)
	}
	if tasks[0].MerkleProof == nil || tasks[0].MerkleProof.VisiblePixelHash != hash {
		t.Fatalf("proof after fold: %+v", tasks[0].MerkleProof)
	}
	kept, err := store.GetContract("contract-001")
	if err != nil || kept.Status != "active" {
		t.Fatalf("contract-001: %#v err=%v", kept, err)
	}
	again, err := FoldContractPrefixTwins(ctx, store)
	if err != nil || again != 0 {
		t.Fatalf("second fold n=%d err=%v", again, err)
	}
	byAlias, err := LookupContract(store, alias)
	if err != nil || byAlias.ContractID != hash || byAlias.Status != "active" {
		t.Fatalf("lookup alias: %#v err=%v", byAlias, err)
	}
}

func TestSQLiteFoldContractPrefixKeepsSubmittedTask(t *testing.T) {
	store := newTestSQLiteStore(t)
	ctx := context.Background()
	hash := strings.Repeat("cd", 32)
	alias := "contract-" + hash
	bareCreated := time.Date(2026, 10, 4, 23, 58, 22, 0, time.UTC)
	// Plant the alias the way a pre-guard database still has it. Write the live
	// row through upsert (so columns match what GetTask scans), then move the
	// primary key onto the alias. A direct alias upsert would hide the fold.
	claimed := time.Date(2026, 10, 5, 0, 9, 0, 0, time.UTC)
	if err := store.UpsertContractWithTasks(ctx, core.Contract{
		ContractID: hash, Title: "Game", Status: "active", TotalBudgetSats: 1000,
		GoalsCount: 1, CreatedAt: bareCreated.Add(time.Hour),
	}, []core.Task{{
		TaskID: hash + "-task-1", ContractID: hash, Title: "Shelf", Status: "submitted",
		BudgetSats: 1000, ClaimedBy: "tb1qkeeper", ClaimedAt: &claimed,
		MerkleProof: &core.MerkleProof{VisiblePixelHash: hash, ContractorWallet: "tb1qkeeper"},
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`UPDATE mcp_contracts SET contract_id=? WHERE contract_id=?`, alias, hash); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`UPDATE mcp_tasks SET contract_id=? WHERE task_id=?`, alias, hash+"-task-1"); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertContractWithTasks(ctx, core.Contract{
		ContractID: hash, Title: "Shell", Status: "pending", TotalBudgetSats: 1000,
		CreatedAt: bareCreated,
	}, nil); err != nil {
		t.Fatal(err)
	}
	n, err := FoldContractPrefixTwins(ctx, store)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("folded %d want 1", n)
	}
	live, err := store.GetContract(hash)
	if err != nil {
		t.Fatal(err)
	}
	if live.Status != "active" || live.GoalsCount != 1 {
		t.Fatalf("bare sqlite row: %#v", live)
	}
	task, err := store.GetTask(hash + "-task-1")
	if err != nil {
		t.Fatal(err)
	}
	if task.ContractID != hash || task.Status != "submitted" || task.ClaimedBy != "tb1qkeeper" {
		t.Fatalf("task after sqlite fold: %+v", task)
	}
	if task.MerkleProof == nil || task.MerkleProof.VisiblePixelHash != hash {
		t.Fatalf("proof after sqlite fold: %+v", task.MerkleProof)
	}
	leftover, err := store.GetContract(alias)
	if err != nil || leftover.Status != "superseded" {
		t.Fatalf("alias status=%q err=%v", leftover.Status, err)
	}
}

func TestUpsertPixelAliasLandsOnBareHash(t *testing.T) {
	store := NewMemoryStore(time.Hour)
	ctx := context.Background()
	hash := strings.Repeat("a1", 32)
	alias := "contract-" + strings.ToUpper(hash)
	if err := store.UpsertContractWithTasks(ctx, core.Contract{
		ContractID: alias, Title: "Revived", Status: "active", TotalBudgetSats: 1000,
		CreatedAt: time.Now(),
	}, []core.Task{{
		TaskID: hash + "-task-1", ContractID: alias, Title: "Shelf", Status: "available", BudgetSats: 1000,
	}}); err != nil {
		t.Fatal(err)
	}
	live, err := store.GetContract(hash)
	if err != nil || live.Status != "active" || live.Title != "Revived" {
		t.Fatalf("bare row: %#v err=%v", live, err)
	}
	if _, err := store.GetContract(alias); err == nil {
		t.Fatal("upsert created a contract- alias row")
	}
	if _, err := store.GetContract("contract-" + hash); err == nil {
		t.Fatal("upsert created a lowercase contract- alias row")
	}
	tasks := ListSiblingTasks(store, hash)
	if len(tasks) != 1 || tasks[0].ContractID != hash {
		t.Fatalf("tasks: %+v", tasks)
	}
	// A non-hash contract- id is a real primary key, not an alias.
	if err := store.UpsertContractWithTasks(ctx, core.Contract{
		ContractID: "contract-001", Title: "Keep", Status: "active", CreatedAt: time.Now(),
	}, nil); err != nil {
		t.Fatal(err)
	}
	kept, err := store.GetContract("contract-001")
	if err != nil || kept.Status != "active" {
		t.Fatalf("contract-001: %#v err=%v", kept, err)
	}
}

func TestUpsertAfterFoldDoesNotReviveAlias(t *testing.T) {
	store := NewMemoryStore(time.Hour)
	ctx := context.Background()
	hash := strings.Repeat("b2", 32)
	alias := "contract-" + hash
	if err := store.UpsertContractWithTasks(ctx, core.Contract{
		ContractID: hash, Title: "Shell", Status: "pending", CreatedAt: time.Now(),
	}, nil); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertContractWithTasks(ctx, core.Contract{
		ContractID: alias, Title: "Game", Status: "active", CreatedAt: time.Now(),
	}, []core.Task{{
		TaskID: hash + "-task-1", ContractID: alias, Title: "Shelf", Status: "submitted", BudgetSats: 1000,
	}}); err != nil {
		t.Fatal(err)
	}
	// The alias write already landed on the bare hash, so the startup fold
	// has nothing left to move. A second alias write must not reopen it.
	n, err := FoldContractPrefixTwins(ctx, store)
	if err != nil || n != 0 {
		t.Fatalf("fold n=%d err=%v", n, err)
	}
	if err := store.UpsertContractWithTasks(ctx, core.Contract{
		ContractID: alias, Title: "Ghost", Status: "active", CreatedAt: time.Now(),
	}, nil); err != nil {
		t.Fatal(err)
	}
	live, err := store.GetContract(hash)
	if err != nil || live.Status != "active" || live.Title != "Ghost" {
		t.Fatalf("bare after rewrite: %#v err=%v", live, err)
	}
	if _, err := store.GetContract(alias); err == nil {
		t.Fatal("alias row revived")
	}
	listed, err := store.ListContracts(core.ContractFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range listed {
		if c.ContractID == alias && !strings.EqualFold(c.Status, "superseded") {
			t.Fatalf("live alias in list: %#v", c)
		}
	}
}

func TestSQLiteUpsertPixelAliasLandsOnBareHash(t *testing.T) {
	store := newTestSQLiteStore(t)
	ctx := context.Background()
	hash := strings.Repeat("c3", 32)
	alias := "contract-" + hash
	if err := store.UpsertContractWithTasks(ctx, core.Contract{
		ContractID: alias, Title: "Game", Status: "active", TotalBudgetSats: 1000,
		CreatedAt: time.Now(),
	}, []core.Task{{
		TaskID: hash + "-task-1", ContractID: alias, Title: "Shelf", Status: "submitted", BudgetSats: 1000,
	}}); err != nil {
		t.Fatal(err)
	}
	live, err := store.GetContract(hash)
	if err != nil || live.Status != "active" || live.Title != "Game" {
		t.Fatalf("bare sqlite row: %#v err=%v", live, err)
	}
	if _, err := store.GetContract(alias); err == nil {
		t.Fatal("sqlite upsert created a contract- alias row")
	}
	task, err := store.GetTask(hash + "-task-1")
	if err != nil || task.ContractID != hash || task.Status != "submitted" {
		t.Fatalf("task: %+v err=%v", task, err)
	}

	// An alias row that already exists (written before this guard) must be
	// superseded when the same id is upserted again, not left active.
	if _, err := store.db.Exec(`INSERT INTO mcp_contracts (contract_id, title, status, created_at) VALUES (?,?,?,?)`,
		"wish-"+hash, "Old", "active", time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertContractWithTasks(ctx, core.Contract{
		ContractID: "wish-" + hash, Title: "Rewritten", Status: "funded", CreatedAt: time.Now(),
	}, nil); err != nil {
		t.Fatal(err)
	}
	bare, err := store.GetContract(hash)
	if err != nil || bare.Status != "funded" || bare.Title != "Rewritten" {
		t.Fatalf("bare after wish- rewrite: %#v err=%v", bare, err)
	}
	old, err := store.GetContract("wish-" + hash)
	if err != nil || old.Status != "superseded" {
		t.Fatalf("wish- alias after rewrite: %#v err=%v", old, err)
	}
}

func TestCollapseGroupsContractPrefixWithBare(t *testing.T) {
	hash := strings.Repeat("ef", 32)
	in := []core.Contract{
		{ContractID: hash, Status: "active", Title: "live"},
		{ContractID: "contract-" + hash, Status: "superseded", Title: "alias"},
		{ContractID: "contract-001", Status: "active", Title: "keep"},
	}
	out := CollapsePixelHashTwins(in)
	if len(out) != 2 {
		t.Fatalf("got %d: %+v", len(out), out)
	}
	for _, c := range out {
		if c.ContractID == "contract-"+hash {
			t.Fatal("superseded contract- alias should be dropped")
		}
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
