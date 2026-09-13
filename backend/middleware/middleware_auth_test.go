package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"stargate-backend/storage/auth"
)

func TestAPIAuthBearerAndCookie(t *testing.T) {
	store := auth.NewAPIKeyStore()
	store.Seed("shared-key", "", "seed")

	ok := APIAuth(store)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth.RequestAPIKey(r) != "shared-key" {
			t.Errorf("context key = %q", auth.RequestAPIKey(r))
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/inscribe", nil)
	req.Header.Set("Authorization", "Bearer shared-key")
	w := httptest.NewRecorder()
	ok.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("bearer: %d %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/inscribe", nil)
	req.AddCookie(&http.Cookie{Name: "X-API-Key", Value: "shared-key"})
	w = httptest.NewRecorder()
	ok.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("cookie: %d %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/inscribe", nil)
	w = httptest.NewRecorder()
	ok.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("missing: %d", w.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/inscribe", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	w = httptest.NewRecorder()
	ok.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("invalid: %d", w.Code)
	}
}

func TestLoopbackOnly(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	h := LoopbackOnly(inner)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("loopback v4: %d", w.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.RemoteAddr = "[::1]:54321"
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("loopback v6: %d", w.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.RemoteAddr = "192.168.1.50:54321"
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("lan: %d", w.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	req.RemoteAddr = "8.8.8.8:443"
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("public: %d", w.Code)
	}

	// Headers are not a source of truth. A public peer claiming to be
	// loopback via X-Forwarded-For must still be refused.
	req = httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.RemoteAddr = "8.8.8.8:443"
	req.Header.Set("X-Forwarded-For", "127.0.0.1")
	req.Header.Set("X-Real-IP", "127.0.0.1")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("forwarded-for spoof: %d", w.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.RemoteAddr = "127.0.0.1"
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("loopback without port: %d", w.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.RemoteAddr = "not-an-ip"
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("garbage remote: %d", w.Code)
	}
}
