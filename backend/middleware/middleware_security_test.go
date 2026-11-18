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
