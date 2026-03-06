package mcp

import (
	"embed"
	"net/http"
	"strings"
)

//go:embed assets/SKILL.md assets/starlight_sdk.sh
var mcpAssets embed.FS

func (h *HTTPMCPServer) serveEmbeddedMCPFile(w http.ResponseWriter, r *http.Request, name string, contentType string, attachmentName string) {
	if r.Method != http.MethodGet {
		h.writeHTTPError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", "Use GET for this MCP asset.")
		return
	}

	data, err := mcpAssets.ReadFile(name)
	if err != nil {
		h.writeHTTPError(w, http.StatusNotFound, "MCP_ASSET_NOT_FOUND", "MCP asset not found", "Verify the requested MCP asset is packaged with the server.")
		return
	}

	base := h.externalBaseURL(r)
	mcpBase := base + "/mcp"
	sdkURL := mcpBase + "/starlight_sdk.sh"
	replacer := strings.NewReplacer(
		"{{BASE_URL}}", base,
		"{{MCP_BASE}}", mcpBase,
		"{{SDK_URL}}", sdkURL,
		"{{MCP_BASE_PATH}}", "/mcp",
		"{{API_BASE_PATH}}", "/api",
	)
	rendered := replacer.Replace(string(data))

	w.Header().Set("Content-Type", contentType)
	if attachmentName != "" {
		w.Header().Set("Content-Disposition", `attachment; filename="`+attachmentName+`"`)
	}
	_, _ = w.Write([]byte(rendered))
}

func (h *HTTPMCPServer) handleSkill(w http.ResponseWriter, r *http.Request) {
	h.serveEmbeddedMCPFile(w, r, "assets/SKILL.md", "text/markdown; charset=utf-8", "")
}

func (h *HTTPMCPServer) handleSDKScript(w http.ResponseWriter, r *http.Request) {
	h.serveEmbeddedMCPFile(w, r, "assets/starlight_sdk.sh", "text/x-shellscript; charset=utf-8", "starlight_sdk.sh")
}
