package mcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"stargate-backend/bitcoin"
	"stargate-backend/core"
	"stargate-backend/core/smart_contract"
	"stargate-backend/handlers"
	scmiddleware "stargate-backend/middleware/smart_contract"
	"stargate-backend/services"
	"stargate-backend/starlight"
	auth "stargate-backend/storage/auth"
	scstore "stargate-backend/storage/smart_contract"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
)

const (
	// maxDeliverablesSize is the maximum size allowed for the deliverables JSON (10MB)
	maxDeliverablesSize = 10 * 1024 * 1024
	// maxArtifactSize is the maximum size allowed for a single artifact (50MB)
	maxArtifactSize = 50 * 1024 * 1024
)

// HTTPMCPServer provides HTTP endpoints for MCP tools
type HTTPMCPServer struct {
	store            scmiddleware.Store
	apiKeyStore      auth.APIKeyValidator
	apiKeyIssuer     auth.APIKeyIssuer
	ingestionSvc     *services.IngestionService
	scannerManager   *starlight.ScannerManager
	smartContractSvc *services.SmartContractService
	bitcoinClient    *bitcoin.BitcoinNodeClient
	server           *scmiddleware.Server
	httpClient       *http.Client
	baseURL          string
	proxyBase        string
	rateLimiter      map[string][]time.Time
	challengeStore   *auth.ChallengeStore
	network          string
}

// NewHTTPMCPServer creates a new HTTP MCP server
func NewHTTPMCPServer(store scmiddleware.Store, apiKeyStore auth.APIKeyValidator, apiKeyIssuer auth.APIKeyIssuer, ingestionSvc *services.IngestionService, scannerManager *starlight.ScannerManager, smartContractSvc *services.SmartContractService, challengeStore *auth.ChallengeStore) *HTTPMCPServer {
	network := "mainnet"
	if strings.Contains(os.Getenv("BITCOIN_NETWORK"), "testnet") {
		network = "testnet4"
	}
	config := bitcoin.GetNetworkConfig(network)

	baseURL := os.Getenv("STARGATE_API_URL")
	if baseURL == "" {
		baseURL = "http://localhost:3001"
	}
	baseURL = strings.TrimSuffix(baseURL, "/")

	return &HTTPMCPServer{
		store:            store,
		apiKeyStore:      apiKeyStore,
		apiKeyIssuer:     apiKeyIssuer,
		ingestionSvc:     ingestionSvc,
		scannerManager:   scannerManager,
		smartContractSvc: smartContractSvc,
		bitcoinClient:    bitcoin.NewBitcoinNodeClient(config.BaseURL),
		server:           nil,
		httpClient:       &http.Client{Timeout: 30 * time.Second},
		baseURL:          baseURL,
		proxyBase:        os.Getenv("STARGATE_PROXY_BASE"),
		rateLimiter:      make(map[string][]time.Time),
		challengeStore:   challengeStore,
		network:          network,
	}
}

// SetServer sets the smart_contract server reference
func (h *HTTPMCPServer) SetServer(server *scmiddleware.Server) {
	h.server = server
}

func (h *HTTPMCPServer) externalBaseURL(r *http.Request) string {
	if r == nil || r.Host == "" {
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
		host = r.Header.Get("X-Original-Host")
	}
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
		"create_task":           true,
		"claim_task":            true,
		"submit_work":           true,
		"approve_proposal":      true,
		"reject_submission":     true,
		"approve_submission":    true,
		"get_auth_challenge":    false, // No auth required - discovery tool
		"verify_auth_challenge": false, // No auth required - solves chicken-egg problem
		"validate_address":      false, // No auth required - AI debugging tool
		"get_task":              false, // No auth required - discovery tool
		"list_submissions":      false, // No auth required - discovery tool
		"build_psbt":            true,  // Auth required - payer address derived from API key
	}
	return authenticatedTools[toolName]
}

