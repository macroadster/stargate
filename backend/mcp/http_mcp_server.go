package mcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"stargate-backend/core"
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
	challengeStore   *auth.ChallengeStore
}

// NewHTTPMCPServer creates a new HTTP MCP server
func NewHTTPMCPServer(store scmiddleware.Store, apiKeyStore auth.APIKeyValidator, ingestionSvc *services.IngestionService, scannerManager *starlight.ScannerManager, smartContractSvc *services.SmartContractService, challengeStore *auth.ChallengeStore) *HTTPMCPServer {
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
		challengeStore:   challengeStore,
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
	h.writeHTTPStructuredError(w, status, &ToolError{
		Code:       code,
		Message:    message,
		Hint:       hint,
		HttpStatus: status,
	})
}

// writeHTTPStructuredError writes structured error responses
func (h *HTTPMCPServer) writeHTTPStructuredError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	resp := MCPResponse{
		Success: false,
		Code:    status,
	}

	// Handle different error types
	switch e := err.(type) {
	case *ToolError:
		resp.ErrorCode = e.Code
		resp.Error = e.Message
		resp.Hint = e.Hint
		resp.Message = e.Message

		// Add structured details
		if e.Tool != "" {
			if resp.Details == nil {
				resp.Details = make(map[string]interface{})
			}
			resp.Details["tool"] = e.Tool
		}
		if e.Field != "" {
			if resp.Details == nil {
				resp.Details = make(map[string]interface{})
			}
			resp.Details["field"] = e.Field
			resp.Details["field_value"] = e.FieldValue
		}
		if e.DocsURL != "" {
			resp.DocsURL = e.DocsURL
		}
		if len(e.Details) > 0 {
			if resp.Details == nil {
				resp.Details = make(map[string]interface{})
			}
			for k, v := range e.Details {
				resp.Details[k] = v
			}
		}

	case *ValidationError:
		resp.ErrorCode = ErrCodeValidationFailed
		resp.Error = e.Message
		resp.Hint = e.Hint
		resp.Message = e.Message

		// Add all field validation errors
		if resp.Details == nil {
			resp.Details = make(map[string]interface{})
		}
		resp.Details["tool"] = e.Tool
		resp.Details["validation_errors"] = e.Fields

		// Build required fields list for easier parsing
		var requiredFields []string
		for field, fieldErr := range e.Fields {
			if fieldErr.Required {
				requiredFields = append(requiredFields, field)
			}
		}
		if len(requiredFields) > 0 {
			resp.RequiredFields = requiredFields
		}

		if e.DocsURL != "" {
			resp.DocsURL = e.DocsURL
		}

	default:
		// Fallback for generic errors
		resp.ErrorCode = ErrCodeInternalError
		resp.Error = err.Error()
		resp.Message = "Internal server error"
		resp.Hint = "Please try again. If the problem persists, contact support"
	}

	// Add timestamp and version for all errors
	resp.Timestamp = time.Now().Format(time.RFC3339)
	resp.Version = "1.0.0"

	json.NewEncoder(w).Encode(resp)
}

