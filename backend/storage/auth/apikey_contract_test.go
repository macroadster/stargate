package auth

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// apiKeySurface is the shared contract for memory, sqlite, and postgres.
type apiKeySurface interface {
	APIKeyIssuer
	APIKeyValidator
	APIKeyWalletUpdater
	APIKeyWalletReissuer
	Seed(key, email, source string)
	SeedEnvironmentVariables()
}

func TestAPIKeyStoreContract(t *testing.T) {
	t.Run("memory", func(t *testing.T) {
		exerciseAPIKeySurface(t, NewAPIKeyStore())
	})
	t.Run("sqlite", func(t *testing.T) {
		store, err := NewSQLiteAPIKeyStore(filepath.Join(t.TempDir(), "api_keys.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.Close() })
		exerciseAPIKeySurface(t, store)
		assertOnlyAPIKeysTable(t, store)
	})
	t.Run("gorm-memory", func(t *testing.T) {
		store, err := NewMemoryGORMAPIKeyStore()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.Close() })
		exerciseAPIKeySurface(t, store)
	})
	if dsn := os.Getenv("STARGATE_TEST_PG_DSN"); dsn != "" {
		t.Run("postgres", func(t *testing.T) {
			store, err := NewPGAPIKeyStore(context.Background(), dsn)
			if err != nil {
				t.Fatalf("NewPGAPIKeyStore: %v", err)
			}
			t.Cleanup(func() { _ = store.Close() })
			exerciseAPIKeySurface(t, store)
		})
	}
}

func exerciseAPIKeySurface(t *testing.T, s apiKeySurface) {
	t.Helper()

	rec, err := s.Issue("a@b.c", "tb1qissued", "registration")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if rec.Key == "" {
		t.Fatal("empty issued key")
	}
	if !s.Validate(rec.Key) || !s.Validate(" "+rec.Key+" ") {
		t.Fatal("Validate should accept issued key (trimmed)")
	}
	got, ok := s.Get(rec.Key)
	if !ok || got.Wallet != "tb1qissued" || got.Email != "a@b.c" {
		t.Fatalf("Get issued: %+v ok=%v", got, ok)
	}

	same, err := s.UpdateWallet(rec.Key, "tb1qissued")
	if err != nil {
		t.Fatalf("UpdateWallet same: %v", err)
	}
	if same.Wallet != "tb1qissued" {
		t.Fatalf("same wallet: %s", same.Wallet)
	}

	s.Seed("seed-contract-key", "seed@x", "seed")
	if !s.Validate("seed-contract-key") {
		t.Fatal("seeded key must validate")
	}

	t.Setenv("STARGATE_API_KEY", "env-contract-key")
	t.Setenv("STARLIGHT_DONATION_ADDRESS", "tb1qdonation")
	s.SeedEnvironmentVariables()
	if !s.Validate("env-contract-key") {
		t.Fatal("STARGATE_API_KEY seed must validate")
	}
	env, ok := s.Get("env-contract-key")
	if !ok || env.Wallet != "tb1qdonation" {
		t.Fatalf("seed bind wallet: %+v ok=%v", env, ok)
	}

	if err := s.InvalidateByWallet("tb1qissued"); err != nil {
		t.Fatalf("InvalidateByWallet: %v", err)
	}
	if s.Validate(rec.Key) {
		t.Fatal("invalidated key still validates")
	}
	if _, err := s.UpdateWallet("missing-key", "tb1qx"); err == nil {
		t.Fatal("UpdateWallet missing key should error")
	}
}

func assertOnlyAPIKeysTable(t *testing.T, store *GORMAPIKeyStore) {
	t.Helper()
	var names []string
	if err := store.db.Raw(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`).Scan(&names).Error; err != nil {
		t.Fatalf("list tables: %v", err)
	}
	for _, n := range names {
		switch n {
		case "api_keys":
		default:
			t.Fatalf("unexpected auth table %q (must not add a second key table)", n)
		}
	}
}