func (h *HTTPMCPServer) callToolDirect(ctx context.Context, toolName string, args map[string]interface{}, apiKey string, r *http.Request) (interface{}, error) {
	switch toolName {
	case "list_contracts":
		return h.handleListContracts(ctx, args)
	case "get_open_contracts":
		return h.handleGetOpenContracts(ctx, args)
	case "list_proposals":
		return h.handleListProposals(ctx, args)
	case "list_tasks":
		return h.handleListTasks(ctx, args)
	case "list_submissions":
		return h.handleListSubmissions(ctx, args)
	case "get_contract":
		return h.handleGetContract(ctx, args)
	case "get_contract_rework_requests":
		return h.handleGetContractReworkRequests(ctx, args)
	case "create_contract_rework_request":
		return h.handleCreateContractReworkRequest(ctx, args, apiKey)
	case "get_task":
		return h.handleGetTask(ctx, args)
	case "list_events":
		return h.handleListEvents(ctx, args)
	case "events_stream":
		return h.handleEventsStream(ctx, args, r)
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
	case "reject_submission":
		return h.handleRejectSubmission(ctx, args, apiKey)
	case "approve_submission":
		return h.handleApproveSubmission(ctx, args, apiKey)
	case "scan_image":
		return h.handleScanImage(ctx, args)
	case "scan_transaction":
		return h.handleScanTransaction(ctx, args)
	case "get_scanner_info":
		return h.handleGetScannerInfo(ctx, args)
	case "get_auth_challenge":
		return h.handleGetAuthChallenge(ctx, args)
	case "verify_auth_challenge":
		return h.handleVerifyAuthChallenge(ctx, args)
	case "validate_address":
		return h.handleValidateAddress(ctx, args)
	case "create_task":
		return h.handleCreateTask(ctx, args, apiKey)
	case "build_psbt":
		return h.handleBuildPSBT(ctx, args, apiKey)
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
	if aiIdentifier, ok := args["ai_identifier"].(string); ok {
		filter.AiIdentifier = aiIdentifier
	}
	if skills, ok := args["skills"].([]interface{}); ok {
		for _, skill := range skills {
			if skillStr, ok := skill.(string); ok {
				filter.Skills = append(filter.Skills, skillStr)
			}
		}
	}

	// Handle pagination parameters
	if limit, ok := args["limit"].(int); ok && limit > 0 {
		filter.Limit = limit
	} else if limitFloat, ok := args["limit"].(float64); ok && limitFloat > 0 {
		filter.Limit = int(limitFloat)
	} else {
		filter.Limit = 50 // Default limit
	}

	if offset, ok := args["offset"].(int); ok && offset >= 0 {
		filter.Offset = offset
	} else if offsetFloat, ok := args["offset"].(float64); ok && offsetFloat >= 0 {
		filter.Offset = int(offsetFloat)
	} else {
		filter.Offset = 0 // Default offset
	}

	contracts, err := h.store.ListContracts(filter)
	if err != nil {
		return nil, err
	}

	// Check if there are more results by requesting one more item
	hasMore := false
	if len(contracts) == filter.Limit {
		checkFilter := filter
		checkFilter.Offset = filter.Offset + filter.Limit
		checkFilter.Limit = 1
		moreResults, err := h.store.ListContracts(checkFilter)
		if err == nil && len(moreResults) > 0 {
			hasMore = true
		}
	}

	return map[string]interface{}{
		"contracts": contracts,
		"total":     len(contracts),
		"limit":     filter.Limit,
		"offset":    filter.Offset,
		"has_more":  hasMore,
	}, nil
}

func (h *HTTPMCPServer) handleListProposals(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	filter := smart_contract.ProposalFilter{}
	if status, ok := args["status"].(string); ok {
		filter.Status = status
	}
	if contractID, ok := args["contract_id"].(string); ok {
		filter.ContractID = contractID
	}
	if proposalID, ok := args["proposal_id"].(string); ok {
		filter.ProposalID = proposalID
	}

	// Handle pagination parameters
	if limit, ok := args["limit"].(int); ok && limit > 0 {
		filter.MaxResults = limit
	} else if limitFloat, ok := args["limit"].(float64); ok && limitFloat > 0 {
		filter.MaxResults = int(limitFloat)
	} else {
		filter.MaxResults = 50 // Default limit
	}

	if offset, ok := args["offset"].(int); ok && offset >= 0 {
		filter.Offset = offset
	} else if offsetFloat, ok := args["offset"].(float64); ok && offsetFloat >= 0 {
		filter.Offset = int(offsetFloat)
	} else {
		filter.Offset = 0 // Default offset
	}

	proposals, err := h.store.ListProposals(ctx, filter)
	if err != nil {
		return nil, err
	}

	// Check if there are more results by requesting one more item
	hasMore := false
	if len(proposals) == filter.MaxResults {
		checkFilter := filter
		checkFilter.Offset = filter.Offset + filter.MaxResults
		checkFilter.MaxResults = 1
		moreResults, err := h.store.ListProposals(ctx, checkFilter)
		if err == nil && len(moreResults) > 0 {
			hasMore = true
		}
	}

	return map[string]interface{}{
		"proposals": proposals,
		"total":     len(proposals),
		"limit":     filter.MaxResults,
		"offset":    filter.Offset,
		"has_more":  hasMore,
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

	// Get creator wallet from API key
	var creatorWallet string
	if apiKeyRec, ok := h.apiKeyStore.Get(apiKey); ok {
		creatorWallet = strings.TrimSpace(apiKeyRec.Wallet)
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
			"creator_wallet":     creatorWallet,
			"contract_id":        contractID,
			"visible_pixel_hash": visiblePixelHash,
		},
	}

	log.Printf("MCP CREATE PROPOSAL DEBUG: ID=%s, metadata=%+v", proposal.ID, proposal.Metadata)
	err = h.store.CreateProposal(ctx, proposal)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "maximum of 5 proposals reached") {
			return nil, NewCreateProposalError("LIMIT_REACHED", "Maximum of 5 proposals reached for this wish to prevent spam", "visible_pixel_hash")
		}
		if strings.Contains(errMsg, "already approved/published") {
			return nil, NewCreateProposalError("ALREADY_FINALIZED", "This wish already has an approved or published proposal and is no longer accepting new proposals", "visible_pixel_hash")
		}
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

func (h *HTTPMCPServer) handleRejectSubmission(ctx context.Context, args map[string]interface{}, apiKey string) (interface{}, error) {
	validation := NewValidationError("reject_submission", "Invalid request parameters")

	submissionID, ok := args["submission_id"].(string)
	if !ok || submissionID == "" {
		validation.AddFieldError("submission_id", args["submission_id"], "submission_id is required and must be a string", true)
	}

	notes, _ := args["notes"].(string)
	rejectionType, _ := args["rejection_type"].(string)

	if validation.HasErrors() {
		return nil, validation
	}

	submission, err := h.store.GetSubmission(ctx, submissionID)
	if err != nil {
		return nil, NewNotFoundError("reject_submission", "submission", submissionID)
	}

	if submission.SubmissionID == "" {
		return nil, NewNotFoundError("reject_submission", "submission", submissionID)
	}

	err = h.store.UpdateSubmissionStatus(ctx, submissionID, "rejected", notes, rejectionType)
	if err != nil {
		return nil, NewInternalError("reject_submission", fmt.Sprintf("Failed to reject submission: %v", err))
	}

	return map[string]interface{}{
		"message":        "submission rejected",
		"submission_id":  submissionID,
		"rejection_type": rejectionType,
	}, nil
}

func (h *HTTPMCPServer) handleApproveSubmission(ctx context.Context, args map[string]interface{}, apiKey string) (interface{}, error) {
	validation := NewValidationError("approve_submission", "Invalid request parameters")

	submissionID, ok := args["submission_id"].(string)
	if !ok || submissionID == "" {
		validation.AddFieldError("submission_id", args["submission_id"], "submission_id is required and must be a string", true)
	}

	if validation.HasErrors() {
		return nil, validation
	}

	submission, err := h.store.GetSubmission(ctx, submissionID)
	if err != nil {
		return nil, NewNotFoundError("approve_submission", "submission", submissionID)
	}

	if submission.SubmissionID == "" {
		return nil, NewNotFoundError("approve_submission", "submission", submissionID)
	}

	err = h.store.UpdateSubmissionStatus(ctx, submissionID, "approved", "", "")
	if err != nil {
		return nil, NewInternalError("approve_submission", fmt.Sprintf("Failed to approve submission: %v", err))
	}

	return map[string]interface{}{
		"message":       "submission approved",
		"submission_id": submissionID,
	}, nil
}

func (h *HTTPMCPServer) requireAuthorizedApprover(apiKey string, proposal smart_contract.Proposal) error {
	// Get approver's wallet from API key
	var approverWallet string
	if h.apiKeyStore != nil {
		if approverRec, ok := h.apiKeyStore.Get(apiKey); ok {
			approverWallet = strings.TrimSpace(approverRec.Wallet)
		}
	}
	if approverWallet == "" {
		return fmt.Errorf("api key with wallet binding required for approval")
	}

	// 0. GLOBAL AUDITOR: Check if the bound wallet is the donation address
	donationAddr := strings.TrimSpace(os.Getenv("STARLIGHT_DONATION_ADDRESS"))
	if donationAddr != "" && strings.EqualFold(approverWallet, donationAddr) {
		log.Printf("AUTHORIZATION: Allowing approval for proposal %s based on Global Auditor status (%s)", proposal.ID, approverWallet)
		return nil
	}

	// 1. Check if matches Wish Creator by wallet
	visibleHash := strings.TrimSpace(proposal.VisiblePixelHash)
	if visibleHash == "" {
		if v, ok := proposal.Metadata["visible_pixel_hash"].(string); ok {
			visibleHash = strings.TrimSpace(v)
		}
	}

	// 1. Check if matches Wish Creator by wallet (from ingestion record)
	if visibleHash != "" && h.ingestionSvc != nil {
		// Try both hash and wish-hash
		rec, err := h.ingestionSvc.Get(visibleHash)
		if err != nil {
			rec, _ = h.ingestionSvc.Get("wish-" + visibleHash)
		}

		if rec != nil && rec.Metadata != nil {
			if wishCreatorWallet, ok := rec.Metadata["creator_wallet"].(string); ok {
				if strings.EqualFold(strings.TrimSpace(wishCreatorWallet), approverWallet) {
					return nil
				}
			}
		}
	}

	// 2. Fallback: Check proposal metadata for wish creator info
	if proposal.Metadata != nil {
		if creatorWallet, ok := proposal.Metadata["creator_wallet"].(string); ok {
			if strings.EqualFold(strings.TrimSpace(creatorWallet), approverWallet) {
				return nil
			}
		}
	}

	// 3. If no wish creator info exists at all, allow for now to prevent deadlock on old data
	hasWishCreatorInfo := false
	if visibleHash != "" && h.ingestionSvc != nil {
		rec, _ := h.ingestionSvc.Get(visibleHash)
		if rec != nil && rec.Metadata != nil {
			if _, ok := rec.Metadata["creator_wallet"].(string); ok {
				hasWishCreatorInfo = true
			}
		}
	}
	if !hasWishCreatorInfo && proposal.Metadata != nil {
		if _, ok := proposal.Metadata["creator_wallet"].(string); ok {
			hasWishCreatorInfo = true
		}
	}

	if !hasWishCreatorInfo {
		log.Printf("WARNING: allowing approval for proposal %s with NO wish creator info", proposal.ID)
		return nil
	}

	return fmt.Errorf("approver wallet %s does not match wish creator", approverWallet)
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

func (h *HTTPMCPServer) handleScanTransaction(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	if h.bitcoinClient == nil {
		return nil, NewServiceUnavailableError("scan_transaction", "bitcoin client")
	}

	if h.scannerManager == nil {
		return nil, NewServiceUnavailableError("scan_transaction", "scanner")
	}

	txID, ok := args["transaction_id"].(string)
	if !ok || txID == "" {
		return nil, NewValidationError("scan_transaction", "transaction_id is required and must be a string")
	}

	if len(txID) != 64 {
		return nil, NewValidationError("scan_transaction", "transaction_id must be 64 characters")
	}

	txInfo, err := h.bitcoinClient.GetTransactionInfo(txID, false, "")
	if err != nil {
		return nil, fmt.Errorf("transaction not found: %w", err)
	}

	blockHeight := txInfo.BlockHeight

	baseDir := os.Getenv("BLOCKS_DIR")
	if baseDir == "" {
		baseDir = "blocks"
	}

	// First try old block directory structure: BLOCKS_DIR/{blockHeight}_*/
	blockDirPattern := filepath.Join(baseDir, fmt.Sprintf("%d_*", blockHeight))
	matches, err := filepath.Glob(blockDirPattern)
	if err != nil || len(matches) == 0 {
		legacyDir := filepath.Join(baseDir, fmt.Sprintf("%d_00000000", blockHeight))
		if _, err := os.Stat(legacyDir); err == nil {
			matches = []string{legacyDir}
		}
	}

	var imagePath string
	if len(matches) > 0 {
		blockDir := matches[0]
		inscriptionsPath := filepath.Join(blockDir, "inscriptions.json")

		inscriptionsData, err := os.ReadFile(inscriptionsPath)
		if err == nil {
			var inscriptionsDoc struct {
				Images []struct {
					TxID     string `json:"tx_id"`
					FileName string `json:"file_name"`
					FilePath string `json:"file_path"`
					Format   string `json:"format"`
				} `json:"images"`
				SmartContracts []struct {
					ContractID string `json:"contract_id"`
					ImagePath  string `json:"image_path"`
					Metadata   struct {
						TxID        string `json:"tx_id"`
						FundingTxID string `json:"funding_txid"`
						ImageFile   string `json:"image_file"`
					} `json:"metadata"`
				} `json:"smart_contracts"`
			}

			if err := json.Unmarshal(inscriptionsData, &inscriptionsDoc); err == nil {
				var imageFile string
				for _, img := range inscriptionsDoc.Images {
					if strings.HasPrefix(img.TxID, txID) {
						imageFile = img.FileName
						break
					}
				}

				if imageFile == "" {
					for _, sc := range inscriptionsDoc.SmartContracts {
						if strings.HasPrefix(sc.Metadata.TxID, txID) || strings.HasPrefix(sc.Metadata.FundingTxID, txID) {
							imageFile = sc.Metadata.ImageFile
							if imageFile == "" && sc.ImagePath != "" {
								imageFile = filepath.Base(sc.ImagePath)
							}
							break
						}
					}
				}

				if imageFile != "" {
					imagePath = filepath.Join(blockDir, "images", imageFile)
				}
			}
		}
	}

	// If block directory lookup failed, try direct file lookup: BLOCKS_DIR/[tx_id].png
	if imagePath == "" {
		imagePath = filepath.Join(baseDir, fmt.Sprintf("%s.png", txID))

		// Try without extension first (in case the recent commits removed it)
		if _, err := os.Stat(imagePath); os.IsNotExist(err) {
			imagePath = filepath.Join(baseDir, txID)
			if _, err := os.Stat(imagePath); os.IsNotExist(err) {
				// Try common image extensions
				for _, ext := range []string{".png", ".jpg", ".jpeg", ".gif", ".webp"} {
					testPath := filepath.Join(baseDir, fmt.Sprintf("%s%s", txID, ext))
					if _, err := os.Stat(testPath); err == nil {
						imagePath = testPath
						break
					}
				}
			}
		}
	}

	// If still not found after both lookup methods
	if imagePath == "" {
		return map[string]interface{}{
			"transaction_id": txID,
			"block_height":   blockHeight,
			"status":         "file_not_found",
			"message":        fmt.Sprintf("Transaction image file not found in %s (tried block directories and direct lookup)", baseDir),
		}, nil
	}

	if _, err := os.Stat(imagePath); os.IsNotExist(err) {
		return map[string]interface{}{
			"transaction_id": txID,
			"block_height":   blockHeight,
			"status":         "file_not_found",
			"message":        fmt.Sprintf("Transaction image file not found in %s (tried block directories and direct lookup)", baseDir),
		}, nil
	}
	imageData, err := os.ReadFile(imagePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read image file: %w", err)
	}

	scanResult, err := h.scannerManager.ScanImage(imageData, core.ScanOptions{
		ExtractMessage:      true,
		ConfidenceThreshold: 0.5,
		IncludeMetadata:     true,
	})

	result := map[string]interface{}{
		"transaction_id": txID,
		"block_height":   blockHeight,
		"image_path":     imagePath,
		"image_file":     filepath.Base(imagePath),
		"image_size":     len(imageData),
		"is_stego":       false,
		"confidence":     0.0,
		"prediction":     "no_stego",
	}

	if err != nil {
		log.Printf("Failed to scan image: %v", err)
		result["scan_error"] = err.Error()
	} else {
		result["is_stego"] = scanResult.IsStego
		result["confidence"] = scanResult.Confidence
		result["prediction"] = scanResult.Prediction
		result["stego_type"] = scanResult.StegoType
		result["extraction_error"] = scanResult.ExtractionError

		if scanResult.IsStego && scanResult.ExtractedMessage != "" {
			result["extracted_message"] = scanResult.ExtractedMessage
			result["skill"] = scanResult.ExtractedMessage
			result["context"] = scanResult.ExtractedMessage
		}
	}

	return result, nil
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
	if skills, ok := args["skills"].([]interface{}); ok {
		for _, skill := range skills {
			if skillStr, ok := skill.(string); ok {
				filter.Skills = append(filter.Skills, skillStr)
			}
		}
	}

	// Handle pagination parameters
	if limit, ok := args["limit"].(int); ok && limit > 0 {
		filter.Limit = limit
	} else if limitFloat, ok := args["limit"].(float64); ok && limitFloat > 0 {
		filter.Limit = int(limitFloat)
	} else {
		filter.Limit = 50 // Default limit
	}

	if offset, ok := args["offset"].(int); ok && offset >= 0 {
		filter.Offset = offset
	} else if offsetFloat, ok := args["offset"].(float64); ok && offsetFloat >= 0 {
		filter.Offset = int(offsetFloat)
	} else {
		filter.Offset = 0 // Default offset
	}

	tasks, err := h.store.ListTasks(filter)
	if err != nil {
		return nil, err
	}

	// Check if there are more results by requesting one more item
	hasMore := false
	if len(tasks) == filter.Limit {
		checkFilter := filter
		checkFilter.Offset = filter.Offset + filter.Limit
		checkFilter.Limit = 1
		moreResults, err := h.store.ListTasks(checkFilter)
		if err == nil && len(moreResults) > 0 {
			hasMore = true
		}
	}

	return map[string]interface{}{
		"tasks":    tasks,
		"total":    len(tasks),
		"limit":    filter.Limit,
		"offset":   filter.Offset,
		"has_more": hasMore,
	}, nil
}

func (h *HTTPMCPServer) handleListSubmissions(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	var taskIDs []string

	// If task_id is provided, use it directly
	if taskID, ok := args["task_id"].(string); ok && taskID != "" {
		taskIDs = []string{taskID}
	} else if contractID, ok := args["contract_id"].(string); ok && contractID != "" {
		// Normalize contract ID - try multiple variations like other handlers
		normalized := strings.TrimSpace(contractID)
		prefixes := []string{"wish-", "proposal-", "contract-"}
		for _, prefix := range prefixes {
			if strings.HasPrefix(normalized, prefix) {
				normalized = strings.TrimPrefix(normalized, prefix)
				break
			}
		}

		// Try different contract ID variations
		contractIDsToTry := []string{contractID, normalized}
		if !strings.HasPrefix(contractID, "wish-") && !strings.HasPrefix(contractID, "proposal-") && !strings.HasPrefix(contractID, "contract-") {
			contractIDsToTry = append(contractIDsToTry, "wish-"+normalized, "contract-"+normalized)
		}

		// Try each contract ID variation until we find tasks
		for _, cid := range contractIDsToTry {
			tasks, err := h.store.ListTasks(smart_contract.TaskFilter{
				ContractID: cid,
				Limit:      1000,
			})
			if err != nil {
				return nil, fmt.Errorf("failed to get tasks for contract: %v", err)
			}
			for _, task := range tasks {
				taskIDs = append(taskIDs, task.TaskID)
			}
			if len(taskIDs) > 0 {
				break // Found tasks, stop trying
			}
		}
	} else {
		// No filter provided - return empty with a hint
		return map[string]interface{}{
			"submissions": []smart_contract.Submission{},
			"total":       0,
			"limit":       50,
			"offset":      0,
			"has_more":    false,
			"hint":        "Provide contract_id or task_id to filter submissions",
		}, nil
	}

	// Handle pagination parameters
	limit := 50
	offset := 0
	if lim, ok := args["limit"].(int); ok && lim > 0 {
		limit = lim
	} else if lim, ok := args["limit"].(float64); ok && lim > 0 {
		limit = int(lim)
	}

	if off, ok := args["offset"].(int); ok && off >= 0 {
		offset = off
	} else if off, ok := args["offset"].(float64); ok && off >= 0 {
		offset = int(off)
	}

	// Get submissions
	submissions, err := h.store.ListSubmissions(ctx, taskIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to list submissions: %v", err)
	}

	// Filter by status if provided
	statusFilter := ""
	if status, ok := args["status"].(string); ok && status != "" {
		statusFilter = strings.ToLower(status)
	}

	var filtered []smart_contract.Submission
	for _, sub := range submissions {
		if statusFilter != "" && strings.ToLower(sub.Status) != statusFilter {
			continue
		}
		filtered = append(filtered, sub)
	}

	// Apply pagination
	hasMore := false
	var paged []smart_contract.Submission
	if offset < len(filtered) {
		end := offset + limit
		if end > len(filtered) {
			end = len(filtered)
		}
		paged = filtered[offset:end]
		if offset+limit < len(filtered) {
			hasMore = true
		}
	} else {
		paged = []smart_contract.Submission{}
	}

	return map[string]interface{}{
		"submissions": paged,
		"total":       len(filtered),
		"limit":       limit,
		"offset":      offset,
		"has_more":    hasMore,
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
		// Try to find by normalized ID or with wish- prefix
		normalized := strings.TrimSpace(contractID)
		prefixes := []string{"wish-", "proposal-", "task-"}
		for _, prefix := range prefixes {
			if strings.HasPrefix(normalized, prefix) {
				normalized = strings.TrimPrefix(normalized, prefix)
				break
			}
		}

		// Try normalized if different
		if normalized != contractID {
			contract, err = h.store.GetContract(normalized)
		}

		// Try with wish- prefix if still not found
		if err != nil {
			wishID := "wish-" + normalized
			if wishID != contractID {
				contract, err = h.store.GetContract(wishID)
			}
		}

		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				return nil, NewNotFoundError("get_contract", "contract", contractID)
			}
			return nil, NewInternalError("get_contract", fmt.Sprintf("Failed to get contract: %v", err))
		}
	}

	return contract, nil
}

