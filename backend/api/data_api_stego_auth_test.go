package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStegoCallbackRejectsWhenSecretUnset(t *testing.T) {
	t.Setenv("STARLIGHT_CALLBACK_SECRET", "")
	api := &DataAPI{}
	req := httptest.NewRequest(http.MethodPost, "/api/stego/callback", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()

	api.HandleStegoCallback(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestStegoCallbackRejectsInvalidSignature(t *testing.T) {
	t.Setenv("STARLIGHT_CALLBACK_SECRET", "secret")
	api := &DataAPI{}
	req := httptest.NewRequest(http.MethodPost, "/api/stego/callback", strings.NewReader(`{}`))
	req.Header.Set("X-Starlight-Signature", "invalid")
	rec := httptest.NewRecorder()

	api.HandleStegoCallback(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}
