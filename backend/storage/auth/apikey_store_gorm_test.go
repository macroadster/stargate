package auth

import (
	"path/filepath"
	"testing"
)

func TestGORMAPIKeyStoreSQLiteRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api_keys.db")

	store, err := NewSQLiteAPIKeyStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteAPIKeyStore: %v", err)
	}
	defer store.Close()

	rec, err := store.Issue("a@b.c", "bc1qtest", "test")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if rec.Key == "" {
		t.Fatal("expected non-empty key")
	}
	if !store.Validate(rec.Key) {
		t.Fatal("Validate should pass for issued key")
	}
	got, ok := store.Get(rec.Key)
	if !ok {
		t.Fatal("Get should find issued key")
	}
	if got.Email != "a@b.c" || got.Wallet != "bc1qtest" {
		t.Fatalf("Get mismatch: %+v", got)
	}

	// Same wallet again must succeed (login re-sends already-bound wallet).
	// SQLite/GORM reports RowsAffected=0 for no-op UPDATE; must not error.
	same, err := store.UpdateWallet(rec.Key, "bc1qtest")
	if err != nil {
		t.Fatalf("UpdateWallet same wallet (login path): %v", err)
	}
	if same.Wallet != "bc1qtest" {
		t.Fatalf("wallet changed on no-op update: %s", same.Wallet)
	}

	updated, err := store.UpdateWallet(rec.Key, "bc1qnew")
	if err != nil {
		t.Fatalf("UpdateWallet: %v", err)
	}
	if updated.Wallet != "bc1qnew" {
		t.Fatalf("wallet not updated: %s", updated.Wallet)
	}

	if _, err := store.UpdateWallet("not-a-real-key", "bc1qx"); err == nil {
		t.Fatal("UpdateWallet missing key should error")
	}

	store.Seed("seedkey1234567890", "seed@x", "seed")
	if !store.Validate("seedkey1234567890") {
		t.Fatal("seeded key should validate")
	}

	if err := store.InvalidateByWallet("bc1qnew"); err != nil {
		t.Fatalf("InvalidateByWallet: %v", err)
	}
	if store.Validate(rec.Key) {
		t.Fatal("key should be gone after invalidate")
	}
}

func TestGORMAPIKeyStoreMemory(t *testing.T) {
	store, err := NewMemoryGORMAPIKeyStore()
	if err != nil {
		t.Fatalf("NewMemoryGORMAPIKeyStore: %v", err)
	}
	defer store.Close()

	rec, err := store.Issue("", "w1", "t")
	if err != nil {
		t.Fatal(err)
	}
	if !store.Validate(rec.Key) {
		t.Fatal("memory validate")
	}
}

func TestGORMAPIKeyStoreEnvSeed(t *testing.T) {
	t.Setenv("STARGATE_API_KEY", "env-test-key-abcdef")
	t.Setenv("STARLIGHT_DONATION_ADDRESS", "bc1qdonation")

	store, err := NewMemoryGORMAPIKeyStore()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.SeedEnvironmentVariables()

	if !store.Validate("env-test-key-abcdef") {
		t.Fatal("env-seeded key should validate")
	}
	got, ok := store.Get("env-test-key-abcdef")
	if !ok || got.Wallet != "bc1qdonation" {
		t.Fatalf("expected donation wallet bind, got %+v ok=%v", got, ok)
	}
}

func TestPlanEnvSeed(t *testing.T) {
	t.Setenv("STARGATE_API_KEY", "k")
	t.Setenv("STARLIGHT_DONATION_ADDRESS", "w")
	plan := PlanEnvSeed()
	if plan.BindKey != "k" || plan.BindWallet != "w" {
		t.Fatalf("bind plan: %+v", plan)
	}
}
