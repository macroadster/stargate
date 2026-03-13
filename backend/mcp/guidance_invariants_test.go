package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"stargate-backend/services"
	"stargate-backend/starlight"
	auth "stargate-backend/storage/auth"
)

func TestMCPGuidanceInvariants(t *testing.T) {
	ingestionSvc := &services.IngestionService{}
	scannerManager := &starlight.ScannerManager{}
	server := NewHTTPMCPServer(nil, nil, nil, ingestionSvc, scannerManager, nil, auth.NewChallengeStore(10*time.Minute))

	t.Run("no hardcoded dev hosts in SKILL.md", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/mcp/SKILL.md", nil)
		r.Host = "api.starlight.local"
		server.handleSkill(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		body := w.Body.String()

		devHosts := []string{"localhost", "127.0.0.1", "example.com"}
		for _, host := range devHosts {
			if strings.Contains(body, host) && !strings.Contains(body, "{{"+strings.ToUpper(host)+"}}") {
				t.Errorf("SKILL.md contains hardcoded dev host %q - should use request-aware URL substitution", host)
			}
		}
	})

	t.Run("no hardcoded dev hosts in SDK script", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/mcp/starlight_sdk.sh", nil)
		r.Host = "api.starlight.local"
		server.handleSDKScript(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		body := w.Body.String()

		devHosts := []string{"localhost", "127.0.0.1", "example.com"}
		for _, host := range devHosts {
			if strings.Contains(body, host) && !strings.Contains(body, "{{"+strings.ToUpper(host)+"}}") {
				t.Errorf("SDK script contains hardcoded dev host %q - should use request-aware URL substitution", host)
			}
		}
	})

	t.Run("no hardcoded dev hosts in docs", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/mcp/docs", nil)
		r.Host = "api.starlight.local"
		server.handleDocs(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		body := w.Body.String()

		devHosts := []string{"localhost", "127.0.0.1"}
		for _, host := range devHosts {
			if strings.Contains(body, host) {
				t.Errorf("docs contain hardcoded dev host %q - should use request-aware URL substitution", host)
			}
		}
	})

	t.Run("sdk discoverability in search metadata", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/mcp/search?q=upload", nil)
		server.handleToolSearch(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}

		tools, ok := resp["tools"].([]interface{})
		if !ok || len(tools) == 0 {
			t.Fatalf("expected tools in search results")
		}

		hasSDKMetadata := false
		for _, tool := range tools {
			toolMap, ok := tool.(map[string]interface{})
			if !ok {
				continue
			}
			if preferredClient, ok := toolMap["preferred_client"].(string); ok && preferredClient != "" {
				hasSDKMetadata = true
				if !strings.Contains(preferredClient, "sdk") && !strings.Contains(preferredClient, "shell") {
					t.Errorf("expected preferred_client to reference SDK or shell script, got %q", preferredClient)
				}
			}
			if docsHint, ok := toolMap["docs_hint"].(string); ok && docsHint != "" {
				hasSDKMetadata = true
				if !strings.Contains(docsHint, "SKILL.md") && !strings.Contains(docsHint, "starlight_sdk") {
					t.Errorf("expected docs_hint to reference SKILL.md or starlight_sdk, got %q", docsHint)
				}
			}
		}

		if !hasSDKMetadata {
			t.Errorf("search results should include SDK discoverability metadata (preferred_client, docs_hint)")
		}
	})

	t.Run("mcp index references docs and skill", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/mcp", nil)
		r.Host = "api.starlight.local"
		server.handleIndex(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		body := w.Body.String()

		if !strings.Contains(body, "/mcp/docs") {
			t.Error("/mcp should reference /mcp/docs")
		}
		if !strings.Contains(body, "/mcp/SKILL.md") {
			t.Error("/mcp should reference /mcp/SKILL.md")
		}
		if !strings.Contains(body, "/mcp/starlight_sdk.sh") {
			t.Error("/mcp should reference /mcp/starlight_sdk.sh")
		}
	})

	t.Run("docs references skill and sdk", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/mcp/docs", nil)
		r.Host = "api.starlight.local"
		server.handleDocs(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		body := w.Body.String()

		if !strings.Contains(body, "/mcp/SKILL.md") {
			t.Error("/mcp/docs should reference /mcp/SKILL.md")
		}
		if !strings.Contains(body, "/mcp/starlight_sdk.sh") {
			t.Error("/mcp/docs should reference /mcp/starlight_sdk.sh")
		}
	})

	t.Run("skill references search and tools", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/mcp/SKILL.md", nil)
		r.Host = "api.starlight.local"
		server.handleSkill(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		body := w.Body.String()

		if !strings.Contains(body, "/mcp/search") && !strings.Contains(body, "{{BASE_URL}}/mcp/search") {
			t.Error("/mcp/SKILL.md should reference /mcp/search")
		}
		if !strings.Contains(body, "/mcp/tools") && !strings.Contains(body, "{{BASE_URL}}/mcp/tools") {
			t.Error("/mcp/SKILL.md should reference /mcp/tools")
		}
	})

	t.Run("search endpoint is accessible without auth", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/mcp/search?q=test", nil)
		server.handleToolSearch(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("docs endpoint is accessible without auth", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/mcp/docs", nil)
		server.handleDocs(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("skill endpoint is accessible without auth", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/mcp/SKILL.md", nil)
		server.handleSkill(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("tools endpoint exposes agent_assets", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/mcp/tools", nil)
		r.Host = "api.starlight.local"
		server.handleListTools(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}

		agentAssets, ok := resp["agent_assets"].(map[string]interface{})
		if !ok {
			t.Error("/mcp/tools should expose agent_assets")
			return
		}

		if _, ok := agentAssets["skill"]; !ok {
			t.Error("agent_assets should include skill reference")
		}
		if _, ok := agentAssets["sdk"]; !ok {
			t.Error("agent_assets should include sdk reference")
		}
	})
}
