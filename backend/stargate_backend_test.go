package main

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
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

func TestDiagnosticsEnabled(t *testing.T) {
	t.Setenv("STARGATE_METRICS", "")
	if diagnosticsEnabled("STARGATE_METRICS") {
		t.Fatal("empty must be off")
	}
	for _, v := range []string{"0", "false", "no", "off", "maybe", "enabled"} {
		t.Setenv("STARGATE_METRICS", v)
		if diagnosticsEnabled("STARGATE_METRICS") {
			t.Fatalf("%q must be off", v)
		}
	}
	for _, v := range []string{"1", "true", "TRUE", "TRUE ", " yes ", "On"} {
		t.Setenv("STARGATE_METRICS", v)
		if !diagnosticsEnabled("STARGATE_METRICS") {
			t.Fatalf("%q must be on", v)
		}
	}
}

func TestRegisterDiagnosticRoutesOffByDefault(t *testing.T) {
	t.Setenv("STARGATE_METRICS", "")
	t.Setenv("STARGATE_PPROF", "")

	mux := http.NewServeMux()
	registerDiagnosticRoutes(mux)

	for _, path := range []string{"/metrics", "/debug/pprof/", "/debug/pprof/heap"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.RemoteAddr = "127.0.0.1:1"
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s registered while unset: %d", path, w.Code)
		}
	}
}

func TestRegisterDiagnosticRoutesOptInIsLoopbackOnly(t *testing.T) {
	t.Setenv("STARGATE_METRICS", "1")
	t.Setenv("STARGATE_PPROF", "true")

	mux := http.NewServeMux()
	registerDiagnosticRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.RemoteAddr = "127.0.0.1:1"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code == http.StatusNotFound || w.Code == http.StatusForbidden {
		t.Fatalf("loopback metrics: %d", w.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	req.RemoteAddr = "[::1]:1"
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code == http.StatusNotFound || w.Code == http.StatusForbidden {
		t.Fatalf("loopback pprof: %d", w.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.RemoteAddr = "8.8.8.8:443"
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("public metrics: %d", w.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	req.RemoteAddr = "8.8.8.8:443"
	req.Header.Set("X-Forwarded-For", "127.0.0.1")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("spoofed pprof: %d", w.Code)
	}
}
