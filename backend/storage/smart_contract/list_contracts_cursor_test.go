package smart_contract

import (
	"context"
	"testing"
	"time"

	core "stargate-backend/core/smart_contract"
)

func TestSQLiteListContractsCursorDate(t *testing.T) {
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	// Seed three confirmed contracts with distinct confirmed_at via ConfirmContract
	// then fix confirmed_at with direct SQL for deterministic ordering.
	ids := []string{"cursor-a", "cursor-b", "cursor-c"}
	heights := []int{100, 101, 102}
	for i, id := range ids {
		c := core.Contract{
			ContractID: id,
			Title:      id,
			Status:     "active",
			CreatedAt:  time.Now().UTC(),
		}
		if err := store.UpsertContractWithTasks(ctx, c, nil); err != nil {
			t.Fatalf("upsert %s: %v", id, err)
		}
		if err := store.ConfirmContract(ctx, id, heights[i], "tx-"+id); err != nil {
			t.Fatalf("confirm %s: %v", id, err)
		}
	}

	// Force known confirmed_at values (space format as used by SQLite datetime('now'))
	times := []string{
		"2026-07-10 12:00:00",
		"2026-07-11 12:00:00",
		"2026-07-12 12:00:00",
	}
	for i, id := range ids {
		if _, err := store.db.Exec(`UPDATE mcp_contracts SET confirmed_at=? WHERE contract_id=?`, times[i], id); err != nil {
			t.Fatalf("set confirmed_at %s: %v", id, err)
		}
	}

	// First page: newest first, limit 2
	page1, err := store.ListContracts(core.ContractFilter{
		Status:             "confirmed",
		Limit:              2,
		OrderByConfirmedAt: true,
	})
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(page1) != 2 {
		t.Fatalf("page1 len=%d want 2", len(page1))
	}
	if page1[0].ContractID != "cursor-c" || page1[1].ContractID != "cursor-b" {
		t.Fatalf("page1 order got %s, %s", page1[0].ContractID, page1[1].ContractID)
	}
	if page1[1].ConfirmedAt == nil {
		t.Fatal("page1[1] missing ConfirmedAt")
	}

	// Second page via cursor_date before last of page1
	cursor := *page1[1].ConfirmedAt
	page2, err := store.ListContracts(core.ContractFilter{
		Status:             "confirmed",
		Limit:              2,
		OrderByConfirmedAt: true,
		CursorDate:         &cursor,
		CursorType:         "before",
	})
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2) != 1 || page2[0].ContractID != "cursor-a" {
		idsOut := make([]string, len(page2))
		for i, c := range page2 {
			idsOut[i] = c.ContractID
		}
		t.Fatalf("page2 want [cursor-a], got %v", idsOut)
	}
}

func TestSQLiteListContractsCursorCreatedAtOpen(t *testing.T) {
	// Open contracts have null confirmed_at; infinite scroll must page by created_at.
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	ids := []string{"open-a", "open-b", "open-c"}
	times := []string{
		"2026-07-10 12:00:00",
		"2026-07-11 12:00:00",
		"2026-07-12 12:00:00",
	}
	for i, id := range ids {
		c := core.Contract{
			ContractID: id,
			Title:      id,
			Status:     "pending",
			CreatedAt:  time.Now().UTC(),
		}
		if err := store.UpsertContractWithTasks(ctx, c, nil); err != nil {
			t.Fatalf("upsert %s: %v", id, err)
		}
		if _, err := store.db.Exec(`UPDATE mcp_contracts SET created_at=?, confirmed_at=NULL WHERE contract_id=?`, times[i], id); err != nil {
			t.Fatalf("set created_at %s: %v", id, err)
		}
	}

	page1, err := store.ListContracts(core.ContractFilter{
		Statuses:         []string{"pending", "created", "funded", "active"},
		Limit:            2,
		OrderByCreatedAt: true,
	})
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(page1) != 2 {
		t.Fatalf("page1 len=%d want 2", len(page1))
	}
	if page1[0].ContractID != "open-c" || page1[1].ContractID != "open-b" {
		t.Fatalf("page1 order got %s, %s", page1[0].ContractID, page1[1].ContractID)
	}
	if page1[1].CreatedAt.IsZero() {
		t.Fatal("page1[1] missing CreatedAt")
	}

	cursor := page1[1].CreatedAt
	page2, err := store.ListContracts(core.ContractFilter{
		Statuses:         []string{"pending", "created", "funded", "active"},
		Limit:            2,
		OrderByCreatedAt: true,
		CursorDate:       &cursor,
		CursorType:       "before",
	})
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2) != 1 || page2[0].ContractID != "open-a" {
		idsOut := make([]string, len(page2))
		for i, c := range page2 {
			idsOut[i] = c.ContractID
		}
		t.Fatalf("page2 want [open-a], got %v", idsOut)
	}
}

func TestSQLiteListContractsCursorDateRFC3339Mix(t *testing.T) {
	// Ensure RFC3339 stored values still compare correctly against cursor.
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	for _, id := range []string{"rfc-old", "rfc-new"} {
		c := core.Contract{ContractID: id, Title: id, Status: "active", CreatedAt: time.Now().UTC()}
		if err := store.UpsertContractWithTasks(ctx, c, nil); err != nil {
			t.Fatalf("upsert: %v", err)
		}
		if err := store.ConfirmContract(ctx, id, 50, "tx-"+id); err != nil {
			t.Fatalf("confirm: %v", err)
		}
	}
	if _, err := store.db.Exec(`UPDATE mcp_contracts SET confirmed_at=? WHERE contract_id=?`, "2026-06-01T10:00:00Z", "rfc-old"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`UPDATE mcp_contracts SET confirmed_at=? WHERE contract_id=?`, "2026-06-15 10:00:00", "rfc-new"); err != nil {
		t.Fatal(err)
	}

	cursor := time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC)
	older, err := store.ListContracts(core.ContractFilter{
		Status:             "confirmed",
		Limit:              10,
		OrderByConfirmedAt: true,
		CursorDate:         &cursor,
		CursorType:         "before",
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(older) != 1 || older[0].ContractID != "rfc-old" {
		t.Fatalf("want only rfc-old, got %+v", older)
	}
}
