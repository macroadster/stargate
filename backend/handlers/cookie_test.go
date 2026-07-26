package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	auth "stargate-backend/storage/auth"
	"testing"
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

// Login after wallet-verify always re-sends the already-bound wallet. Must not 500.
func TestHandleLoginWithAlreadyBoundWallet(t *testing.T) {
	store := auth.NewAPIKeyStore()
	rec, err := store.Issue("", "tb1qtestwallet", "wallet-verify")
	if err != nil {
		t.Fatal(err)
	}

	handler := NewAPIKeyHandler(store, store, nil)
	body, _ := json.Marshal(map[string]string{
		"api_key":         rec.Key,
		"wallet_address":  "tb1qtestwallet",
	})
	req := httptest.NewRequest("POST", "/api/auth/login", bytes.NewBuffer(body))
	w := httptest.NewRecorder()

	handler.HandleLogin(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 for login with already-bound wallet, got %d body=%s",
			resp.StatusCode, w.Body.String())
	}
}

func TestHandleLoginRejectsWalletRebind(t *testing.T) {
	store := auth.NewAPIKeyStore()
	rec, err := store.Issue("", "tb1qoriginal", "wallet-verify")
	if err != nil {
		t.Fatal(err)
	}

	handler := NewAPIKeyHandler(store, store, nil)
	body, _ := json.Marshal(map[string]string{
		"api_key":        rec.Key,
		"wallet_address": "tb1qother",
	})
	req := httptest.NewRequest("POST", "/api/auth/login", bytes.NewBuffer(body))
	w := httptest.NewRecorder()

	handler.HandleLogin(w, req)

	if w.Result().StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for rebind attempt, got %d body=%s", w.Code, w.Body.String())
	}
}

// Production path uses GORM SQLite. Login with already-bound wallet must not 500.
func TestHandleLoginGORMSameWallet(t *testing.T) {
	store, err := auth.NewMemoryGORMAPIKeyStore()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	rec, err := store.Issue("", "tb1qgormwallet", "wallet-verify")
	if err != nil {
		t.Fatal(err)
	}

	handler := NewAPIKeyHandler(store, store, nil)
	body, _ := json.Marshal(map[string]string{
		"api_key":        rec.Key,
		"wallet_address": "tb1qgormwallet",
	})
	req := httptest.NewRequest("POST", "/api/auth/login", bytes.NewBuffer(body))
	w := httptest.NewRecorder()

	handler.HandleLogin(w, req)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on GORM login with bound wallet, got %d body=%s",
			w.Code, w.Body.String())
	}
}
