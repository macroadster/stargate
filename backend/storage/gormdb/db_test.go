package gormdb

import (
	"path/filepath"
	"testing"
	"time"
)

func TestOpenSQLiteAndMemory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.db")
	db, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer Close(db)
	if d := DialectOf(db); d != DialectSQLite {
		t.Fatalf("dialect: %s", d)
	}

	mem, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	defer Close(mem)
	if d := DialectOf(mem); d != DialectMemory {
		t.Fatalf("memory dialect: %s", d)
	}
}

// autoMigrateRow is a minimal model to exercise sqlite_master-aware AutoMigrate.
type autoMigrateRow struct {
	ID        string    `gorm:"column:id;primaryKey;size:64"`
	Name      string    `gorm:"column:name;index:idx_auto_migrate_name"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

func (autoMigrateRow) TableName() string { return "auto_migrate_probe" }

// TestSQLiteAutoMigrateIdempotent ensures AutoMigrate succeeds on reopen when
// the table already exists (regression for stargate-6wu).
func TestSQLiteAutoMigrateIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "migrate.db")

	db1, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("open1: %v", err)
	}
	if err := db1.AutoMigrate(&autoMigrateRow{}); err != nil {
		t.Fatalf("AutoMigrate first: %v", err)
	}
	if err := db1.Create(&autoMigrateRow{ID: "k1", Name: "n1", CreatedAt: time.Now().UTC()}).Error; err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := Close(db1); err != nil {
		t.Fatalf("close1: %v", err)
	}

	db2, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("open2: %v", err)
	}
	defer Close(db2)

	if err := db2.AutoMigrate(&autoMigrateRow{}); err != nil {
		t.Fatalf("AutoMigrate reopen (must not fail with table already exists): %v", err)
	}
	var n int64
	if err := db2.Model(&autoMigrateRow{}).Where("id = ?", "k1").Count(&n).Error; err != nil || n != 1 {
		t.Fatalf("row missing after reopen: n=%d err=%v", n, err)
	}
	if !db2.Migrator().HasTable(&autoMigrateRow{}) {
		t.Fatal("HasTable should be true after create")
	}
	if !db2.Migrator().HasIndex(&autoMigrateRow{}, "idx_auto_migrate_name") {
		t.Fatal("HasIndex should find idx_auto_migrate_name")
	}
}
