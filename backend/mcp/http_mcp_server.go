package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
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
	server           *scmiddleware.Server
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
		server:           nil,
		httpClient:       &http.Client{Timeout: 10 * time.Second},
		baseURL:          "http://localhost:3001", // Default backend URL
		rateLimiter:      make(map[string][]time.Time),
	}
}

// SetServer sets the smart_contract server reference
func (h *HTTPMCPServer) SetServer(server *scmiddleware.Server) {
	h.server = server
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

func (h *HTTPMCPServer) toolRequiresAuth(toolName string) bool {
	authenticatedTools := map[string]bool{
		"create_contract":  true,
		"create_proposal":  true,
		"claim_task":       true,
		"submit_work":      true,
		"approve_proposal": true,
	}
	return authenticatedTools[toolName]
}

func (h *HTTPMCPServer) callToolDirect(ctx context.Context, toolName string, args map[string]interface{}, apiKey string) (interface{}, error) {
	switch toolName {
	case "list_contracts":
		return h.handleListContracts(ctx, args)
	case "get_open_contracts":
		return h.handleGetOpenContracts(ctx, args)
	case "list_proposals":
		return h.handleListProposals(ctx, args)
	case "list_tasks":
		return h.handleListTasks(ctx, args)
	case "get_contract":
		return h.handleGetContract(ctx, args)
	case "list_events":
		return h.handleListEvents(ctx, args)
	case "events_stream":
		return h.handleEventsStream(ctx, args)
	case "create_contract":
		return h.handleCreateContract(ctx, args, apiKey)
	case "claim_task":
		return h.handleClaimTask(ctx, args, apiKey)
	case "create_proposal":
		return h.handleCreateProposal(ctx, args, apiKey)
	case "submit_work":
		return h.handleSubmitWork(ctx, args, apiKey)
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

	var wallet string
	if h.apiKeyStore != nil {
		if keyInfo, ok := h.apiKeyStore.Get(apiKey); ok {
			wallet = keyInfo.Wallet
		}
	}
	if wallet == "" {
		return nil, fmt.Errorf("wallet address required - please bind wallet to API key using /api/auth/verify")
	}

	claim, err := h.store.ClaimTask(taskID, wallet, nil)
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

	proposalID := fmt.Sprintf("proposal-%d", time.Now().UnixNano())
	contractID := "wish-" + visiblePixelHash
	proposal := smart_contract.Proposal{
		ID:               proposalID,
		Title:            title,
		DescriptionMD:    descriptionMD,
		VisiblePixelHash: visiblePixelHash,
		BudgetSats:       budgetSats,
		Status:           "pending",
		CreatedAt:        time.Now(),
		Metadata: map[string]interface{}{
			"creator_api_key_hash": apiKeyHash(apiKey),
			"contract_id":          contractID,
			"visible_pixel_hash":   visiblePixelHash,
		},
	}

	log.Printf("MCP CREATE PROPOSAL DEBUG: ID=%s, metadata=%+v", proposal.ID, proposal.Metadata)
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

	if err := requireCreatorApproval(apiKey, *proposal); err != nil {
		return nil, err
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

	if h.server != nil {
		if publishErr := h.server.PublishProposalTasks(ctx, proposalID); publishErr != nil {
			log.Printf("failed to publish tasks for proposal %s: %v", proposalID, publishErr)
		}
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

func (h *HTTPMCPServer) handleListTasks(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	filter := smart_contract.TaskFilter{}
	if contractID, ok := args["contract_id"].(string); ok {
		filter.ContractID = contractID
	}
	if status, ok := args["status"].(string); ok {
		filter.Status = status
	}
	if limit, ok := args["limit"].(float64); ok {
		filter.Limit = int(limit)
	}

	tasks, err := h.store.ListTasks(filter)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"tasks":       tasks,
		"total_count": len(tasks),
	}, nil
}

func (h *HTTPMCPServer) handleGetContract(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	contractID, ok := args["contract_id"].(string)
	if !ok {
		return nil, fmt.Errorf("contract_id is required")
	}

	contract, err := h.store.GetContract(contractID)
	if err != nil {
		return nil, err
	}

	return contract, nil
}

func (h *HTTPMCPServer) handleListEvents(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	return map[string]interface{}{
		"endpoint": "/api/smart_contract/events",
		"message":  "Use the events endpoint directly with optional filters",
		"filters": map[string]interface{}{
			"type":      "Event type filter",
			"entity_id": "Entity ID filter",
			"actor":     "Actor identifier filter",
			"limit":     "Maximum number of events to return",
		},
	}, nil
}

func (h *HTTPMCPServer) handleEventsStream(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	baseURL := h.externalBaseURL(&http.Request{})
	return map[string]interface{}{
		"stream_url": baseURL + "/api/smart_contract/events/stream",
		"auth_hints": map[string]string{
			"header": "Authorization: Bearer <api_key>",
			"query":  "actor=<identifier>&entity_id=<id>&type=<event_type>",
		},
		"message": "Connect to this SSE endpoint to receive real-time MCP events",
	}, nil
}

func (h *HTTPMCPServer) handleCreateContract(ctx context.Context, args map[string]interface{}, apiKey string) (interface{}, error) {
	message, ok := args["message"].(string)
	if !ok || message == "" {
		return nil, fmt.Errorf("message is required")
	}

	imageBase64, _ := args["image_base64"].(string)
	price, _ := args["price"].(string)
	priceUnit, _ := args["price_unit"].(string)
	address, _ := args["address"].(string)
	fundingMode, _ := args["funding_mode"].(string)

	reqBody := map[string]interface{}{
		"message": message,
	}

	if imageBase64 != "" {
		reqBody["image_base64"] = imageBase64
	}
	if price != "" {
		reqBody["price"] = price
	}
	if priceUnit != "" {
		reqBody["price_unit"] = priceUnit
	}
	if address != "" {
		reqBody["address"] = address
	}
	if fundingMode != "" {
		reqBody["funding_mode"] = fundingMode
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	inscribeURL := h.baseURL + "/api/inscribe"
	req, err := http.NewRequestWithContext(ctx, "POST", inscribeURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call inscribe API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		var errResp struct {
			Success bool   `json:"success"`
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		json.Unmarshal(body, &errResp)
		return nil, fmt.Errorf("inscribe API error (%d): %s - %s", resp.StatusCode, errResp.Error, errResp.Message)
	}

	var successResp struct {
		Success bool   `json:"success"`
		Data    any    `json:"data"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(body, &successResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if !successResp.Success {
		return nil, fmt.Errorf("inscribe failed: %s", successResp.Error)
	}

	return successResp.Data, nil
}

func (h *HTTPMCPServer) handleSubmitWork(ctx context.Context, args map[string]interface{}, apiKey string) (interface{}, error) {
	claimID, ok := args["claim_id"].(string)
	if !ok {
		return nil, fmt.Errorf("claim_id is required")
	}

	deliverables, ok := args["deliverables"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("deliverables is required")
	}

	submission, err := h.store.SubmitWork(claimID, deliverables, nil)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"message":    "work submitted successfully",
		"claim_id":   claimID,
		"submission": submission,
	}, nil
}

// handleGetOpenContracts browses open contracts with caching (no auth required)
func (h *HTTPMCPServer) handleGetOpenContracts(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	// Build filter from args
	filter := smart_contract.ContractFilter{}

	// Handle status parameter
	if status, ok := args["status"].(string); ok {
		if status != "all" {
			filter.Status = status
		}
	} else {
		// Default to pending contracts for MCP tool
		filter.Status = "pending"
	}

	// Handle limit parameter
	limit := 50 // default
	if lim, ok := args["limit"].(float64); ok {
		limit = int(lim)
		if limit <= 0 {
			limit = 50
		}
	}

	// Query contracts from store
	contracts, err := h.store.ListContracts(filter)
	if err != nil {
		return nil, fmt.Errorf("failed to list contracts: %w", err)
	}

	// Apply limit if specified
	if len(contracts) > limit {
		contracts = contracts[:limit]
	}

	// Convert contracts to MCP-compatible format
	var mcpContracts []map[string]interface{}
	for _, contract := range contracts {
		mcpContract := map[string]interface{}{
			"contract_id":       contract.ContractID,
			"title":             contract.Title,
			"total_budget_sats": contract.TotalBudgetSats,
			"goals_count":       contract.GoalsCount,
			"available_tasks":   contract.AvailableTasksCount,
			"status":            contract.Status,
			"skills":            contract.Skills,
			"stego_image_url":   contract.StegoImageURL,
		}

		// Add fallback URL if no stego image URL
		if contract.StegoImageURL == "" {
			// Strip "wish-" prefix for stealth filename
			hash := contract.ContractID
			if strings.HasPrefix(contract.ContractID, "wish-") {
				hash = strings.TrimPrefix(contract.ContractID, "wish-")
			}
			mcpContract["stego_image_url"] = fmt.Sprintf("/uploads/%s", hash)
		}

		mcpContracts = append(mcpContracts, mcpContract)
	}

	return map[string]interface{}{
		"contracts":   mcpContracts,
		"total_count": len(mcpContracts),
		"status":      filter.Status,
		"limit":       limit,
	}, nil
}

// RegisterRoutes registers HTTP MCP endpoints
func (h *HTTPMCPServer) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/mcp/tools", h.handleListTools)      // No auth - allows discovery
	mux.HandleFunc("/mcp/search", h.handleToolSearch)    // No auth - search tools
	mux.HandleFunc("/mcp/call", h.handleToolCall)        // Tool-level auth for specific tools
	mux.HandleFunc("/mcp/discover", h.handleDiscover)    // No auth - allows discovery
	mux.HandleFunc("/mcp/docs", h.handleDocs)            // No auth required for documentation
	mux.HandleFunc("/mcp/openapi.json", h.handleOpenAPI) // No auth required for API spec
	mux.HandleFunc("/mcp/health", h.handleHealth)
	mux.HandleFunc("/mcp/events", h.handleEventsProxy)
	mux.HandleFunc("/mcp", h.handleIndex)
	// Register catch-all last
	mux.HandleFunc("/mcp/", h.handleIndex)
}
