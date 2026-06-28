package mcp

// ToolMetadata contains lightweight metadata for search.
type ToolMetadata struct {
	Name            string   `json:"name"`
	Description     string   `json:"description"`
	Category        string   `json:"category"`
	AuthRequired    bool     `json:"auth_required"`
	PreferredClient string   `json:"preferred_client,omitempty"`
	DocsHint        string   `json:"docs_hint,omitempty"`
	Keywords        []string `json:"keywords,omitempty"`
}

// getToolSchemas returns detailed schemas for all available tools.
// Schemas come from GuidanceManifest only (SKILL-driven). There is no second
// hardcoded schema map (retired getToolSchemasLegacy / fallback table).
func (h *HTTPMCPServer) getToolSchemas() map[string]interface{} {
	if h.guidance != nil {
		return h.guidance.GetToolSchemas()
	}
	return map[string]interface{}{}
}

// getToolList returns lightweight metadata for all tools.
func (h *HTTPMCPServer) getToolList() []ToolMetadata {
	if h.guidance != nil {
		return h.guidance.GetToolList()
	}
	return nil
}

// searchTools filters tools by keyword and category.
func (h *HTTPMCPServer) searchTools(query string, category string, limit int) []ToolMetadata {
	if h.guidance != nil {
		return h.guidance.SearchTools(query, category, limit)
	}
	return nil
}