// writeStructuredErrorJSONRPC writes a structured error response without setting HTTP status (always 200 OK for JSON-RPC)
func (h *HTTPMCPServer) writeStructuredErrorJSONRPC(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	// Do NOT set HTTP status - always return 200 OK for JSON-RPC

	resp := MCPResponse{
		Success: false,
	}

	// Handle different error types (same logic as writeHTTPStructuredError but without status)
	switch e := err.(type) {
	case *ToolError:
		resp.ErrorCode = e.Code
		resp.Error = e.Message
		resp.Hint = e.Hint
		resp.Message = e.Message

		// Add structured details
		if e.Tool != "" {
			if resp.Details == nil {
				resp.Details = make(map[string]interface{})
			}
			resp.Details["tool"] = e.Tool
		}
		if e.Field != "" {
			if resp.Details == nil {
				resp.Details = make(map[string]interface{})
			}
			resp.Details["field"] = e.Field
			resp.Details["field_value"] = e.FieldValue
		}
		if e.DocsURL != "" {
			resp.DocsURL = e.DocsURL
		}
		if len(e.Details) > 0 {
			if resp.Details == nil {
				resp.Details = make(map[string]interface{})
			}
			for k, v := range e.Details {
				resp.Details[k] = v
			}
		}

	case *ValidationError:
		resp.ErrorCode = ErrCodeValidationFailed
		resp.Error = e.Message
		resp.Hint = e.Hint
		resp.Message = e.Message

		// Add all field validation errors
		if resp.Details == nil {
			resp.Details = make(map[string]interface{})
		}
		resp.Details["tool"] = e.Tool
		resp.Details["validation_errors"] = e.Fields

		// Build required fields list for easier parsing
		var requiredFields []string
		for field, fieldErr := range e.Fields {
			if fieldErr.Required {
				requiredFields = append(requiredFields, field)
			}
		}
		if len(requiredFields) > 0 {
			resp.RequiredFields = requiredFields
		}

		if e.DocsURL != "" {
			resp.DocsURL = e.DocsURL
		}

	default:
		// Fallback for generic errors
		resp.ErrorCode = ErrCodeInternalError
		resp.Error = err.Error()
		resp.Message = "Internal server error"
		resp.Hint = "Please try again. If the problem persists, contact support"
	}

	// Add timestamp and version for all errors
	resp.Timestamp = time.Now().Format(time.RFC3339)
	resp.Version = "1.0.0"

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
		"create_proposal":       true,
		"create_wish":           true,
		"claim_task":            true,
		"submit_work":           true,
		"approve_proposal":      true,
		"get_auth_challenge":    false, // No auth required - discovery tool
		"verify_auth_challenge": false, // No auth required - solves chicken-egg problem
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
	case "create_wish":
		return h.handleCreateWish(ctx, args, apiKey)
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
	case "get_auth_challenge":
		return h.handleGetAuthChallenge(ctx, args)
	case "verify_auth_challenge":
		return h.handleVerifyAuthChallenge(ctx, args)
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
	validation := NewValidationError("claim_task", "Invalid request parameters")

	taskID, ok := args["task_id"].(string)
	if !ok || taskID == "" {
		validation.AddFieldError("task_id", args["task_id"], "task_id is required and must be a string", true)
	}

	var wallet string
	if h.apiKeyStore != nil {
		if keyInfo, ok := h.apiKeyStore.Get(apiKey); ok {
			wallet = keyInfo.Wallet
		}
	}
	if wallet == "" {
		return nil, NewUnauthorizedError("claim_task", "wallet address required - please bind wallet to API key using /api/auth/verify")
	}

	// Return validation errors if any
	if validation.HasErrors() {
		return nil, validation
	}

	claim, err := h.store.ClaimTask(taskID, wallet, nil)
	if err != nil {
		// Convert common errors to structured errors
		if strings.Contains(err.Error(), "not found") {
			return nil, NewNotFoundError("claim_task", "task", taskID)
		}
		if strings.Contains(err.Error(), "already claimed") {
			return nil, NewClaimTaskError("ALREADY_CLAIMED", "Task has already been claimed", "task_id")
		}
		return nil, err
	}

	return map[string]interface{}{
		"claim": claim,
	}, nil
}

func (h *HTTPMCPServer) handleCreateProposal(ctx context.Context, args map[string]interface{}, apiKey string) (interface{}, error) {
	validation := NewValidationError("create_proposal", "Invalid request parameters")

	title, ok := args["title"].(string)
	if !ok || title == "" {
		validation.AddFieldError("title", args["title"], "title is required and must be a non-empty string", true)
	}

	descriptionMD, ok := args["description_md"].(string)
	if !ok || descriptionMD == "" {
		validation.AddFieldError("description_md", args["description_md"], "description_md is required and must be a non-empty string", true)
	}

	visiblePixelHash, ok := args["visible_pixel_hash"].(string)
	if !ok || visiblePixelHash == "" {
		validation.AddFieldError("visible_pixel_hash", args["visible_pixel_hash"], "visible_pixel_hash is required and must be a string", true)
	}

	// Validate budget_sats if provided
	var budgetSats int64 = 0
	if budget, ok := args["budget_sats"]; ok {
		if b, ok := budget.(float64); ok {
			if b < 0 {
				validation.AddFieldError("budget_sats", budget, "budget_sats must be a non-negative number", false)
			} else {
				budgetSats = int64(b)
			}
		} else {
			validation.AddTypeError("budget_sats", budget, "number")
		}
	}

	// Return validation errors if any
	if validation.HasErrors() {
		return nil, validation
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
		return nil, NewNotFoundError("create_proposal", "wish", visiblePixelHash)
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
		return nil, NewInternalError("create_proposal", fmt.Sprintf("Failed to create proposal: %v", err))
	}

	return map[string]interface{}{
		"proposal": proposal,
	}, nil
}

