package auth

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// Fresh AutoMigrate schema uses DATETIME for created_at — baseline.
func TestGORMCreatedAtFreshSchema(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t.db")
	store, err := NewSQLiteAPIKeyStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	rec, err := store.Issue("a@b.c", "w1", "test")
	if err != nil {
		t.Fatal(err)
	}
	got, ok := store.Get(rec.Key)
	if !ok {
		t.Fatal("Get failed after Issue on fresh schema")
	}
	if got.Wallet != "w1" {
		t.Fatalf("wallet=%q", got.Wallet)
	}
}

// Production api_keys.db was created with created_at TEXT (pre-GORM). Scanning
// those rows into time.Time fails with modernc unless SQLTime is used.
// Regression for stargate-691 (API key login 500 "failed to bind wallet").
func TestGORMTimeScanLegacyTextSchema(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.db")

	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = raw.Exec(`
CREATE TABLE api_keys (
  key_hash TEXT PRIMARY KEY,
  email TEXT,
  wallet_address TEXT,
  source TEXT,
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
`); err != nil {
		t.Fatal(err)
	}
	if _, err = raw.Exec(
		`INSERT INTO api_keys (key_hash, email, wallet_address, source, created_at) VALUES (?, '', 'tb1qwallet', 'seed', ?)`,
		hashAPIKey("seed-key"), "2026-03-09T20:49:02Z",
	); err != nil {
		t.Fatal(err)
	}
	raw.Close()

	store, err := NewSQLiteAPIKeyStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteAPIKeyStore: %v", err)
	}
	defer store.Close()

	if !store.Validate("seed-key") {
		t.Fatal("Validate should pass")
	}
	got, found := store.Get("seed-key")
	if !found {
		t.Fatal("Get must work on legacy TEXT created_at (login needs wallet metadata)")
	}
	if got.Wallet != "tb1qwallet" {
		t.Fatalf("wallet=%q", got.Wallet)
	}
	if _, err := store.UpdateWallet("seed-key", "tb1qwallet"); err != nil {
		t.Fatalf("UpdateWallet same wallet (login path): %v", err)
	}

	rec, err := store.Issue("", "w2", "test")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, ok := store.Get(rec.Key); !ok {
		t.Fatal("Issue+Get must work on legacy TEXT schema")
	}
}

// Unparseable created_at used to make Get/First fail after Validate (COUNT) passed,
// and login then 500'd on UpdateWallet. Must stay a successful metadata read.
func TestGORMTimeScanGarbageCreatedAt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "garbage.db")

	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = raw.Exec(`
CREATE TABLE api_keys (
  key_hash TEXT PRIMARY KEY,
  email TEXT,
  wallet_address TEXT,
  source TEXT,
  created_at TEXT NOT NULL
);
`); err != nil {
		t.Fatal(err)
	}
	if _, err = raw.Exec(
		`INSERT INTO api_keys (key_hash, email, wallet_address, source, created_at) VALUES (?, '', 'tb1qgarbage', 'seed', ?)`,
		hashAPIKey("garbage-key"), "not-a-real-timestamp",
	); err != nil {
		t.Fatal(err)
	}
	raw.Close()

	store, err := NewSQLiteAPIKeyStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if !store.Validate("garbage-key") {
		t.Fatal("Validate should pass")
	}
	got, found := store.Get("garbage-key")
	if !found {
		t.Fatal("Get must succeed when created_at is garbage")
	}
	if got.Wallet != "tb1qgarbage" {
		t.Fatalf("wallet=%q", got.Wallet)
	}
	if _, err := store.UpdateWallet("garbage-key", "tb1qgarbage"); err != nil {
		t.Fatalf("UpdateWallet login path: %v", err)
	}
}
