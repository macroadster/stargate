package auth

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"stargate-backend/storage/gormdb"
)

// APIKeyRow is the GORM model for the api_keys table (SQLite + Postgres).
// Plaintext keys are never stored — only SHA-256 hex digests (key_hash).
// CreatedAt uses gormdb.SQLTime so legacy SQLite TEXT timestamps scan correctly
// (production DBs predate GORM AutoMigrate DATETIME columns).
type APIKeyRow struct {
	KeyHash       string         `gorm:"column:key_hash;primaryKey;size:64"`
	Email         string         `gorm:"column:email"`
	WalletAddress string         `gorm:"column:wallet_address;index:idx_api_keys_wallet"`
	Source        string         `gorm:"column:source"`
	CreatedAt     gormdb.SQLTime `gorm:"column:created_at"`
}

// TableName pins the historical table name.
func (APIKeyRow) TableName() string { return "api_keys" }

// GORMAPIKeyStore is the single durable API key implementation for SQLite and Postgres.
// Memory mode continues to use APIKeyStore (map); optional memory SQL can use OpenMemory.
type GORMAPIKeyStore struct {
	db *gorm.DB
}

// NewGORMAPIKeyStore migrates the api_keys schema and returns a store.
func NewGORMAPIKeyStore(db *gorm.DB) (*GORMAPIKeyStore, error) {
	if db == nil {
		return nil, fmt.Errorf("auth: nil gorm DB")
	}
	if err := db.AutoMigrate(&APIKeyRow{}); err != nil {
		return nil, fmt.Errorf("auth: migrate api_keys: %w", err)
	}
	return &GORMAPIKeyStore{db: db}, nil
}

// NewSQLiteAPIKeyStore opens pure-Go SQLite and returns a GORM-backed store.
// Kept for factory / call-site compatibility.
func NewSQLiteAPIKeyStore(dbPath string) (*GORMAPIKeyStore, error) {
	db, err := gormdb.OpenSQLite(dbPath)
	if err != nil {
		return nil, err
	}
	store, err := NewGORMAPIKeyStore(db)
	if err != nil {
		_ = gormdb.Close(db)
		return nil, err
	}
	return store, nil
}

// NewPGAPIKeyStore opens Postgres and returns a GORM-backed store.
// ctx is accepted for API compatibility; open uses the driver's own context.
func NewPGAPIKeyStore(ctx context.Context, dsn string) (*GORMAPIKeyStore, error) {
	_ = ctx
	db, err := gormdb.OpenPostgres(dsn)
	if err != nil {
		return nil, err
	}
	store, err := NewGORMAPIKeyStore(db)
	if err != nil {
		_ = gormdb.Close(db)
		return nil, err
	}
	return store, nil
}

// NewMemoryGORMAPIKeyStore is an optional SQL-backed memory store for parity tests.
func NewMemoryGORMAPIKeyStore() (*GORMAPIKeyStore, error) {
	db, err := gormdb.OpenMemory()
	if err != nil {
		return nil, err
	}
	return NewGORMAPIKeyStore(db)
}

// Close releases the underlying connection pool.
func (s *GORMAPIKeyStore) Close() error {
	return gormdb.Close(s.db)
}

// DB exposes the underlying GORM handle (tests / advanced use).
func (s *GORMAPIKeyStore) DB() *gorm.DB { return s.db }

// Validate implements APIKeyValidator.
func (s *GORMAPIKeyStore) Validate(key string) bool {
	if key == "" {
		return false
	}
	var n int64
	err := s.db.Model(&APIKeyRow{}).
		Where("key_hash = ?", hashAPIKey(key)).
		Count(&n).Error
	return err == nil && n > 0
}

// Get returns the API key metadata for the provided plaintext key.
func (s *GORMAPIKeyStore) Get(key string) (APIKey, bool) {
	if key == "" {
		return APIKey{}, false
	}
	var row APIKeyRow
	err := s.db.Where("key_hash = ?", hashAPIKey(key)).First(&row).Error
	if err != nil {
		return APIKey{}, false
	}
	return APIKey{
		Email:     row.Email,
		Wallet:    row.WalletAddress,
		Source:    row.Source,
		CreatedAt: row.CreatedAt.ToTime(),
	}, true
}

