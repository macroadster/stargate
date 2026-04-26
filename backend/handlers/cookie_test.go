package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	auth "stargate-backend/storage/auth"
)

func TestHandleLoginSetsCookie(t *testing.T) {
	store := auth.NewAPIKeyStore()
	apiKey := "test-api-key"
	store.Seed(apiKey, "test@example.com", "test")

	handler := NewAPIKeyHandler(store, store, nil)

	body, _ := json.Marshal(map[string]string{
		"api_key": apiKey,
	})
	req := httptest.NewRequest("POST", "/api/auth/login", bytes.NewBuffer(body))
	w := httptest.NewRecorder()

	handler.HandleLogin(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	cookies := resp.Cookies()
	found := false
	for _, cookie := range cookies {
		if cookie.Name == "X-API-Key" {
			found = true
			if cookie.Value != apiKey {
				t.Errorf("expected cookie value %s, got %s", apiKey, cookie.Value)
			}
			if !cookie.HttpOnly {
				t.Error("expected cookie to be HttpOnly")
			}
			if cookie.SameSite != http.SameSiteStrictMode {
				t.Errorf("expected SameSite Strict, got %v", cookie.SameSite)
			}
		}
	}
	if !found {
		t.Error("X-API-Key cookie not found in response")
	}
}

func TestHandleLogoutClearsCookie(t *testing.T) {
	handler := NewAPIKeyHandler(nil, nil, nil)
	req := httptest.NewRequest("POST", "/api/auth/logout", nil)
	w := httptest.NewRecorder()

	handler.HandleLogout(w, req)

	resp := w.Result()
	cookies := resp.Cookies()
	found := false
	for _, cookie := range cookies {
		if cookie.Name == "X-API-Key" {
			found = true
			if cookie.MaxAge != -1 {
				t.Errorf("expected MaxAge -1, got %d", cookie.MaxAge)
			}
		}
	}
	if !found {
		t.Error("X-API-Key cookie not found in logout response")
	}
}
