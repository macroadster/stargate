package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func cspFor(t *testing.T, path string) string {
	t.Helper()
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	SecurityHeaders(inner).ServeHTTP(w, req)
	return w.Header().Get("Content-Security-Policy")
}

func TestSecurityHeadersDesktopAllowsExternalFrameOrigins(t *testing.T) {
	csp := cspFor(t, "/desktop/shell.html")
	if !strings.Contains(csp, "frame-src 'self' blob: http: https:;") {
		t.Fatalf("/desktop CSP frame-src must allow http(s) origins, got: %s", csp)
	}
}

func TestSecurityHeadersOtherPathsKeepTightFrameSrc(t *testing.T) {
	for _, path := range []string{"/", "/api/health", "/contract/abc123"} {
		csp := cspFor(t, path)
		if !strings.Contains(csp, "frame-src 'self' blob:;") {
			t.Fatalf("%s CSP frame-src = want tight self blob, got: %s", path, csp)
		}
		if strings.Contains(csp, "frame-src 'self' blob: http: https:;") {
			t.Fatalf("%s CSP must not allow arbitrary frame origins: %s", path, csp)
		}
	}
}

func TestSecurityHeadersSandboxPathLockedDown(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	req := httptest.NewRequest(http.MethodGet, "/sandbox/abc/", nil)
	w := httptest.NewRecorder()
	SecurityHeaders(inner).ServeHTTP(w, req)
	if got := w.Header().Get("X-Frame-Options"); got != "SAMEORIGIN" {
		t.Fatalf("sandbox X-Frame-Options = %q, want SAMEORIGIN", got)
	}
	csp := w.Header().Get("Content-Security-Policy")
	for _, want := range []string{
		"default-src 'self'",
		"connect-src 'self' blob: data:;",
		"media-src 'self' blob: data:;",
		"img-src 'self' data: blob:;",
		"worker-src 'self' blob:;",
		"object-src 'none'",
		"base-uri 'none'",
		"form-action 'self'",
		"frame-ancestors 'self'",
	} {
		if !strings.Contains(csp, want) {
			t.Fatalf("sandbox CSP missing %q, got: %s", want, csp)
		}
	}
	// Remote egress must stay closed: no wildcard/http(s) source in connect-src.
	if strings.Contains(csp, "connect-src *") || strings.Contains(csp, "connect-src https:") || strings.Contains(csp, "http: https:") {
		t.Fatalf("sandbox CSP must not allow remote connect origins, got: %s", csp)
	}
}

func TestSecurityHeadersSandboxDefaultHasWasmWasmUnsafeEval(t *testing.T) {
	if !strings.Contains(sandboxCSPDefault, "'wasm-unsafe-eval'") {
		t.Fatalf("default sandbox CSP must permit WASM compilation for sql.js contracts: %s", sandboxCSPDefault)
	}
}

func TestSecurityHeadersSandboxChatOverride(t *testing.T) {
	csp := cspFor(t, "/sandbox/503751efad310be50b8c2e63563dc1a2c1ddef4f52c9d3b91a6394c75b3985f3/")
	for _, want := range []string{
		"https://cdnjs.cloudflare.com", // sql.js JS + WASM
		"https://starlight-ai.freemyip.com", // chat relay (EventSource/fetch)
	} {
		if !strings.Contains(csp, want) {
			t.Fatalf("chat override CSP must allow %q, got: %s", want, csp)
		}
	}
}

func TestSecurityHeadersSandboxLoopsOverride(t *testing.T) {
	csp := cspFor(t, "/sandbox/2d89e6ebfd09604938416481cb69cee2a63ceadab93541b3fbf4a26293274bf7/")
	for _, want := range []string{
		"https://unpkg.com",
		"media-src 'self' blob: data: https:",
		"connect-src 'self' blob: data: https:",
	} {
		if !strings.Contains(csp, want) {
			t.Fatalf("loops override CSP must allow %q, got: %s", want, csp)
		}
	}
}

func TestSecurityHeadersSandboxOtherHashKeepsStrictDefault(t *testing.T) {
	csp := cspFor(t, "/sandbox/deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef/")
	if strings.Contains(csp, "cdnjs.cloudflare.com") || strings.Contains(csp, "unpkg.com") {
		t.Fatalf("unknown hash must not get override origins, got: %s", csp)
	}
	if !strings.Contains(csp, "connect-src 'self' blob: data:;") {
		t.Fatalf("unknown hash must keep strict connect-src, got: %s", csp)
	}
}

func TestSandboxPathHashExtractsFirstSegment(t *testing.T) {
	for in, want := range map[string]string{
		"/sandbox/abc/":              "abc",
		"/sandbox/abc/vendor/x.js":   "abc",
		"/sandbox/abc":               "abc",
		"/sandbox/":                  "",
	} {
		if got := sandboxPathHash(in); got != want {
			t.Fatalf("sandboxPathHash(%q) = %q, want %q", in, got, want)
		}
	}
}