func (h *HTTPMCPServer) handleGetContractReworkRequests(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	validation := NewValidationError("get_contract_rework_requests", "Invalid request parameters")

	contractID, ok := args["contract_id"].(string)
	if !ok || contractID == "" {
		validation.AddFieldError("contract_id", args["contract_id"], "contract_id is required and must be a string", true)
	}

	if validation.HasErrors() {
		return nil, validation
	}

	reworkReqs, err := h.store.GetContractReworkRequests(ctx, contractID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return nil, NewNotFoundError("get_contract_rework_requests", "contract", contractID)
		}
		return nil, NewInternalError("get_contract_rework_requests", fmt.Sprintf("Failed to get rework requests: %v", err))
	}

	return map[string]interface{}{
		"rework_requests": reworkReqs,
	}, nil
}

func (h *HTTPMCPServer) handleCreateContractReworkRequest(ctx context.Context, args map[string]interface{}, apiKey string) (interface{}, error) {
	validation := NewValidationError("create_contract_rework_request", "Invalid request parameters")

	contractID, ok := args["contract_id"].(string)
	if !ok || contractID == "" {
		validation.AddFieldError("contract_id", args["contract_id"], "contract_id is required and must be a string", true)
	}

	notes, ok := args["notes"].(string)
	if !ok || strings.TrimSpace(notes) == "" {
		validation.AddFieldError("notes", args["notes"], "notes are required and must be a string", true)
	}

	if validation.HasErrors() {
		return nil, validation
	}

	if apiKey == "" {
		return nil, NewUnauthorizedError("create_contract_rework_request", "API key required to create rework request")
	}

	var requester string
	if h.apiKeyStore != nil {
		if keyInfo, ok := h.apiKeyStore.Get(apiKey); ok {
			requester = strings.TrimSpace(keyInfo.Wallet)
		}
	}
	if requester == "" {
		return nil, NewUnauthorizedError("create_contract_rework_request", "API key must have an associated wallet address")
	}

	reworkReq, err := h.store.CreateContractReworkRequest(ctx, contractID, requester, notes)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return nil, NewNotFoundError("create_contract_rework_request", "contract", contractID)
		}
		return nil, NewInternalError("create_contract_rework_request", fmt.Sprintf("Failed to create rework request: %v", err))
	}

	return reworkReq, nil
}

