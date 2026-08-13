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
