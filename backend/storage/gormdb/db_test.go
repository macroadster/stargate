package gormdb

import (
	"path/filepath"
	"testing"
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