func (h *HTTPMCPServer) handleGetTask(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	validation := NewValidationError("get_task", "Invalid request parameters")

	taskID, ok := args["task_id"].(string)
	if !ok || taskID == "" {
		validation.AddFieldError("task_id", args["task_id"], "task_id is required and must be a string", true)
	}

	// Return validation errors if any
	if validation.HasErrors() {
		return nil, validation
	}

	// Fetch task from store
	task, err := h.store.GetTask(taskID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return nil, NewNotFoundError("get_task", "task", taskID)
		}
		return nil, NewInternalError("get_task", fmt.Sprintf("Failed to get task: %v", err))
	}

	return task, nil
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

func (h *HTTPMCPServer) handleEventsStream(ctx context.Context, args map[string]interface{}, r *http.Request) (interface{}, error) {
	baseURL := h.externalBaseURL(r)
	return map[string]interface{}{
		"stream_url": baseURL + "/mcp/events",
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

		// Validate and process artifacts if present
		if artifacts, exists := deliverables["artifacts"]; exists {
			artifactsList, ok := artifacts.([]interface{})
			if !ok {
				validation.AddFieldError("deliverables.artifacts", artifacts, "artifacts must be an array", true)
			} else {
				for i, artifact := range artifactsList {
					artifactMap, ok := artifact.(map[string]interface{})
					if !ok {
						validation.AddFieldError(fmt.Sprintf("deliverables.artifacts[%d]", i), artifact, "each artifact must be an object", true)
						continue
					}

					// Validate filename
					if filename, ok := artifactMap["filename"].(string); !ok || filename == "" {
						validation.AddFieldError(fmt.Sprintf("deliverables.artifacts[%d].filename", i), artifactMap["filename"], "artifact filename is required and must be a string", true)
					}

					// Validate content
					if content, ok := artifactMap["content"].(string); !ok || content == "" {
						validation.AddFieldError(fmt.Sprintf("deliverables.artifacts[%d].content", i), artifactMap["content"], "artifact content is required and must be a base64-encoded string", true)
					}
				}
			}
		}
	}

	// Return validation errors if any
	if validation.HasErrors() {
		return nil, validation
	}

	// Process file artifacts if present
	var processedArtifacts []map[string]interface{}
	if artifacts, exists := deliverables["artifacts"]; exists {
		artifactsList, ok := artifacts.([]interface{})
		if ok {
			// Get uploads directory
			uploadsDir := os.Getenv("UPLOADS_DIR")
			if uploadsDir == "" {
				uploadsDir = "/data/uploads"
			}

			// Create results directory: UPLOADS_DIR/results/[contract_id]
			// Look up the contract/task relationship to get contract_id for file organization
			var resultsDir string
			subDir := claimID // Fallback to claim ID
			if claim, err := h.store.GetClaim(claimID); err == nil {
				if task, err := h.store.GetTask(claim.TaskID); err == nil {
					// For wish contracts, the contract_id IS the visible_pixel_hash
					// This allows organizing all work for a contract/wish under one directory
					// Normalize to remove prefixes like "wish-" to ensure consistent path
					subDir = scstore.NormalizeContractID(task.ContractID)
				}
			}
			resultsDir = filepath.Join(uploadsDir, "results", subDir)

			if err := os.MkdirAll(resultsDir, 0755); err != nil {
				return nil, NewInternalError("submit_work", fmt.Sprintf("Failed to create results directory: %v", err))
			}

			for i, artifact := range artifactsList {
				artifactMap, ok := artifact.(map[string]interface{})
				if !ok {
					continue // Skip invalid artifacts (already validated above)
				}

				filename, _ := artifactMap["filename"].(string)
				content, _ := artifactMap["content"].(string)
				contentType, _ := artifactMap["content_type"].(string)

				// Decode base64 content
				fileData, err := base64.StdEncoding.DecodeString(content)
				if err != nil {
					return nil, NewInternalError("submit_work", fmt.Sprintf("Failed to decode artifact %d (%s): %v", i, filename, err))
				}

				// Enforce artifact size limit
				if len(fileData) > maxArtifactSize {
					return nil, NewSubmitWorkError("FILE_TOO_LARGE", fmt.Sprintf("Artifact '%s' exceeds maximum size of %d MB", filename, maxArtifactSize/(1024*1024)), "deliverables.artifacts")
				}

				// Create secure file path - preserve directory structure safely
				// Sanitize the filename to prevent directory traversal
				// We strip all absolute path indicators and ".." components
				filename = filepath.Clean(filename)
				if filepath.IsAbs(filename) {
					// Convert absolute path to relative path components
					if vol := filepath.VolumeName(filename); vol != "" {
						filename = strings.TrimPrefix(filename, vol)
					}
					filename = strings.TrimPrefix(filename, string(filepath.Separator))
				}

				// Rebuild path component by component to ensure no escapes
				parts := strings.Split(filename, string(filepath.Separator))
				var safeParts []string
				for _, part := range parts {
					if part == ".." || part == "." || part == "" {
						continue
					}
					// Sanitize each component to be a safe filename
					safePart := strings.ReplaceAll(part, "/", "_")
					safePart = strings.ReplaceAll(safePart, "\\", "_")
					safePart = strings.ReplaceAll(safePart, "..", "")
					if safePart != "" {
						safeParts = append(safeParts, safePart)
					}
				}

				if len(safeParts) == 0 {
					safeParts = []string{fmt.Sprintf("artifact_%d", i)}
				}

				safeFilename := filepath.Join(safeParts...)
				dir := filepath.Dir(safeFilename)
				baseName := filepath.Base(safeFilename)

				subDirPath := resultsDir
				if dir != "." && dir != "" {
					subDirPath = filepath.Join(resultsDir, dir)
				}

				if err := os.MkdirAll(subDirPath, 0755); err != nil {
					return nil, NewInternalError("submit_work", fmt.Sprintf("Failed to create artifact subdirectory: %v", err))
				}

				filePath := filepath.Join(subDirPath, baseName)
				fmt.Printf("DEBUG: Writing file to: %s\n", filePath)

				// Write file
				if err := os.WriteFile(filePath, fileData, 0644); err != nil {
					return nil, NewInternalError("submit_work", fmt.Sprintf("Failed to write artifact %d (%s): %v", i, filename, err))
				}

				// Detect content type if not provided
				if contentType == "" {
					contentType = http.DetectContentType(fileData)
				}

				// Create file info for response - preserve relative path
				relativePath := safeFilename
				fileInfo := map[string]interface{}{
					"filename":      relativePath,
					"original_name": filename,
					"size":          len(fileData),
					"content_type":  contentType,
					"path":          fmt.Sprintf("/uploads/results/%s/%s", subDir, relativePath),
				}
				processedArtifacts = append(processedArtifacts, fileInfo)

				// CRITICAL: Remove content from artifactMap to prevent it from being stored in JSONB
				delete(artifactMap, "content")
			}

			// Update deliverables with processed artifact info
			deliverables["artifacts_uploaded"] = processedArtifacts
		}
	}

	// Final safeguard: Check the size of deliverables JSON before storing
	delivJSON, err := json.Marshal(deliverables)
	if err == nil && len(delivJSON) > maxDeliverablesSize {
		return nil, NewSubmitWorkError("DATA_TOO_LARGE", fmt.Sprintf("Total deliverables data size (%d bytes) exceeds limit of %d bytes", len(delivJSON), maxDeliverablesSize), "deliverables")
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

	result := map[string]interface{}{
		"message":    "work submitted successfully",
		"claim_id":   claimID,
		"submission": submission,
	}

	// Include artifact information in response
	if len(processedArtifacts) > 0 {
		result["artifacts"] = processedArtifacts
		result["artifacts_count"] = len(processedArtifacts)
	}

	return result, nil
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

	// Check for AI-friendly mode flag
	aiMode, _ := args["ai_mode"].(bool)

	// Return validation errors if any
	if validation.HasErrors() {
		return nil, validation
	}

	if h.challengeStore == nil {
		return nil, NewServiceUnavailableError("get_auth_challenge", "challenge store")
	}

	var challenge auth.Challenge
	var err error

	// Use AI-friendly settings if requested
	if aiMode {
		challenge, err = h.challengeStore.IssueAIChallenge(strings.TrimSpace(wallet))
	} else {
		challenge, err = h.challengeStore.Issue(strings.TrimSpace(wallet))
	}

	if err != nil {
		return nil, NewInternalError("get_auth_challenge", fmt.Sprintf("Failed to issue challenge: %v", err))
	}

	return map[string]interface{}{
		"nonce":            challenge.Nonce,
		"expires_at":       challenge.ExpiresAt,
		"wallet":           challenge.Wallet,
		"max_attempts":     challenge.MaxAttempts,
		"ai_friendly_mode": aiMode,
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

	email, _ := args["email"].(string)         // Optional
	detailedMode, _ := args["detailed"].(bool) // Optional - enable detailed error reporting

	// Return validation errors if any
	if validation.HasErrors() {
		return nil, validation
	}

	if h.challengeStore == nil {
		return nil, NewServiceUnavailableError("verify_auth_challenge", "challenge store")
	}

	// Enhanced verifier that provides detailed results
	var sigResult handlers.SignatureVerificationResult
	verifier := func(ch auth.Challenge, sig string) bool {
		sigResult = handlers.VerifyBTCSignatureWithDetails(ch.Wallet, sig, strings.TrimSpace(ch.Nonce))
		if !sigResult.Success {
			return false
		}
		return true
	}

	// Use detailed verification for better error handling
	var verifyResult auth.VerificationResult
	if detailedMode {
		verifyResult = h.challengeStore.VerifyWithDetails(strings.TrimSpace(wallet), strings.TrimSpace(signature), verifier)
	} else {
		success := h.challengeStore.Verify(strings.TrimSpace(wallet), strings.TrimSpace(signature), verifier)
		verifyResult = auth.VerificationResult{
			Success: success,
			Reason:  "Basic verification completed",
		}
	}

	if !verifyResult.Success {
		// Provide detailed error information when requested
		if detailedMode {
			// For detailed mode, return a structured error response with verification details
			return map[string]interface{}{
				"verified": false,
				"reason":   verifyResult.Reason,
				"details": map[string]interface{}{
					"remaining_attempts": verifyResult.RemainingAttempts,
					"signature_info": map[string]interface{}{
						"success": sigResult.Success,
						"format":  sigResult.Format,
						"message": sigResult.Message,
					},
				},
			}, nil
		} else {
			return nil, NewValidationError("verify_auth_challenge", "Invalid signature")
		}
	}

	// Issue API key directly using the issuer
	if h.apiKeyIssuer == nil {
		return nil, NewServiceUnavailableError("verify_auth_challenge", "API key issuer")
	}

	apiKeyRec, err := h.apiKeyIssuer.Issue(email, strings.TrimSpace(wallet), "wallet-verify")
	if err != nil {
		return nil, NewInternalError("verify_auth_challenge", fmt.Sprintf("Failed to issue API key: %v", err))
	}

	return map[string]interface{}{
		"api_key":  apiKeyRec.Key,
		"wallet":   apiKeyRec.Wallet,
		"email":    apiKeyRec.Email,
		"verified": true,
	}, nil
}

// handleValidateAddress validates and provides detailed information about a Bitcoin address (no auth required)
func (h *HTTPMCPServer) handleValidateAddress(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	validation := NewValidationError("validate_address", "Invalid request parameters")

	address, ok := args["address"].(string)
	if !ok || strings.TrimSpace(address) == "" {
		validation.AddFieldError("address", args["address"], "address is required and must be a non-empty string", true)
	}

	// Return validation errors if any
	if validation.HasErrors() {
		return nil, validation
	}

	// Use the enhanced address detection from handlers package
	info := handlers.DetectAddressInfo(strings.TrimSpace(address))

	return map[string]interface{}{
		"address":      info.Address,
		"is_valid":     info.IsValid,
		"address_type": info.AddressType,
		"network":      info.Network,
		"error":        info.Error,
	}, nil
}

// verifyBTCSignature verifies a Bitcoin signature against a message
// Uses the exported verification functions from the handlers package
func (h *HTTPMCPServer) verifyBTCSignature(address, signature, message string) (bool, error) {
	return handlers.VerifyBTCSignature(address, signature, message)
}

func (h *HTTPMCPServer) handleCreateTask(ctx context.Context, args map[string]interface{}, apiKey string) (interface{}, error) {
	validation := NewValidationError("create_task", "Invalid request parameters")

	// Validate required fields
	contractID, ok := args["contract_id"].(string)
	if !ok || strings.TrimSpace(contractID) == "" {
		validation.AddFieldError("contract_id", args["contract_id"], "contract_id is required and must be a non-empty string", true)
	}

	title, ok := args["title"].(string)
	if !ok || strings.TrimSpace(title) == "" {
		validation.AddFieldError("title", args["title"], "title is required and must be a non-empty string", true)
	}

	description, ok := args["description"].(string)
	if !ok || strings.TrimSpace(description) == "" {
		validation.AddFieldError("description", args["description"], "description is required and must be a non-empty string", true)
	}

	// Validate budget_sats
	var budgetSats int64 = 0
	if budget, ok := args["budget_sats"]; ok {
		if b, ok := budget.(float64); ok {
			if b <= 0 {
				validation.AddFieldError("budget_sats", budget, "budget_sats must be a positive number", true)
			} else {
				budgetSats = int64(b)
			}
		} else {
			validation.AddTypeError("budget_sats", budget, "number")
		}
	} else {
		validation.AddFieldError("budget_sats", nil, "budget_sats is required and must be a positive number", true)
	}

	// Validate optional fields
	var skills []string
	if skillsRaw, ok := args["skills"].([]interface{}); ok {
		for _, skill := range skillsRaw {
			if skillStr, ok := skill.(string); ok && strings.TrimSpace(skillStr) != "" {
				skills = append(skills, strings.TrimSpace(skillStr))
			}
		}
	}

	difficulty, _ := args["difficulty"].(string)
	if difficulty != "" {
		validDifficulties := map[string]bool{"easy": true, "medium": true, "hard": true}
		if !validDifficulties[difficulty] {
			validation.AddFieldError("difficulty", difficulty, "difficulty must be one of: easy, medium, hard", false)
		}
	}

	var estimatedHours int = 0
	if estHours, ok := args["estimated_hours"].(float64); ok {
		if estHours < 0 {
			validation.AddFieldError("estimated_hours", estHours, "estimated_hours must be a non-negative number", false)
		} else {
			estimatedHours = int(estHours)
		}
	} else if estHours, ok := args["estimated_hours"].(int); ok {
		if estHours < 0 {
			validation.AddFieldError("estimated_hours", estHours, "estimated_hours must be a non-negative integer", false)
		} else {
			estimatedHours = estHours
		}
	}

	var requirements map[string]string
	if reqRaw, ok := args["requirements"].(map[string]interface{}); ok {
		requirements = make(map[string]string)
		for key, value := range reqRaw {
			if valueStr, ok := value.(string); ok {
				requirements[key] = valueStr
			}
		}
	}

	// Return validation errors if any
	if validation.HasErrors() {
		return nil, validation
	}

	if h.store == nil {
		return nil, NewServiceUnavailableError("create_task", "task store")
	}

	// Verify contract exists
	_, err := h.store.GetContract(contractID)
	if err != nil {
		return nil, NewValidationError("create_task", fmt.Sprintf("Contract not found: %s", contractID))
	}

	// Create the task
	taskID := fmt.Sprintf("%s-task-%d", contractID, time.Now().Unix())

	task := smart_contract.Task{
		TaskID:         taskID,
		ContractID:     contractID,
		GoalID:         fmt.Sprintf("%s-goal-1", contractID), // Default goal ID
		Title:          strings.TrimSpace(title),
		Description:    strings.TrimSpace(description),
		BudgetSats:     budgetSats,
		Skills:         skills,
		Status:         "available", // Default status
		Difficulty:     difficulty,
		EstimatedHours: estimatedHours,
		Requirements:   requirements,
	}

	// Upsert the task
	err = h.store.UpsertTask(ctx, task)
	if err != nil {
		return nil, NewInternalError("create_task", fmt.Sprintf("Failed to create task: %v", err))
	}

	return map[string]interface{}{
		"task_id":         task.TaskID,
		"contract_id":     task.ContractID,
		"title":           task.Title,
		"description":     task.Description,
		"budget_sats":     task.BudgetSats,
		"skills":          task.Skills,
		"status":          task.Status,
		"difficulty":      task.Difficulty,
		"estimated_hours": task.EstimatedHours,
		"requirements":    task.Requirements,
		"created_at":      time.Now().Format(time.RFC3339),
	}, nil
}

func (h *HTTPMCPServer) handleBuildPSBT(ctx context.Context, args map[string]interface{}, apiKey string) (interface{}, error) {
	if h.bitcoinClient == nil {
		return nil, NewServiceUnavailableError("build_psbt", "bitcoin client")
	}

	if apiKey == "" {
		return nil, NewUnauthorizedError("build_psbt", "API key required - payer address is derived from your authenticated wallet")
	}

	var payerAddressStr string
	if h.apiKeyStore != nil {
		if keyInfo, ok := h.apiKeyStore.Get(apiKey); ok {
			payerAddressStr = strings.TrimSpace(keyInfo.Wallet)
		}
	}
	if payerAddressStr == "" {
		return nil, NewUnauthorizedError("build_psbt", "API key must have an associated wallet address")
	}

	validation := NewValidationError("build_psbt", "Invalid request parameters")

	pixelHash, ok := args["pixel_hash"].(string)
	if !ok || strings.TrimSpace(pixelHash) == "" {
		validation.AddFieldError("pixel_hash", args["pixel_hash"], "pixel_hash is required and must be a non-empty string", true)
	}

	if validation.HasErrors() {
		return nil, validation
	}

	normalizedHash := strings.TrimSpace(pixelHash)
	contractID := "wish-" + normalizedHash

	_, err := h.store.GetContract(contractID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return nil, NewNotFoundError("build_psbt", "contract", contractID)
		}
		return nil, NewInternalError("build_psbt", fmt.Sprintf("Failed to get contract: %v", err))
	}

	params := h.chainParams()
	origMempoolBase := os.Getenv("MEMPOOL_API_BASE")
	if netConfig := bitcoin.GetNetworkConfig(h.network); netConfig.BaseURL != "" {
		os.Setenv("MEMPOOL_API_BASE", netConfig.BaseURL)
	}
	mempoolClient := bitcoin.NewMempoolClient()
	if origMempoolBase != "" {
		os.Setenv("MEMPOOL_API_BASE", origMempoolBase)
	} else {
		os.Unsetenv("MEMPOOL_API_BASE")
	}

	payerAddress, err := btcutil.DecodeAddress(payerAddressStr, params)
	if err != nil {
		return nil, NewValidationError("build_psbt", fmt.Sprintf("invalid payer address: %v", err))
	}

	tasks, err := h.store.ListTasks(smart_contract.TaskFilter{
		ContractID: contractID,
		Limit:      1000,
	})
	if err != nil {
		return nil, NewInternalError("build_psbt", fmt.Sprintf("Failed to list tasks: %v", err))
	}

	var payouts []bitcoin.PayoutOutput
	for _, task := range tasks {
		if task.Status != "approved" {
			continue
		}
		wallet := strings.TrimSpace(task.ContractorWallet)
		if wallet == "" && task.MerkleProof != nil {
			wallet = strings.TrimSpace(task.MerkleProof.ContractorWallet)
		}
		if wallet == "" {
			continue
		}
		addr, err := btcutil.DecodeAddress(wallet, params)
		if err != nil {
			continue
		}
		payouts = append(payouts, bitcoin.PayoutOutput{
			Address:   addr,
			ValueSats: task.BudgetSats,
		})
	}

	if len(payouts) == 0 {
		return nil, NewValidationError("build_psbt", "no approved tasks with contractor wallets found")
	}

	feeRate := int64(10)
	if fr, ok := args["fee_rate_sat_per_vb"].(float64); ok && fr > 0 {
		feeRate = int64(fr)
	}

	commitmentSats := int64(0)
	if cs, ok := args["commitment_sats"].(float64); ok && cs > 0 {
		commitmentSats = int64(cs)
	}

	var pixelHashBytes []byte
	if commitmentSats > 0 {
		pixelHashBytes, err = hex.DecodeString(normalizedHash)
		if err != nil {
			validation.AddFieldError("pixel_hash", normalizedHash, "must be a valid hex string", false)
		} else if len(pixelHashBytes) != 32 {
			validation.AddFieldError("pixel_hash", normalizedHash, "must be exactly 32 bytes (64 hex characters)", false)
		}
		if validation.HasErrors() {
			return nil, validation
		}
	}

	req := bitcoin.PSBTRequest{
		PayerAddress:      payerAddress,
		Payouts:           payouts,
		FeeRateSatPerVB:   feeRate,
		ChangeAddress:     payerAddress,
		PixelHash:         pixelHashBytes,
		CommitmentSats:    commitmentSats,
		CommitmentAddress: payerAddress,
	}

	result, err := bitcoin.BuildFundingPSBT(mempoolClient, params, req)
	if err != nil {
		return nil, NewInternalError("build_psbt", fmt.Sprintf("Failed to build PSBT: %v", err))
	}

	return map[string]interface{}{
		"psbt_base64":        result.EncodedBase64,
		"psbt_hex":           result.EncodedHex,
		"fee_sats":           result.FeeSats,
		"change_sats":        result.ChangeSats,
		"change_addresses":   result.ChangeAddresses,
		"change_amounts":     result.ChangeAmounts,
		"selected_sats":      result.SelectedSats,
		"payout_amounts":     result.PayoutAmounts,
		"commitment_sats":    result.CommitmentSats,
		"commitment_address": result.CommitmentAddr,
		"funding_txid":       result.FundingTxID,
		"contract_id":        contractID,
		"payout_count":       len(payouts),
	}, nil
}

func (h *HTTPMCPServer) chainParams() *chaincfg.Params {
	switch h.network {
	case "mainnet":
		return &chaincfg.MainNetParams
	case "testnet":
		return &chaincfg.TestNet3Params
	case "testnet4":
		return &chaincfg.TestNet4Params
	case "signet":
		return &chaincfg.SigNetParams
	default:
		return &chaincfg.TestNet4Params
	}
}

// RegisterRoutes registers HTTP MCP endpoints
func (h *HTTPMCPServer) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/mcp/tools", h.handleListTools)   // No auth - allows discovery
	mux.HandleFunc("/mcp/search", h.handleToolSearch) // No auth - search tools
	mux.HandleFunc("/mcp/call", h.handleToolCall)     // Tool-level auth for specific tools
	mux.HandleFunc("/mcp/discover", h.handleDiscover) // No auth - allows discovery
	mux.HandleFunc("/mcp/docs", h.handleDocs)         // No auth required for documentation
	mux.HandleFunc("/mcp/SKILL.md", h.handleSkill)    // No auth required for canonical agent workflow
	mux.HandleFunc("/mcp/starlight_sdk.sh", h.handleSDKScript)
	mux.HandleFunc("/mcp/openapi.json", h.handleOpenAPI) // No auth required for API spec
	mux.HandleFunc("/mcp/health", h.handleHealth)
	mux.HandleFunc("/mcp/events", h.handleEventsProxy)
	mux.HandleFunc("/mcp", h.handleIndex)
	// Register catch-all last
	mux.HandleFunc("/mcp/", h.handleIndex)
}
