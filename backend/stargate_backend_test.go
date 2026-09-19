package main

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	scstore "stargate-backend/storage/smart_contract"
)

func TestWaitForContextStopsDelayOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	started := time.Now()
	if waitForContext(ctx, time.Hour) {
		t.Fatal("cancelled context reported completed delay")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("cancelled wait took %s", elapsed)
	}
}

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

	allStores := initializeMCPComponents()

	if _, ok := allStores.SmartContractStore.(*scstore.MemoryStore); !ok {
		t.Fatalf("expected memory store fallback, got %T", allStores.SmartContractStore)
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

// irl.5: a cleaned path that only shares a string prefix with the base is not
// inside it. The old HasPrefix check treated /tmp/uploads-sibling as inside
// /tmp/uploads, which is one config change away from mattering.
func TestConfinedResolvedPathRejectsSiblingPrefix(t *testing.T) {
	base := filepath.Join(t.TempDir(), "uploads")
	sibling := base + "-sibling"
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sibling, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := confinedResolvedPath(base, filepath.Join(sibling, "secret")); err == nil {
		t.Fatal("expected a sibling that only shares the string prefix to be refused")
	}
}

func TestConfinedResolvedPathAllowsInsideAndBase(t *testing.T) {
	base := filepath.Join(t.TempDir(), "uploads")
	inside := filepath.Join(base, "ab", "cd", "ef", "file")

	got, err := confinedResolvedPath(base, inside)
	if err != nil {
		t.Fatalf("inside path refused: %v", err)
	}
	if got != filepath.Clean(inside) {
		t.Fatalf("got %q, want %q", got, filepath.Clean(inside))
	}

	got, err = confinedResolvedPath(base, base)
	if err != nil {
		t.Fatalf("base itself refused: %v", err)
	}
	if got != filepath.Clean(base) {
		t.Fatalf("base: got %q, want %q", got, filepath.Clean(base))
	}
}

func TestConfinedResolvedPathRejectsTraversal(t *testing.T) {
	base := filepath.Join(t.TempDir(), "uploads")
	escape := filepath.Join(base, "..", "outside")
	if _, err := confinedResolvedPath(base, escape); err == nil {
		t.Fatal("expected a path that walks out of the base to be refused")
	}
}

func TestCustomUploadsHandlerRejectsEncodedTraversal(t *testing.T) {
	root := t.TempDir()
	uploads := filepath.Join(root, "uploads")
	sibling := filepath.Join(root, "uploads-sibling")
	if err := os.MkdirAll(uploads, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sibling, 0o755); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(sibling, "secret.txt")
	if err := os.WriteFile(secret, []byte("leaked"), 0o644); err != nil {
		t.Fatal(err)
	}

	handler := customUploadsHandler(uploads)

	// ServeMux 301s a literal '..' segment. Percent-encoded forms survive mux
	// cleaning and used to arrive at the handler with '../' intact. irl.5
	// names both encodings; they decode to the same walk but must each stay
	// pinned so a later rewrite cannot drop one.
	cases := []struct {
		name string
		url  string
	}{
		{"dotdot-slash", "/uploads/..%2fuploads-sibling%2fsecret.txt"},
		{"encoded-dots", "/uploads/%2e%2e/uploads-sibling/secret.txt"},
		{"encoded-dots-and-slash", "/uploads/%2e%2e%2fuploads-sibling%2fsecret.txt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.url, nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code == http.StatusOK && strings.Contains(w.Body.String(), "leaked") {
				t.Fatalf("encoded traversal served sibling content: %d %q", w.Code, w.Body.String())
			}
			if w.Code != http.StatusForbidden && w.Code != http.StatusNotFound {
				t.Fatalf("encoded traversal: got %d %q, want 403 or 404", w.Code, w.Body.String())
			}
		})
	}
}

func TestCustomUploadsHandlerServesInside(t *testing.T) {
	uploads := t.TempDir()
	name := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	path := filepath.Join(uploads, name)
	if err := os.WriteFile(path, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}

	handler := customUploadsHandler(uploads)
	req := httptest.NewRequest(http.MethodGet, "/uploads/"+name, nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("inside file: %d %q", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "payload") {
		t.Fatalf("inside file body = %q", w.Body.String())
	}
}

func TestSandboxHandlerEmptyExplainsConfirm(t *testing.T) {
	uploads := t.TempDir()
	results := filepath.Join(uploads, "results")
	if err := os.MkdirAll(results, 0o755); err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("ab", 32)
	handler := sandboxHandler(uploads, results)
	req := httptest.NewRequest(http.MethodGet, "/sandbox/"+hash+"/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "not unpacked yet") {
		t.Fatalf("empty sandbox body = %q", body)
	}
	if !strings.Contains(body, hash) {
		t.Fatalf("missing hash in empty sandbox page: %q", body)
	}
}

func TestSandboxHandlerServesUnpackedFile(t *testing.T) {
	uploads := t.TempDir()
	results := filepath.Join(uploads, "results")
	hash := strings.Repeat("cd", 32)
	dir := filepath.Join(results, hash[0:2], hash[2:4], hash[4:6], hash)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>ok</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	handler := sandboxHandler(uploads, results)
	req := httptest.NewRequest(http.MethodGet, "/sandbox/"+hash+"/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q Location=%s, want 200", w.Code, w.Body.String(), w.Header().Get("Location"))
	}
	if !strings.Contains(w.Body.String(), "<html>ok</html>") {
		t.Fatalf("body=%q", w.Body.String())
	}
}
