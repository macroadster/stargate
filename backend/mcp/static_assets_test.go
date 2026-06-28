package mcp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"stargate-backend/services"
	"stargate-backend/starlight"
	auth "stargate-backend/storage/auth"
)

func TestStaticMCPAssets(t *testing.T) {
	ingestionSvc := &services.IngestionService{}
	scannerManager := &starlight.ScannerManager{}
	server := NewHTTPMCPServer(nil, nil, nil, ingestionSvc, scannerManager, nil, auth.NewChallengeStore(10*time.Minute))

	t.Run("skill endpoint serves markdown", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/mcp/SKILL.md", nil)
		server.handleSkill(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		if contentType := w.Header().Get("Content-Type"); !strings.Contains(contentType, "text/markdown") {
			t.Fatalf("expected markdown content type, got %q", contentType)
		}
		if !strings.Contains(w.Body.String(), "Starlight MCP Skill") {
			t.Fatalf("expected skill markdown body")
		}
		if !strings.Contains(w.Body.String(), "http://example.com/mcp/starlight_sdk.sh") {
			t.Fatalf("expected request-aware sdk url in skill body")
		}
	})

	t.Run("sdk endpoint serves shell script attachment", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/mcp/starlight_sdk.sh", nil)
		server.handleSDKScript(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		if contentType := w.Header().Get("Content-Type"); !strings.Contains(contentType, "shellscript") {
			t.Fatalf("expected shellscript content type, got %q", contentType)
		}
		if disposition := w.Header().Get("Content-Disposition"); !strings.Contains(disposition, "starlight_sdk.sh") {
			t.Fatalf("expected attachment filename in content disposition, got %q", disposition)
		}
		if !strings.Contains(w.Body.String(), "starlight_sdk.sh create-wish") {
			t.Fatalf("expected sdk script body")
		}
		if !strings.Contains(w.Body.String(), "MCP_BASE=${MCP_BASE:-http://example.com/mcp}") {
			t.Fatalf("expected request-aware mcp base in sdk script")
		}
	})
}