// Issue implements APIKeyIssuer.
func (s *GORMAPIKeyStore) Issue(email, wallet, source string) (APIKey, error) {
	key, err := generateKey()
	if err != nil {
		return APIKey{}, err
	}
	rec := APIKey{
		Key:       key,
		Email:     email,
		Wallet:    wallet,
		Source:    source,
		CreatedAt: time.Now().UTC(),
	}
	row := APIKeyRow{
		KeyHash:       hashAPIKey(key),
		Email:         rec.Email,
		WalletAddress: rec.Wallet,
		Source:        rec.Source,
		CreatedAt:     gormdb.NewSQLTime(rec.CreatedAt),
	}
	if err := s.db.Create(&row).Error; err != nil {
		return APIKey{}, err
	}
	return rec, nil
}

// UpdateWallet binds a wallet address to an existing API key.
// Setting the same wallet already stored is a no-op success: drivers (notably
// SQLite via GORM) report RowsAffected=0 when the value is unchanged, and the
// login path always re-sends the bound wallet after wallet-verify issuance.
func (s *GORMAPIKeyStore) UpdateWallet(key, wallet string) (APIKey, error) {
	normalizedKey := strings.TrimSpace(key)
	normalizedWallet := strings.TrimSpace(wallet)
	if normalizedKey == "" {
		return APIKey{}, fmt.Errorf("api key required")
	}
	if normalizedWallet == "" {
		return APIKey{}, fmt.Errorf("wallet_address required")
	}
	keyHash := hashAPIKey(normalizedKey)
	res := s.db.Model(&APIKeyRow{}).
		Where("key_hash = ?", keyHash).
		Update("wallet_address", normalizedWallet)
	if res.Error != nil {
		return APIKey{}, res.Error
	}
	var row APIKeyRow
	if err := s.db.Where("key_hash = ?", keyHash).First(&row).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return APIKey{}, fmt.Errorf("api key not found")
		}
		return APIKey{}, err
	}
	// RowsAffected can be 0 when the wallet was already set to the same value.
	// Only treat that as failure when the row is missing (handled above).
	return APIKey{
		Email:     row.Email,
		Wallet:    row.WalletAddress,
		Source:    row.Source,
		CreatedAt: row.CreatedAt.ToTime(),
	}, nil
}

// InvalidateByWallet removes all API keys associated with a wallet address.
func (s *GORMAPIKeyStore) InvalidateByWallet(wallet string) error {
	if strings.TrimSpace(wallet) == "" {
		return fmt.Errorf("wallet required")
	}
	normalizedWallet := strings.ToLower(strings.TrimSpace(wallet))
	// LOWER() works on both SQLite and Postgres.
	return s.db.Where("LOWER(wallet_address) = ?", normalizedWallet).
		Delete(&APIKeyRow{}).Error
}

// Seed inserts a provided key if not empty (idempotent).
func (s *GORMAPIKeyStore) Seed(key, email, source string) {
	if key == "" {
		return
	}
	row := APIKeyRow{
		KeyHash:   hashAPIKey(key),
		Email:     email,
		Source:    source,
		CreatedAt: gormdb.NewSQLTime(time.Now().UTC()),
	}
	_ = s.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error
}

// SeedEnvironmentVariables seeds STARGATE_API_KEY and STARLIGHT_DONATION_ADDRESS.
func (s *GORMAPIKeyStore) SeedEnvironmentVariables() {
	plan := PlanEnvSeed()
	if plan.BindKey != "" {
		row := APIKeyRow{
			KeyHash:       hashAPIKey(plan.BindKey),
			WalletAddress: plan.BindWallet,
			Source:        "seed",
			CreatedAt:     gormdb.NewSQLTime(time.Now().UTC()),
		}
		_ = s.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error
		return
	}
	if plan.SeedKeyOnly != "" {
		s.Seed(plan.SeedKeyOnly, "", "seed")
	}
	if plan.SeedDonationAsKey != "" {
		s.Seed(plan.SeedDonationAsKey, "donation@starlight", "donation_seed")
	}
}

// Compile-time interface checks.
var (
	_ APIKeyValidator     = (*GORMAPIKeyStore)(nil)
	_ APIKeyIssuer        = (*GORMAPIKeyStore)(nil)
	_ APIKeyWalletUpdater = (*GORMAPIKeyStore)(nil)
	_ APIKeyWalletReissuer = (*GORMAPIKeyStore)(nil)
)
