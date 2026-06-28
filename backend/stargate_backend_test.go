package main

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	scstore "stargate-backend/storage/smart_contract"
)

func TestInitializeMCPComponentsFallsBackToMemoryWhenSQLiteInitFails(t *testing.T) {
	tmpDir := t.TempDir()
	blockerPath := filepath.Join(tmpDir, "not-a-dir")
	if err := os.WriteFile(blockerPath, []byte("block sqlite path"), 0o644); err != nil {
		t.Fatalf("write blocker file: %v", err)
	}

	origDataDir := os.Getenv("STARGATE_DATA_DIR")
	origPGDSN := os.Getenv("STARGATE_PG_DSN")
	origSeed := os.Getenv("STARGATE_SEED_FIXTURES")
	t.Cleanup(func() {
		_ = os.Setenv("STARGATE_DATA_DIR", origDataDir)
		_ = os.Setenv("STARGATE_PG_DSN", origPGDSN)
		_ = os.Setenv("STARGATE_SEED_FIXTURES", origSeed)
	})

	if err := os.Setenv("STARGATE_DATA_DIR", blockerPath); err != nil {
		t.Fatalf("set data dir: %v", err)
	}
	if err := os.Unsetenv("STARGATE_PG_DSN"); err != nil {
		t.Fatalf("unset pg dsn: %v", err)
	}
	if err := os.Setenv("STARGATE_SEED_FIXTURES", "false"); err != nil {
		t.Fatalf("set seed fixtures: %v", err)
	}

	var logBuf bytes.Buffer
	origWriter := log.Writer()
	log.SetOutput(&logBuf)
	t.Cleanup(func() {
		log.SetOutput(origWriter)
	})

	store, _, _, _, _ := initializeMCPComponents()

	if _, ok := store.(*scstore.MemoryStore); !ok {
		t.Fatalf("expected memory store fallback, got %T", store)
	}

	logOutput := logBuf.String()
	if !strings.Contains(logOutput, "falling back to memory store") {
		t.Fatalf("expected fallback log message, got %q", logOutput)
	}
	if !strings.Contains(logOutput, "Components initialized with memory store") {
		t.Fatalf("expected actual memory store log, got %q", logOutput)
	}
}
