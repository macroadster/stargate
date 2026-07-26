package auth

import (
	"path/filepath"
	"testing"
)

// TestGORMAPIKeySurvivesReopen simulates a process restart: issue a key, close
// the store (release SQLite), open again, and validate. Regression for
// stargate-6wu — AutoMigrate used to fail with "table already exists" and the
// factory fell back to an empty in-memory key store.
func TestGORMAPIKeySurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api_keys.db")

	store1, err := NewSQLiteAPIKeyStore(path)
	if err != nil {
		t.Fatalf("open1: %v", err)
	}
	rec, err := store1.Issue("a@b.c", "bc1qtest", "test")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if !store1.Validate(rec.Key) {
		t.Fatal("validate before close failed")
	}
	if err := store1.Close(); err != nil {
		t.Fatalf("close1: %v", err)
	}

	store2, err := NewSQLiteAPIKeyStore(path)
	if err != nil {
		t.Fatalf("open2: %v", err)
	}
	defer store2.Close()

	if !store2.Validate(rec.Key) {
		var rows []APIKeyRow
		_ = store2.db.Find(&rows).Error
		t.Fatalf("key invalid after reopen; key=%s path=%s rows=%+v", rec.Key, path, rows)
	}
	got, ok := store2.Get(rec.Key)
	if !ok {
		t.Fatal("Get failed after reopen")
	}
	if got.Email != "a@b.c" || got.Wallet != "bc1qtest" {
		t.Fatalf("metadata mismatch after reopen: %+v", got)
	}

	// Third open: AutoMigrate + index creation must stay idempotent.
	if err := store2.Close(); err != nil {
		t.Fatalf("close2: %v", err)
	}
	store3, err := NewSQLiteAPIKeyStore(path)
	if err != nil {
		t.Fatalf("open3: %v", err)
	}
	defer store3.Close()
	if !store3.Validate(rec.Key) {
		t.Fatal("key invalid after third open")
	}
}
