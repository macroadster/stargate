package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionString(t *testing.T) {
	origV, origC := version, gitCommit
	t.Cleanup(func() {
		version, gitCommit = origV, origC
	})

	version, gitCommit = "v1.2.3", "unknown"
	if got := versionString(); got != "stargate v1.2.3" {
		t.Fatalf("version only: got %q", got)
	}

	version, gitCommit = "v1.2.3", "abc1234"
	if got := versionString(); got != "stargate v1.2.3 (abc1234)" {
		t.Fatalf("version+commit: got %q", got)
	}

	version, gitCommit = "", "unknown"
	if got := versionString(); got != "stargate dev" {
		t.Fatalf("empty version: got %q", got)
	}
}

func TestRunCLINoArgsStartsServer(t *testing.T) {
	var out, err bytes.Buffer
	handled, code := runCLI(nil, &out, &err)
	if handled || code != 0 {
		t.Fatalf("nil args: handled=%v code=%d", handled, code)
	}
	handled, code = runCLI([]string{}, &out, &err)
	if handled || code != 0 {
		t.Fatalf("empty args: handled=%v code=%d", handled, code)
	}
}

func TestRunCLIVersionHelpAndUnknown(t *testing.T) {
	origV, origC := version, gitCommit
	version, gitCommit = "v9.9.9", "deadbeef"
	t.Cleanup(func() {
		version, gitCommit = origV, origC
	})

	cases := []struct {
		name       string
		args       []string
		wantOut    string
		wantErrSub string
		wantCode   int
		wantHandle bool
	}{
		{name: "double-dash-version", args: []string{"--version"}, wantOut: "stargate v9.9.9 (deadbeef)", wantCode: 0, wantHandle: true},
		{name: "version-cmd", args: []string{"version"}, wantOut: "stargate v9.9.9 (deadbeef)", wantCode: 0, wantHandle: true},
		{name: "short-v", args: []string{"-v"}, wantOut: "stargate v9.9.9 (deadbeef)", wantCode: 0, wantHandle: true},
		{name: "help-flag", args: []string{"--help"}, wantOut: "Usage: stargate", wantCode: 0, wantHandle: true},
		{name: "help-cmd", args: []string{"help"}, wantOut: "Usage: stargate", wantCode: 0, wantHandle: true},
		{name: "help-serve", args: []string{"help", "serve"}, wantOut: "stargate serve", wantCode: 0, wantHandle: true},
		{name: "short-h", args: []string{"-h"}, wantOut: "Usage: stargate", wantCode: 0, wantHandle: true},
		{name: "unknown-cmd", args: []string{"frobnicate"}, wantErrSub: "unknown command", wantCode: 2, wantHandle: true},
		{name: "help-unknown", args: []string{"help", "nope"}, wantErrSub: "unknown help topic", wantCode: 2, wantHandle: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			handled, code := runCLI(tc.args, &stdout, &stderr)
			if handled != tc.wantHandle {
				t.Fatalf("handled: got %v want %v\nstdout=%q\nstderr=%q", handled, tc.wantHandle, stdout.String(), stderr.String())
			}
			if code != tc.wantCode {
				t.Fatalf("exit code: got %d want %d\nstdout=%q\nstderr=%q", code, tc.wantCode, stdout.String(), stderr.String())
			}
			if tc.wantOut != "" && !strings.Contains(stdout.String(), tc.wantOut) {
				t.Fatalf("stdout missing %q: %q", tc.wantOut, stdout.String())
			}
			if tc.wantErrSub != "" && !strings.Contains(stderr.String(), tc.wantErrSub) {
				t.Fatalf("stderr missing %q: %q", tc.wantErrSub, stderr.String())
			}
		})
	}
}

