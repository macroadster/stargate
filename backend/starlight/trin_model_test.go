package starlight

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTempGGUF(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("fake-gguf-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestResolveTrinModelPath(t *testing.T) {
	// Isolate data dir so default path cannot leak from the machine.
	dataDir := t.TempDir()
	t.Setenv("STARGATE_DATA_DIR", dataDir)
	t.Setenv("STARLIGHT_GGUF", "")
	t.Setenv("STARLIGHT_TRIN_MODEL", "")

	if p := ResolveTrinModelPath(); p != "" {
		t.Errorf("empty envs + no default file: want empty got %q", p)
	}

	// Env set to missing path → skip, still empty
	t.Setenv("STARLIGHT_TRIN_MODEL", filepath.Join(dataDir, "missing-alias.gguf"))
	if p := ResolveTrinModelPath(); p != "" {
		t.Errorf("missing alias env: want empty got %q", p)
	}

	// Existing alias file
	alias := writeTempGGUF(t, dataDir, "alias.gguf")
	t.Setenv("STARLIGHT_TRIN_MODEL", alias)
	if p := ResolveTrinModelPath(); p != alias {
		t.Errorf("alias: got %q want %q", p, alias)
	}

	// STARLIGHT_GGUF wins when both exist
	preferred := writeTempGGUF(t, dataDir, "preferred.gguf")
	t.Setenv("STARLIGHT_GGUF", preferred)
	if p := ResolveTrinModelPath(); p != preferred {
		t.Errorf("preferred: got %q want %q", p, preferred)
	}

	// Missing preferred env skips to existing alias
	t.Setenv("STARLIGHT_GGUF", filepath.Join(dataDir, "gone.gguf"))
	if p := ResolveTrinModelPath(); p != alias {
		t.Errorf("missing preferred fallthrough to alias: got %q want %q", p, alias)
	}

	// Default path under STARGATE_DATA_DIR when envs cleared
	t.Setenv("STARLIGHT_GGUF", "")
	t.Setenv("STARLIGHT_TRIN_MODEL", "")
	def := DefaultTrinModelPath()
	if err := os.MkdirAll(filepath.Dir(def), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(def, []byte("default-gguf"), 0o644); err != nil {
		t.Fatal(err)
	}
	if p := ResolveTrinModelPath(); p != def {
		t.Errorf("default path: got %q want %q", p, def)
	}

	// Missing preferred + missing alias, but default exists → default
	t.Setenv("STARLIGHT_GGUF", filepath.Join(dataDir, "nope.gguf"))
	t.Setenv("STARLIGHT_TRIN_MODEL", filepath.Join(dataDir, "also-nope.gguf"))
	if p := ResolveTrinModelPath(); p != def {
		t.Errorf("missing envs fallthrough to default: got %q want %q", p, def)
	}
}

func TestDefaultTrinModelPath(t *testing.T) {
	t.Setenv("STARGATE_DATA_DIR", "/tmp/stargate-test-data")
	got := DefaultTrinModelPath()
	want := filepath.Join("/tmp/stargate-test-data", "models", "starlight.gguf")
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestHFDownloadURL(t *testing.T) {
	old := hfBaseURL
	hfBaseURL = "https://huggingface.co"
	t.Cleanup(func() { hfBaseURL = old })

	t.Setenv("STARLIGHT_HF_REPO", "")
	t.Setenv("STARLIGHT_HF_FILE", "")
	t.Setenv("STARLIGHT_HF_REVISION", "")
	got := hfDownloadURL()
	want := "https://huggingface.co/macroadster/starlight-prod/resolve/main/starlight.gguf"
	if got != want {
		t.Errorf("defaults: got %q want %q", got, want)
	}

	t.Setenv("STARLIGHT_HF_REPO", "org/repo")
	t.Setenv("STARLIGHT_HF_FILE", "model.gguf")
	t.Setenv("STARLIGHT_HF_REVISION", "v1")
	got = hfDownloadURL()
	want = "https://huggingface.co/org/repo/resolve/v1/model.gguf"
	if got != want {
		t.Errorf("custom: got %q want %q", got, want)
	}
}

func TestDownloadTrinModel_httptest(t *testing.T) {
	payload := []byte("gguf-content-from-test-server")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}))
	t.Cleanup(srv.Close)

	dest := filepath.Join(t.TempDir(), "models", "starlight.gguf")
	if err := downloadFile(srv.Client(), srv.URL+"/file.gguf", dest); err != nil {
		t.Fatalf("downloadFile: %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Errorf("content got %q want %q", got, payload)
	}
	if _, err := os.Stat(dest + ".partial"); !os.IsNotExist(err) {
		t.Errorf("partial file should not remain: %v", err)
	}
}

func TestDownloadTrinModel_httpError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	dest := filepath.Join(t.TempDir(), "models", "starlight.gguf")
	err := downloadFile(srv.Client(), srv.URL+"/missing.gguf", dest)
	if err == nil {
		t.Fatal("expected error for 404")
	}
	if !strings.Contains(err.Error(), "404") && !strings.Contains(err.Error(), "Not Found") {
		t.Errorf("error should mention status: %v", err)
	}
	if fileExistsRegular(dest) {
		t.Error("dest should not exist after failed download")
	}
	if _, err := os.Stat(dest + ".partial"); !os.IsNotExist(err) {
		t.Error("partial should not remain after failed download")
	}
}

