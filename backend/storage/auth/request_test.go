package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAPIKeyFromRequestPrecedence(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-API-Key", " header-key ")
	req.Header.Set("Authorization", "Bearer bearer-key")
	req.AddCookie(&http.Cookie{Name: "X-API-Key", Value: "cookie-key"})
	if got := APIKeyFromRequest(req); got != "header-key" {
		t.Fatalf("header should win, got %q", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer  bearer-key ")
	req.AddCookie(&http.Cookie{Name: "X-API-Key", Value: "cookie-key"})
	if got := APIKeyFromRequest(req); got != "bearer-key" {
		t.Fatalf("bearer should win over cookie, got %q", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "X-API-Key", Value: " cookie-key "})
	if got := APIKeyFromRequest(req); got != "cookie-key" {
		t.Fatalf("cookie fallback, got %q", got)
	}

	if APIKeyFromRequest(nil) != "" {
		t.Fatal("nil request")
	}
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Basic abc")
	if APIKeyFromRequest(req) != "" {
		t.Fatal("non-bearer auth must be ignored")
	}
}

func TestRequestAPIKeyPrefersContext(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-API-Key", "header")
	req = req.WithContext(WithAPIKey(req.Context(), "ctx-key"))
	if got := RequestAPIKey(req); got != "ctx-key" {
		t.Fatalf("context should win, got %q", got)
	}
}