func TestRunCLIServeFlags(t *testing.T) {
	// Isolate env mutations.
	keys := []string{
		"STARGATE_HTTP_PORT", "STARGATE_DATA_DIR", "STARGATE_STORAGE",
		"STARGATE_PG_DSN", "UPLOADS_DIR", "BLOCKS_DIR", "STARGATE_AGENT_ENABLED",
	}
	orig := map[string]string{}
	for _, k := range keys {
		orig[k] = os.Getenv(k)
		_ = os.Unsetenv(k)
	}
	t.Cleanup(func() {
		for k, v := range orig {
			if v == "" {
				_ = os.Unsetenv(k)
			} else {
				_ = os.Setenv(k, v)
			}
		}
	})

	t.Run("serve-subcommand", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		handled, code := runCLI([]string{"serve", "--port", "4123", "--data-dir", "/tmp/sg-test", "--storage", "memory", "--agent"}, &stdout, &stderr)
		if handled || code != 0 {
			t.Fatalf("handled=%v code=%d stderr=%q", handled, code, stderr.String())
		}
		if os.Getenv("STARGATE_HTTP_PORT") != "4123" {
			t.Fatalf("port: %q", os.Getenv("STARGATE_HTTP_PORT"))
		}
		if os.Getenv("STARGATE_DATA_DIR") != "/tmp/sg-test" {
			t.Fatalf("data-dir: %q", os.Getenv("STARGATE_DATA_DIR"))
		}
		if os.Getenv("STARGATE_STORAGE") != "memory" {
			t.Fatalf("storage: %q", os.Getenv("STARGATE_STORAGE"))
		}
		if os.Getenv("STARGATE_AGENT_ENABLED") != "true" {
			t.Fatalf("agent: %q", os.Getenv("STARGATE_AGENT_ENABLED"))
		}
	})

	t.Run("bare-flags", func(t *testing.T) {
		_ = os.Unsetenv("STARGATE_HTTP_PORT")
		var stdout, stderr bytes.Buffer
		handled, code := runCLI([]string{"-p", "9999"}, &stdout, &stderr)
		if handled || code != 0 {
			t.Fatalf("handled=%v code=%d stderr=%q", handled, code, stderr.String())
		}
		if os.Getenv("STARGATE_HTTP_PORT") != "9999" {
			t.Fatalf("port: %q", os.Getenv("STARGATE_HTTP_PORT"))
		}
	})

	t.Run("invalid-storage", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		handled, code := runCLI([]string{"serve", "--storage", "redis"}, &stdout, &stderr)
		if !handled || code != 2 {
			t.Fatalf("handled=%v code=%d", handled, code)
		}
		if !strings.Contains(stderr.String(), "invalid --storage") {
			t.Fatalf("stderr: %q", stderr.String())
		}
	})

	t.Run("serve-help", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		handled, code := runCLI([]string{"serve", "--help"}, &stdout, &stderr)
		if !handled || code != 0 {
			t.Fatalf("handled=%v code=%d stderr=%q", handled, code, stderr.String())
		}
		if !strings.Contains(stdout.String(), "stargate serve") {
			t.Fatalf("stdout: %q", stdout.String())
		}
	})

	t.Run("start-alias", func(t *testing.T) {
		_ = os.Unsetenv("STARGATE_HTTP_PORT")
		var stdout, stderr bytes.Buffer
		handled, code := runCLI([]string{"start", "-p", "3005"}, &stdout, &stderr)
		if handled || code != 0 {
			t.Fatalf("handled=%v code=%d stderr=%q", handled, code, stderr.String())
		}
		if os.Getenv("STARGATE_HTTP_PORT") != "3005" {
			t.Fatalf("port: %q", os.Getenv("STARGATE_HTTP_PORT"))
		}
	})
}

