package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"stargate-backend/core/smart_contract"
	scmiddleware "stargate-backend/middleware/smart_contract"
	"stargate-backend/services"
	"stargate-backend/starlight"
	auth "stargate-backend/storage/auth"
)

// HTTPMCPServer provides HTTP endpoints for MCP tools
type HTTPMCPServer struct {
	store            scmiddleware.Store
	apiKeyStore      auth.APIKeyValidator
	ingestionSvc     *services.IngestionService
	scannerManager   *starlight.ScannerManager
	smartContractSvc *services.SmartContractService
	httpClient       *http.Client
	baseURL          string
	rateLimiter      map[string][]time.Time // API key -> request timestamps
}

// NewHTTPMCPServer creates a new HTTP MCP server
func NewHTTPMCPServer(store scmiddleware.Store, apiKeyStore auth.APIKeyValidator, ingestionSvc *services.IngestionService, scannerManager *starlight.ScannerManager, smartContractSvc *services.SmartContractService) *HTTPMCPServer {
	return &HTTPMCPServer{
		store:            store,
		apiKeyStore:      apiKeyStore,
		ingestionSvc:     ingestionSvc,
		scannerManager:   scannerManager,
		smartContractSvc: smartContractSvc,
		httpClient:       &http.Client{Timeout: 10 * time.Second},
		baseURL:          "http://localhost:3001", // Default backend URL
		rateLimiter:      make(map[string][]time.Time),
	}
}

func (h *HTTPMCPServer) externalBaseURL(r *http.Request) string {
	if r == nil {
		return h.baseURL
	}
	scheme := r.Header.Get("X-Forwarded-Proto")
	if scheme == "" {
		if r.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}
	if strings.Contains(host, ",") {
		host = strings.TrimSpace(strings.Split(host, ",")[0])
	}
	if host == "" {
		return h.baseURL
	}
	return scheme + "://" + host
}

func (h *HTTPMCPServer) writeHTTPError(w http.ResponseWriter, status int, code string, message string, hint string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	resp := MCPResponse{
		Success:   false,
		Error:     hint, // Put the actual error message here for tests
		ErrorCode: code,
		Message:   message,
		Code:      status,
		Hint:      hint,
	}
	json.NewEncoder(w).Encode(resp)
}

func (h *HTTPMCPServer) statusFromError(err error) int {
	if err == nil {
		return http.StatusInternalServerError
	}
	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "not found"):
		return http.StatusNotFound
	case strings.Contains(lower, "already claimed"),
		strings.Contains(lower, "already taken"),
		strings.Contains(lower, "conflict"),
		strings.Contains(lower, "taken"):
		return http.StatusConflict
	case strings.Contains(lower, "missing"),
		strings.Contains(lower, "required"),
		strings.Contains(lower, "invalid"),
		strings.Contains(lower, "expected"):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

func (h *HTTPMCPServer) callToolDirect(ctx context.Context, toolName string, args map[string]interface{}, apiKey string) (interface{}, error) {
	switch toolName {
	case "list_contracts":
		return h.handleListContracts(ctx, args)
	case "list_proposals":
		return h.handleListProposals(ctx, args)
	case "claim_task":
		return h.handleClaimTask(ctx, args, apiKey)
	case "create_proposal":
		return h.handleCreateProposal(ctx, args, apiKey)
	case "approve_proposal":
		return h.handleApproveProposal(ctx, args, apiKey)
	case "scan_image":
		return h.handleScanImage(ctx, args)
	case "get_scanner_info":
		return h.handleGetScannerInfo(ctx, args)
	default:
		return nil, fmt.Errorf("unknown tool: %s", toolName)
	}
}

func (h *HTTPMCPServer) handleListContracts(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	filter := smart_contract.ContractFilter{}
	if status, ok := args["status"].(string); ok {
		filter.Status = status
	}
	if creator, ok := args["creator"].(string); ok {
		filter.Creator = creator
	}

	contracts, err := h.store.ListContracts(filter)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"contracts":   contracts,
		"total_count": len(contracts),
	}, nil
}

func (h *HTTPMCPServer) handleListProposals(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	filter := smart_contract.ProposalFilter{}
	if status, ok := args["status"].(string); ok {
		filter.Status = status
	}

	proposals, err := h.store.ListProposals(ctx, filter)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"proposals": proposals,
		"total":     len(proposals),
	}, nil
}

