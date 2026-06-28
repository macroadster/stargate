package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleSurfaces(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/surfaces", nil)
	HandleSurfaces(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var cat SurfaceCatalog
	if err := json.NewDecoder(rec.Body).Decode(&cat); err != nil {
		t.Fatal(err)
	}
	if cat.Primary["smart_contract"] == "" {
		t.Fatal("missing primary smart_contract")
	}
	if len(cat.Aliases) == 0 {
		t.Fatal("expected aliases")
	}
	if cat.ToolREST["list_contracts"] == "" {
		t.Fatal("expected tool REST map")
	}
}

func TestWithDeprecationHeaders(t *testing.T) {
	h := WithDeprecation("/api/open-contracts", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/smart-contracts", nil)
	h(rec, req)
	if rec.Header().Get("Deprecation") != "true" {
		t.Fatal("missing Deprecation")
	}
	if rec.Header().Get("X-Stargate-Primary-Path") != "/api/open-contracts" {
		t.Fatalf("primary path header: %q", rec.Header().Get("X-Stargate-Primary-Path"))
	}
}