func TestRunCLIEnv(t *testing.T) {
	t.Setenv("STARGATE_HTTP_PORT", "7777")
	t.Setenv("STARGATE_DATA_DIR", "/var/lib/stargate")
	t.Setenv("STARGATE_API_KEY", "supersecrettoken")

	var stdout, stderr bytes.Buffer
	handled, code := runCLI([]string{"env"}, &stdout, &stderr)
	if !handled || code != 0 {
		t.Fatalf("handled=%v code=%d stderr=%q", handled, code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "7777") {
		t.Fatalf("missing port: %q", out)
	}
	if !strings.Contains(out, "/var/lib/stargate") {
		t.Fatalf("missing data dir: %q", out)
	}
	if strings.Contains(out, "supersecrettoken") {
		t.Fatalf("API key not redacted: %q", out)
	}
	if !strings.Contains(out, "redacted") {
		t.Fatalf("expected redacted marker: %q", out)
	}

	stdout.Reset()
	stderr.Reset()
	handled, code = runCLI([]string{"env", "--json"}, &stdout, &stderr)
	if !handled || code != 0 {
		t.Fatalf("json handled=%v code=%d stderr=%q", handled, code, stderr.String())
	}
	var m map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &m); err != nil {
		t.Fatalf("json: %v\n%s", err, stdout.String())
	}
	if m["STARGATE_HTTP_PORT"] != "7777" {
		t.Fatalf("json port: %#v", m["STARGATE_HTTP_PORT"])
	}
}

func TestRunCLIDoctor(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("STARGATE_DATA_DIR", tmp)
	t.Setenv("UPLOADS_DIR", filepath.Join(tmp, "uploads"))
	t.Setenv("BLOCKS_DIR", filepath.Join(tmp, "blocks"))
	// Create one dir to exercise writable path.
	if err := os.MkdirAll(filepath.Join(tmp, "uploads"), 0o755); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	handled, code := runCLI([]string{"doctor"}, &stdout, &stderr)
	if !handled || code != 0 {
		t.Fatalf("handled=%v code=%d\nstdout=%s\nstderr=%s", handled, code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "All checks passed") {
		t.Fatalf("stdout: %q", stdout.String())
	}
}

func TestRunCLIHealth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(srv.Close)

	var stdout, stderr bytes.Buffer
	handled, code := runCLI([]string{"health", "--url", srv.URL}, &stdout, &stderr)
	if !handled || code != 0 {
		t.Fatalf("handled=%v code=%d stderr=%q stdout=%q", handled, code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), `"status":"ok"`) {
		t.Fatalf("stdout: %q", stdout.String())
	}

	// Unreachable host.
	stdout.Reset()
	stderr.Reset()
	handled, code = runCLI([]string{"health", "--url", "http://127.0.0.1:1"}, &stdout, &stderr)
	if !handled || code != 1 {
		t.Fatalf("unreachable: handled=%v code=%d", handled, code)
	}
}

func TestRunCLICompletion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	handled, code := runCLI([]string{"completion", "bash"}, &stdout, &stderr)
	if !handled || code != 0 {
		t.Fatalf("handled=%v code=%d stderr=%q", handled, code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "complete -F _stargate stargate") {
		t.Fatalf("bash script missing: %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	handled, code = runCLI([]string{"completion", "zsh"}, &stdout, &stderr)
	if !handled || code != 0 || !strings.Contains(stdout.String(), "#compdef stargate") {
		t.Fatalf("zsh: handled=%v code=%d out=%q err=%q", handled, code, stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	handled, code = runCLI([]string{"completion", "fish"}, &stdout, &stderr)
	if !handled || code != 0 || !strings.Contains(stdout.String(), "complete -c stargate") {
		t.Fatalf("fish: handled=%v code=%d out=%q err=%q", handled, code, stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	handled, code = runCLI([]string{"completion", "powershell"}, &stdout, &stderr)
	if !handled || code != 2 {
		t.Fatalf("bad shell: handled=%v code=%d", handled, code)
	}
}

func TestRedact(t *testing.T) {
	if redact("") != "" {
		t.Fatal("empty")
	}
	if redact("short") != "********" {
		t.Fatalf("short: %q", redact("short"))
	}
	if !strings.Contains(redact("supersecrettoken"), "redacted") {
		t.Fatalf("long: %q", redact("supersecrettoken"))
	}
}

func TestEffectiveConfigPaths(t *testing.T) {
	t.Setenv("STARGATE_DATA_DIR", "/data")
	_ = os.Unsetenv("UPLOADS_DIR")
	cfg := effectiveConfig()
	if cfg["UPLOADS_DIR"] != filepath.Join("/data", "uploads") {
		t.Fatalf("uploads: %#v", cfg["UPLOADS_DIR"])
	}
}