func TestEnsureTrinModel_usesExisting(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("STARGATE_DATA_DIR", dataDir)
	t.Setenv("STARLIGHT_GGUF", "")
	t.Setenv("STARLIGHT_TRIN_MODEL", "")
	t.Setenv("STARLIGHT_GGUF_FORCE_DOWNLOAD", "")

	// If download were attempted this would fail — prove no download needed.
	old := hfBaseURL
	hfBaseURL = "http://127.0.0.1:1"
	t.Cleanup(func() { hfBaseURL = old })

	def := DefaultTrinModelPath()
	if err := os.MkdirAll(filepath.Dir(def), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(def, []byte("already-here"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := EnsureTrinModel()
	if got != def {
		t.Errorf("got %q want %q", got, def)
	}
}

func TestEnsureTrinModel_downloadsWhenMissing(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("STARGATE_DATA_DIR", dataDir)
	t.Setenv("STARLIGHT_GGUF", "")
	t.Setenv("STARLIGHT_TRIN_MODEL", "")
	t.Setenv("STARLIGHT_GGUF_FORCE_DOWNLOAD", "")
	t.Setenv("STARLIGHT_HF_REPO", "test/repo")
	t.Setenv("STARLIGHT_HF_FILE", "starlight.gguf")
	t.Setenv("STARLIGHT_HF_REVISION", "main")

	payload := []byte("downloaded-gguf-body")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Path: /test/repo/resolve/main/starlight.gguf
		if !strings.Contains(r.URL.Path, "starlight.gguf") {
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}))
	t.Cleanup(srv.Close)

	oldBase := hfBaseURL
	oldClient := httpClient
	hfBaseURL = srv.URL
	httpClient = srv.Client()
	t.Cleanup(func() {
		hfBaseURL = oldBase
		httpClient = oldClient
	})

	got := EnsureTrinModel()
	def := DefaultTrinModelPath()
	if got != def {
		t.Fatalf("got %q want %q", got, def)
	}
	body, err := os.ReadFile(def)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != string(payload) {
		t.Errorf("content got %q want %q", body, payload)
	}
}

func TestEnsureTrinModel_downloadFailReturnsEmpty(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("STARGATE_DATA_DIR", dataDir)
	t.Setenv("STARLIGHT_GGUF", "")
	t.Setenv("STARLIGHT_TRIN_MODEL", "")
	t.Setenv("STARLIGHT_GGUF_FORCE_DOWNLOAD", "")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	oldBase := hfBaseURL
	oldClient := httpClient
	hfBaseURL = srv.URL
	httpClient = srv.Client()
	t.Cleanup(func() {
		hfBaseURL = oldBase
		httpClient = oldClient
	})

	if got := EnsureTrinModel(); got != "" {
		t.Errorf("want empty on download fail, got %q", got)
	}
	if fileExistsRegular(DefaultTrinModelPath()) {
		t.Error("default path should not exist after failed download")
	}
}

func TestEnsureTrinModel_forceRedownload(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("STARGATE_DATA_DIR", dataDir)
	t.Setenv("STARLIGHT_GGUF", "")
	t.Setenv("STARLIGHT_TRIN_MODEL", "")
	t.Setenv("STARLIGHT_GGUF_FORCE_DOWNLOAD", "1")

	def := DefaultTrinModelPath()
	if err := os.MkdirAll(filepath.Dir(def), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(def, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	payload := []byte("fresh-download")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}))
	t.Cleanup(srv.Close)

	oldBase := hfBaseURL
	oldClient := httpClient
	hfBaseURL = srv.URL
	httpClient = srv.Client()
	t.Cleanup(func() {
		hfBaseURL = oldBase
		httpClient = oldClient
	})

	got := EnsureTrinModel()
	if got != def {
		t.Fatalf("got %q want %q", got, def)
	}
	body, err := os.ReadFile(def)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != string(payload) {
		t.Errorf("content got %q want %q", body, payload)
	}
}