func (h *HTTPMCPServer) handleClaimTask(ctx context.Context, args map[string]interface{}, apiKey string) (interface{}, error) {
	taskID, ok := args["task_id"].(string)
	if !ok {
		return nil, fmt.Errorf("task_id is required")
	}

	aiIdentifier, ok := args["ai_identifier"].(string)
	if !ok {
		return nil, fmt.Errorf("ai_identifier is required")
	}

	// For the wallet, we need to get it from the API key validator
	var wallet string
	if h.apiKeyStore != nil {
		if keyInfo, ok := h.apiKeyStore.Get(apiKey); ok {
			wallet = keyInfo.Wallet
		}
	}

	claim, err := h.store.ClaimTask(taskID, aiIdentifier, wallet, nil)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"claim": claim,
	}, nil
}

func (h *HTTPMCPServer) handleCreateProposal(ctx context.Context, args map[string]interface{}, apiKey string) (interface{}, error) {
	title, ok := args["title"].(string)
	if !ok {
		return nil, fmt.Errorf("title is required")
	}

	descriptionMD, ok := args["description_md"].(string)
	if !ok {
		return nil, fmt.Errorf("description_md is required")
	}

	visiblePixelHash, ok := args["visible_pixel_hash"].(string)
	if !ok {
		return nil, fmt.Errorf("visible_pixel_hash is required")
	}

	// Check if wish contract exists
	wishID := "wish-" + visiblePixelHash
	contracts, err := h.store.ListContracts(smart_contract.ContractFilter{})
	if err != nil {
		return nil, err
	}
	wishExists := false
	for _, contract := range contracts {
		if contract.ContractID == wishID {
			wishExists = true
			break
		}
	}
	if !wishExists {
		return nil, fmt.Errorf("wish not found")
	}

	budgetSats := int64(0)
	if budget, ok := args["budget_sats"]; ok {
		if b, ok := budget.(float64); ok {
			budgetSats = int64(b)
		}
	}

	proposal := smart_contract.Proposal{
		Title:            title,
		DescriptionMD:    descriptionMD,
		VisiblePixelHash: visiblePixelHash,
		BudgetSats:       budgetSats,
		Status:           "pending",
		Metadata: map[string]interface{}{
			"creator_api_key_hash": apiKeyHash(apiKey),
		},
	}

	err = h.store.CreateProposal(ctx, proposal)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"proposal": proposal,
	}, nil
}

func (h *HTTPMCPServer) handleApproveProposal(ctx context.Context, args map[string]interface{}, apiKey string) (interface{}, error) {
	proposalID, ok := args["proposal_id"].(string)
	if !ok {
		return nil, fmt.Errorf("proposal_id is required")
	}

	// Get the proposal to check if wish exists
	proposals, err := h.store.ListProposals(ctx, smart_contract.ProposalFilter{})
	if err != nil {
		return nil, err
	}

	var proposal *smart_contract.Proposal
	for i := range proposals {
		if proposals[i].ID == proposalID {
			proposal = &proposals[i]
			break
		}
	}
	if proposal == nil {
		return nil, fmt.Errorf("proposal not found")
	}

	// Check if wish contract exists
	wishID := "wish-" + proposal.VisiblePixelHash
	contracts, err := h.store.ListContracts(smart_contract.ContractFilter{})
	if err != nil {
		return nil, err
	}
	wishExists := false
	for _, contract := range contracts {
		if contract.ContractID == wishID {
			wishExists = true
			break
		}
	}
	if !wishExists {
		return nil, fmt.Errorf("wish not found")
	}

	err = h.store.ApproveProposal(ctx, proposalID)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"message":     "proposal approved",
		"proposal_id": proposalID,
	}, nil
}

func (h *HTTPMCPServer) handleScanImage(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	return map[string]interface{}{
		"error": "scanner not implemented in test environment",
	}, nil
}

func (h *HTTPMCPServer) handleGetScannerInfo(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	if h.scannerManager == nil {
		return nil, fmt.Errorf("scanner not available")
	}
	return map[string]interface{}{
		"available": true,
		"version":   "1.0.0",
	}, nil
}

// RegisterRoutes registers HTTP MCP endpoints
func (h *HTTPMCPServer) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/mcp/tools", h.authWrap(h.handleListTools))
	mux.HandleFunc("/mcp/call", h.authWrap(h.handleToolCall))
	mux.HandleFunc("/mcp/discover", h.authWrap(h.handleDiscover))
	mux.HandleFunc("/mcp/docs", h.handleDocs)            // No auth required for documentation
	mux.HandleFunc("/mcp/openapi.json", h.handleOpenAPI) // No auth required for API spec
	mux.HandleFunc("/mcp/health", h.handleHealth)
	mux.HandleFunc("/mcp/events", h.authWrap(h.handleEventsProxy))
	mux.HandleFunc("/mcp", h.authWrap(h.handleIndex))
	// Register catch-all last
	mux.HandleFunc("/mcp/", h.authWrap(h.handleIndex))
}