func (h *HTTPMCPServer) handleApproveProposal(ctx context.Context, args map[string]interface{}, apiKey string) (interface{}, error) {
	validation := NewValidationError("approve_proposal", "Invalid request parameters")

	proposalID, ok := args["proposal_id"].(string)
	if !ok || proposalID == "" {
		validation.AddFieldError("proposal_id", args["proposal_id"], "proposal_id is required and must be a string", true)
	}

	// Return validation errors if any
	if validation.HasErrors() {
		return nil, validation
	}

	// Get the proposal to check if wish exists
	proposals, err := h.store.ListProposals(ctx, smart_contract.ProposalFilter{})
	if err != nil {
		return nil, NewInternalError("approve_proposal", fmt.Sprintf("Failed to list proposals: %v", err))
	}

	var proposal *smart_contract.Proposal
	for i := range proposals {
		if proposals[i].ID == proposalID {
			proposal = &proposals[i]
			break
		}
	}
	if proposal == nil {
		return nil, NewNotFoundError("approve_proposal", "proposal", proposalID)
	}

	if err := h.requireAuthorizedApprover(apiKey, *proposal); err != nil {
		return nil, NewUnauthorizedError("approve_proposal", fmt.Sprintf("Not authorized to approve this proposal: %v", err))
	}

	// Check if wish contract exists
	wishID := "wish-" + proposal.VisiblePixelHash
	contracts, err := h.store.ListContracts(smart_contract.ContractFilter{})
	if err != nil {
		return nil, NewInternalError("approve_proposal", fmt.Sprintf("Failed to check wish existence: %v", err))
	}
	wishExists := false
	for _, contract := range contracts {
		if contract.ContractID == wishID {
			wishExists = true
			break
		}
	}
	if !wishExists {
		return nil, NewNotFoundError("approve_proposal", "wish", proposal.VisiblePixelHash)
	}

	err = h.store.ApproveProposal(ctx, proposalID)
	if err != nil {
		return nil, NewInternalError("approve_proposal", fmt.Sprintf("Failed to approve proposal: %v", err))
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

func (h *HTTPMCPServer) requireAuthorizedApprover(apiKey string, proposal smart_contract.Proposal) error {
	currentHash := apiKeyHash(apiKey)
	if currentHash == "" {
		return fmt.Errorf("api key required for approval")
	}

	// 1. Check if matches Proposal Creator
	if proposal.Metadata != nil {
		if creatorHash, ok := proposal.Metadata["creator_api_key_hash"].(string); ok {
			if strings.TrimSpace(creatorHash) == currentHash {
				return nil
			}
		}
	}

	// 2. Check if matches Wish Creator
	visibleHash := strings.TrimSpace(proposal.VisiblePixelHash)
	if visibleHash == "" {
		if v, ok := proposal.Metadata["visible_pixel_hash"].(string); ok {
			visibleHash = strings.TrimSpace(v)
		}
	}

	if visibleHash != "" && h.ingestionSvc != nil {
		// Try both hash and wish-hash
		rec, err := h.ingestionSvc.Get(visibleHash)
		if err != nil {
			rec, _ = h.ingestionSvc.Get("wish-" + visibleHash)
		}

		if rec != nil && rec.Metadata != nil {
			if wishCreatorHash, ok := rec.Metadata["creator_api_key_hash"].(string); ok {
				if strings.TrimSpace(wishCreatorHash) == currentHash {
					return nil
				}
			}
		}
	}

	// 3. Fallback: if no creator info exists at all, allow for now to prevent deadlock on old data
	// (But if it exists and doesn't match, we reject)
	hasAnyCreatorInfo := false
	if proposal.Metadata != nil {
		if _, ok := proposal.Metadata["creator_api_key_hash"].(string); ok {
			hasAnyCreatorInfo = true
		}
	}
	if !hasAnyCreatorInfo && visibleHash != "" && h.ingestionSvc != nil {
		rec, _ := h.ingestionSvc.Get(visibleHash)
		if rec != nil && rec.Metadata != nil {
			if _, ok := rec.Metadata["creator_api_key_hash"].(string); ok {
				hasAnyCreatorInfo = true
			}
		}
	}

	if !hasAnyCreatorInfo {
		log.Printf("WARNING: allowing approval for proposal %s with NO creator info", proposal.ID)
		return nil
	}

	return fmt.Errorf("approver does not match proposal creator or wish creator")
}

func (h *HTTPMCPServer) handleScanImage(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	if h.scannerManager == nil {
		return nil, NewServiceUnavailableError("scan_image", "scanner")
	}

	imageDataStr, ok := args["image_data"].(string)
	if !ok || imageDataStr == "" {
		return nil, NewValidationError("scan_image", "image_data is required")
	}

	imageData, err := base64.StdEncoding.DecodeString(imageDataStr)
	if err != nil {
		return nil, NewValidationError("scan_image", "invalid base64 image data: "+err.Error())
	}

	scanResult, err := h.scannerManager.ScanImage(imageData, core.ScanOptions{
		ExtractMessage:      true,
		ConfidenceThreshold: 0.5,
		IncludeMetadata:     true,
	})
	if err != nil {
		return nil, fmt.Errorf("scan failed: %w", err)
	}

	return map[string]interface{}{
		"is_stego":          scanResult.IsStego,
		"stego_probability": scanResult.StegoProbability,
		"confidence":        scanResult.Confidence,
		"prediction":        scanResult.Prediction,
		"stego_type":        scanResult.StegoType,
		"extracted_message": scanResult.ExtractedMessage,
		"extraction_error":  scanResult.ExtractionError,
	}, nil
}

func (h *HTTPMCPServer) handleGetScannerInfo(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	if h.scannerManager == nil {
		return nil, NewServiceUnavailableError("get_scanner_info", "scanner")
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
	validation := NewValidationError("get_contract", "Invalid request parameters")

	contractID, ok := args["contract_id"].(string)
	if !ok || contractID == "" {
		validation.AddFieldError("contract_id", args["contract_id"], "contract_id is required and must be a string", true)
	}

	// Return validation errors if any
	if validation.HasErrors() {
		return nil, validation
	}

	contract, err := h.store.GetContract(contractID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return nil, NewNotFoundError("get_contract", "contract", contractID)
		}
		return nil, NewInternalError("get_contract", fmt.Sprintf("Failed to get contract: %v", err))
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

func (h *HTTPMCPServer) handleCreateWish(ctx context.Context, args map[string]interface{}, apiKey string) (interface{}, error) {
	validation := NewValidationError("create_wish", "Invalid request parameters")

	message, ok := args["message"].(string)
	if !ok || message == "" {
		validation.AddFieldError("message", args["message"], "message is required and must be a non-empty string", true)
	}

	// Validate optional fields
	imageBase64, _ := args["image_base64"].(string)
	price, priceOk := args["price"].(string)
	priceUnit, unitOk := args["price_unit"].(string)
	address, _ := args["address"].(string)
	fundingMode, modeOk := args["funding_mode"].(string)

	// Validate price_unit if price is provided
	if priceOk && price != "" && (!unitOk || priceUnit == "") {
		validation.AddFieldError("price_unit", args["price_unit"], "price_unit is required when price is provided", false)
	}

	// Validate funding_mode value
	if modeOk && fundingMode != "" && fundingMode != "payout" && fundingMode != "raise_fund" {
		validation.AddFieldError("funding_mode", fundingMode, "funding_mode must be 'payout' or 'raise_fund'", false)
	}

	// Return validation errors if any
	if validation.HasErrors() {
		return nil, validation
	}

	reqBody := map[string]interface{}{
		"message":       message,
		"skip_proposal": true,
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
		return nil, NewInternalError("create_wish", fmt.Sprintf("Failed to marshal request: %v", err))
	}

	inscribeURL := h.baseURL + "/api/inscribe"
	req, err := http.NewRequestWithContext(ctx, "POST", inscribeURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, NewInternalError("create_wish", fmt.Sprintf("Failed to create request: %v", err))
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return nil, NewServiceUnavailableError("create_wish", "inscribe API")
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, NewInternalError("create_wish", "Failed to read response from inscribe API")
	}

	if resp.StatusCode >= 400 {
		// Try to parse error as object first
		var errObjResp struct {
			Success bool `json:"success"`
			Error   struct {
				Code      string `json:"code"`
				Message   string `json:"message"`
				Timestamp string `json:"timestamp"`
				RequestID string `json:"request_id"`
			} `json:"error"`
		}
		if err := json.Unmarshal(body, &errObjResp); err == nil {
			return nil, NewCreateWishError("INSCRIBE_ERROR", fmt.Sprintf("Inscribe API error: %s", errObjResp.Error.Message), "")
		}

		// Fallback to string error
		var errStrResp struct {
			Success bool   `json:"success"`
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(body, &errStrResp); err == nil {
			return nil, NewCreateWishError("INSCRIBE_ERROR", fmt.Sprintf("Inscribe API error: %s", errStrResp.Error), "")
		}

		return nil, NewCreateWishError("INSCRIBE_ERROR", fmt.Sprintf("Inscribe API error (%d)", resp.StatusCode), "")
	}

	var successResp struct {
		Success bool `json:"success"`
		Data    any  `json:"data"`
		Error   any  `json:"error"` // Changed from string to any to handle both formats
	}
	if err := json.Unmarshal(body, &successResp); err != nil {
		return nil, NewInternalError("create_wish", "Failed to parse inscribe API response")
	}

	if !successResp.Success {
		// Handle error as both string and object
		var errorMsg string
		if errorStr, ok := successResp.Error.(string); ok {
			errorMsg = errorStr
		} else if errorObj, ok := successResp.Error.(map[string]interface{}); ok {
			if msg, ok := errorObj["message"].(string); ok {
				errorMsg = msg
			} else {
				errorMsg = fmt.Sprintf("%v", errorObj)
			}
		} else {
			errorMsg = fmt.Sprintf("Unknown error: %v", successResp.Error)
		}
		return nil, NewCreateWishError("INSCRIBE_FAILED", errorMsg, "")
	}

	return successResp.Data, nil
}

func (h *HTTPMCPServer) handleSubmitWork(ctx context.Context, args map[string]interface{}, apiKey string) (interface{}, error) {
	validation := NewValidationError("submit_work", "Invalid request parameters")

	claimID, ok := args["claim_id"].(string)
	if !ok || claimID == "" {
		validation.AddFieldError("claim_id", args["claim_id"], "claim_id is required and must be a string", true)
	}

	deliverables, ok := args["deliverables"].(map[string]interface{})
	if !ok || deliverables == nil {
		validation.AddFieldError("deliverables", args["deliverables"], "deliverables is required and must be an object", true)
	} else {
		// Validate deliverables structure
		if _, ok := deliverables["notes"].(string); !ok {
			validation.AddFieldError("deliverables.notes", deliverables["notes"], "deliverables must contain a 'notes' field with description of completed work", true)
		}
	}

	// Return validation errors if any
	if validation.HasErrors() {
		return nil, validation
	}

	submission, err := h.store.SubmitWork(claimID, deliverables, nil)
	if err != nil {
		// Convert common errors to structured errors
		if strings.Contains(err.Error(), "not found") {
			return nil, NewNotFoundError("submit_work", "claim", claimID)
		}
		if strings.Contains(err.Error(), "already submitted") {
			return nil, NewSubmitWorkError("ALREADY_SUBMITTED", "Work has already been submitted for this claim", "claim_id")
		}
		return nil, NewInternalError("submit_work", fmt.Sprintf("Failed to submit work: %v", err))
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

// handleGetAuthChallenge issues a nonce for wallet verification (no auth required)
func (h *HTTPMCPServer) handleGetAuthChallenge(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	validation := NewValidationError("get_auth_challenge", "Invalid request parameters")

	wallet, ok := args["wallet_address"].(string)
	if !ok || strings.TrimSpace(wallet) == "" {
		validation.AddFieldError("wallet_address", args["wallet_address"], "wallet_address is required and must be a non-empty string", true)
	}

	// Return validation errors if any
	if validation.HasErrors() {
		return nil, validation
	}

	if h.challengeStore == nil {
		return nil, NewServiceUnavailableError("get_auth_challenge", "challenge store")
	}

	challenge, err := h.challengeStore.Issue(strings.TrimSpace(wallet))
	if err != nil {
		return nil, NewInternalError("get_auth_challenge", fmt.Sprintf("Failed to issue challenge: %v", err))
	}

	return map[string]interface{}{
		"nonce":      challenge.Nonce,
		"expires_at": challenge.ExpiresAt,
		"wallet":     challenge.Wallet,
	}, nil
}

// handleVerifyAuthChallenge checks signature against nonce and issues an API key (no auth required)
func (h *HTTPMCPServer) handleVerifyAuthChallenge(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	validation := NewValidationError("verify_auth_challenge", "Invalid request parameters")

	wallet, ok := args["wallet_address"].(string)
	if !ok || strings.TrimSpace(wallet) == "" {
		validation.AddFieldError("wallet_address", args["wallet_address"], "wallet_address is required and must be a non-empty string", true)
	}

	signature, ok := args["signature"].(string)
	if !ok || strings.TrimSpace(signature) == "" {
		validation.AddFieldError("signature", args["signature"], "signature is required and must be a non-empty string", true)
	}

	email, _ := args["email"].(string) // Optional

	// Return validation errors if any
	if validation.HasErrors() {
		return nil, validation
	}

	if h.challengeStore == nil {
		return nil, NewServiceUnavailableError("verify_auth_challenge", "challenge store")
	}

	// Verify the Bitcoin signature
	verifier := func(ch auth.Challenge, sig string) bool {
		// Import the signature verification logic from auth_handler
		ok, err := h.verifyBTCSignature(ch.Wallet, sig, strings.TrimSpace(ch.Nonce))
		if err != nil {
			return false
		}
		return ok
	}

	if !h.challengeStore.Verify(strings.TrimSpace(wallet), strings.TrimSpace(signature), verifier) {
		return nil, NewValidationError("verify_auth_challenge", "Invalid signature")
	}

	// Issue API key using the existing auth system (we need access to the issuer)
	// For now, we'll call the existing API endpoint
	reqBody := map[string]interface{}{
		"wallet_address": strings.TrimSpace(wallet),
		"signature":      strings.TrimSpace(signature),
	}
	if email != "" {
		reqBody["email"] = email
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, NewInternalError("verify_auth_challenge", "Failed to marshal verification request")
	}

	verifyURL := h.baseURL + "/api/auth/verify"
	req, err := http.NewRequestWithContext(ctx, "POST", verifyURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, NewInternalError("verify_auth_challenge", "Failed to create verification request")
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return nil, NewServiceUnavailableError("verify_auth_challenge", "verification API")
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, NewInternalError("verify_auth_challenge", "Failed to read verification response")
	}

	if resp.StatusCode >= 400 {
		var errResp struct {
			Success bool   `json:"success"`
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(body, &errResp); err == nil {
			return nil, NewValidationError("verify_auth_challenge", fmt.Sprintf("Verification failed: %s - %s", errResp.Error, errResp.Message))
		}
		return nil, NewValidationError("verify_auth_challenge", fmt.Sprintf("Verification failed (%d)", resp.StatusCode))
	}

	var successResp struct {
		Success  bool   `json:"success"`
		APIKey   string `json:"api_key"`
		Wallet   string `json:"wallet"`
		Email    string `json:"email"`
		Verified bool   `json:"verified"`
	}
	if err := json.Unmarshal(body, &successResp); err != nil {
		return nil, NewInternalError("verify_auth_challenge", "Failed to parse verification response")
	}

	if !successResp.Success {
		return nil, NewValidationError("verify_auth_challenge", "Verification unsuccessful")
	}

	return map[string]interface{}{
		"api_key":  successResp.APIKey,
		"wallet":   successResp.Wallet,
		"email":    successResp.Email,
		"verified": successResp.Verified,
	}, nil
}

// verifyBTCSignature verifies a Bitcoin signature against a message
// This is a simplified version of the logic from auth_handler.go
func (h *HTTPMCPServer) verifyBTCSignature(address, signature, message string) (bool, error) {
	// For now, we'll delegate to the existing API by calling the verify endpoint
	// This is a temporary implementation - in a real scenario, we'd import the verification logic
	reqBody := map[string]interface{}{
		"wallet_address": address,
		"signature":      signature,
	}

	jsonBody, _ := json.Marshal(reqBody)
	req, _ := http.NewRequest("POST", h.baseURL+"/api/auth/verify", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	return resp.StatusCode < 400, nil
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
