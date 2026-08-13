package handlers

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	auth "stargate-backend/storage/auth"

	_ "modernc.org/sqlite"
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
		"api_key":        rec.Key,
		"wallet_address": "tb1qtestwallet",
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

func TestHandleRegisterIssuesSharedStoreKey(t *testing.T) {
	store := auth.NewAPIKeyStore()
	handler := NewAPIKeyHandler(store, store, nil)
	body, _ := json.Marshal(map[string]string{
		"email":          "reg@example.com",
		"wallet_address": "tb1qregister",
	})
	req := httptest.NewRequest("POST", "/api/auth/register", bytes.NewBuffer(body))
	w := httptest.NewRecorder()
	handler.HandleRegister(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("register must reject unsigned wallet bind: %d %s", w.Code, w.Body.String())
	}

	body, _ = json.Marshal(map[string]string{"email": "reg@example.com"})
	req = httptest.NewRequest("POST", "/api/auth/register", bytes.NewBuffer(body))
	w = httptest.NewRecorder()
	handler.HandleRegister(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("register: %d %s", w.Code, w.Body.String())
	}
	var wrap struct {
		Data struct {
			APIKey string `json:"api_key"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &wrap); err != nil {
		t.Fatal(err)
	}
	if wrap.Data.APIKey == "" || !store.Validate(wrap.Data.APIKey) {
		t.Fatalf("register must issue into the same store: %+v", wrap)
	}

	loginBody, _ := json.Marshal(map[string]string{"api_key": wrap.Data.APIKey})
	lw := httptest.NewRecorder()
	handler.HandleLogin(lw, httptest.NewRequest("POST", "/api/auth/login", bytes.NewBuffer(loginBody)))
	if lw.Code != http.StatusOK {
		t.Fatalf("login after register: %d %s", lw.Code, lw.Body.String())
	}
}

func TestHandleLoginEmptyAndMissingStore(t *testing.T) {
	handler := NewAPIKeyHandler(nil, nil, nil)
	w := httptest.NewRecorder()
	handler.HandleLogin(w, httptest.NewRequest("POST", "/api/auth/login", bytes.NewBufferString(`{"api_key":"x"}`)))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil validator: %d", w.Code)
	}

	store := auth.NewAPIKeyStore()
	handler = NewAPIKeyHandler(store, store, nil)
	w = httptest.NewRecorder()
	handler.HandleLogin(w, httptest.NewRequest("POST", "/api/auth/login", bytes.NewBufferString(`{"api_key":""}`)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("empty key: %d %s", w.Code, w.Body.String())
	}
}

func TestHandleLoginGarbageCreatedAtDoesNot500(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "login-garbage.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = raw.Exec(`
CREATE TABLE api_keys (
  key_hash TEXT PRIMARY KEY,
  email TEXT,
  wallet_address TEXT,
  source TEXT,
  created_at TEXT NOT NULL
);
`); err != nil {
		t.Fatal(err)
	}
	key := "login-garbage-key"
	sum := sha256.Sum256([]byte(key))
	hash := hex.EncodeToString(sum[:])
	if _, err = raw.Exec(
		`INSERT INTO api_keys (key_hash, email, wallet_address, source, created_at) VALUES (?, '', '', 'seed', ?)`,
		hash, "???not-a-time???",
	); err != nil {
		t.Fatal(err)
	}
	raw.Close()

	store, err := auth.NewSQLiteAPIKeyStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	handler := NewAPIKeyHandler(store, store, nil)
	body, _ := json.Marshal(map[string]string{"api_key": key})
	w := httptest.NewRecorder()
	handler.HandleLogin(w, httptest.NewRequest("POST", "/api/auth/login", bytes.NewBuffer(body)))
	if w.Code != http.StatusOK {
		t.Fatalf("login with garbage created_at: %d %s", w.Code, w.Body.String())
	}
}

func TestHandleLoginRejectsUnsignedWalletBind(t *testing.T) {
	store := auth.NewAPIKeyStore()
	rec, err := store.Issue("a@b.c", "", "registration")
	if err != nil {
		t.Fatal(err)
	}
	handler := NewAPIKeyHandler(store, store, nil)
	body, _ := json.Marshal(map[string]string{
		"api_key":        rec.Key,
		"wallet_address": "tb1qstolen",
	})
	w := httptest.NewRecorder()
	handler.HandleLogin(w, httptest.NewRequest("POST", "/api/auth/login", bytes.NewBuffer(body)))
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for unsigned wallet bind, got %d %s", w.Code, w.Body.String())
	}
}
