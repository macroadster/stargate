package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"stargate-backend/core"
	"stargate-backend/core/smart_contract"
	scmiddleware "stargate-backend/middleware/smart_contract"
	"stargate-backend/models"
	"stargate-backend/services"
	"stargate-backend/starlight"
	scstore "stargate-backend/storage/smart_contract"
)

// HTTPMCPServer provides HTTP endpoints for MCP tools
type HTTPMCPServer struct {
	store            scmiddleware.Store
	apiKey           string
	ingestionSvc     *services.IngestionService
	scannerManager   *starlight.ScannerManager
	smartContractSvc *services.SmartContractService
	httpClient       *http.Client
	baseURL          string
}

// NewHTTPMCPServer creates a new HTTP MCP server
func NewHTTPMCPServer(store scmiddleware.Store, apiKey string, ingestionSvc *services.IngestionService, scannerManager *starlight.ScannerManager, smartContractSvc *services.SmartContractService) *HTTPMCPServer {
	return &HTTPMCPServer{
		store:            store,
		apiKey:           apiKey,
		ingestionSvc:     ingestionSvc,
		scannerManager:   scannerManager,
		smartContractSvc: smartContractSvc,
		httpClient:       &http.Client{Timeout: 10 * time.Second},
		baseURL:          "http://localhost:3001", // Default backend URL
	}
}

// MCPRequest represents an incoming MCP tool call via HTTP
type MCPRequest struct {
	Tool      string                 `json:"tool"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
}

// MCPResponse represents response from an MCP tool call
type MCPResponse struct {
	Success bool        `json:"success"`
	Result  interface{} `json:"result,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// RegisterRoutes registers HTTP MCP endpoints
func (h *HTTPMCPServer) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/mcp/tools", h.authWrap(h.handleListTools))
	mux.HandleFunc("/mcp/call", h.authWrap(h.handleToolCall))
	// Register catch-all last
	mux.HandleFunc("/mcp/", h.authWrap(h.handleToolCall))
}

func (h *HTTPMCPServer) authWrap(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Printf("DEBUG: authWrap called for path: %s", r.URL.Path)
		// Check API key if configured
		if h.apiKey != "" {
			key := r.Header.Get("X-API-Key")
			if key == "" {
				http.Error(w, "Missing API key", http.StatusUnauthorized)
				return
			}
			if key != h.apiKey {
				http.Error(w, "Invalid API key", http.StatusForbidden)
				return
			}
		}
		log.Printf("DEBUG: authWrap passed, calling next handler")
		next(w, r)
	}
}

func (h *HTTPMCPServer) handleListTools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Create a simple list of available tools
	tools := map[string]interface{}{
		"contracts": []string{
			"list_contracts",
			"get_contract",
			"get_contract_funding",
			"get_open_contracts",
		},
		"tasks": []string{
			"list_tasks",
			"get_task",
			"claim_task",
			"submit_work",
			"get_task_proof",
			"get_task_status",
		},
		"skills": []string{
			"list_skills",
		},
		"proposals": []string{
			"list_proposals",
			"get_proposal",
			"create_proposal",
			"approve_proposal",
			"publish_proposal",
		},
		"submissions": []string{
			"list_submissions",
			"get_submission",
			"review_submission",
			"rework_submission",
		},
		"events": []string{
			"list_events",
		},
		"scanning": []string{
			"scan_image",
			"scan_block",
			"extract_message",
			"get_scanner_info",
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"tools": tools,
		"total": 23, // Total number of tools available
	})
}

func (h *HTTPMCPServer) handleToolCall(w http.ResponseWriter, r *http.Request) {
	log.Printf("DEBUG: HTTP MCP handleToolCall called with URL: %s, method: %s", r.URL.Path, r.Method)
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req MCPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.sendError(w, "Invalid JSON: "+err.Error())
		return
	}

	log.Printf("DEBUG: Tool requested: '%s'", req.Tool)
	if req.Tool == "" {
		h.sendError(w, "Tool name is required")
		return
	}

	// Call the appropriate tool handler directly
	ctx := context.Background()
	result, err := h.callToolDirect(ctx, req.Tool, req.Arguments)
	if err != nil {
		h.sendError(w, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(MCPResponse{
		Success: true,
		Result:  result,
	})
}

func (h *HTTPMCPServer) callToolDirect(ctx context.Context, toolName string, args map[string]interface{}) (interface{}, error) {
	store := h.store

	// Debug: log the tool name
	log.Printf("DEBUG: callToolDirect called with tool: '%s' (len=%d)", toolName, len(toolName))

	switch toolName {
	case "list_contracts":
		status := ""
		if s, ok := args["status"].(string); ok {
			status = s
		}
		var skills []string
		if skillSlice, ok := args["skills"].([]interface{}); ok {
			for _, skill := range skillSlice {
				if skillStr, ok := skill.(string); ok {
					skills = append(skills, skillStr)
				}
			}
		}
		contracts, err := store.ListContracts(status, skills)
		if err != nil {
			return nil, fmt.Errorf("Failed to list contracts: %v", err)
		}
		return map[string]interface{}{
			"contracts":   contracts,
			"total_count": len(contracts),
		}, nil

	case "get_contract":
		contractID, ok := args["contract_id"].(string)
		if !ok {
			return nil, fmt.Errorf("contract_id is required")
		}
		contract, err := store.GetContract(contractID)
		if err != nil {
			return nil, fmt.Errorf("Failed to get contract: %v", err)
		}
		return contract, nil

	case "get_open_contracts":
		// Make HTTP request to /api/open-contracts
		resp, err := h.httpClient.Get(h.baseURL + "/api/open-contracts")
		if err != nil {
			return nil, fmt.Errorf("Failed to fetch open contracts: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
		}

		var apiResponse map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&apiResponse); err != nil {
			return nil, fmt.Errorf("Failed to decode API response: %v", err)
		}

		if success, ok := apiResponse["success"].(bool); !ok || !success {
			return nil, fmt.Errorf("API request failed")
		}

		data, ok := apiResponse["data"].(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("Invalid API response format")
		}

		results, _ := data["results"]
		total, _ := data["total"].(float64) // JSON numbers are float64

		return map[string]interface{}{
			"contracts": results,
			"total":     int(total),
		}, nil

	case "get_contract_funding":
		contractID, ok := args["contract_id"].(string)
		if !ok {
			return nil, fmt.Errorf("contract_id is required")
		}
		contract, proofs, err := store.ContractFunding(contractID)
		if err != nil {
			return nil, fmt.Errorf("Failed to get contract funding: %v", err)
		}
		return map[string]interface{}{
			"contract": contract,
			"proofs":   proofs,
		}, nil

	case "list_tasks":
		var skills []string
		if skillSlice, ok := args["skills"].([]interface{}); ok {
			for _, skill := range skillSlice {
				if skillStr, ok := skill.(string); ok {
					skills = append(skills, skillStr)
				}
			}
		}

		filter := smart_contract.TaskFilter{
			Skills:        skills,
			MaxDifficulty: h.toString(args["max_difficulty"]),
			Status:        h.toString(args["status"]),
			Limit:         int(h.toInt64(args["limit"])),
			Offset:        int(h.toInt64(args["offset"])),
			MinBudgetSats: h.toInt64(args["min_budget_sats"]),
			ContractID:    h.toString(args["contract_id"]),
			ClaimedBy:     h.toString(args["claimed_by"]),
		}

		if filter.Limit == 0 {
			filter.Limit = 50
		}

		tasks, err := store.ListTasks(filter)
		if err != nil {
			return nil, fmt.Errorf("Failed to list tasks: %v", err)
		}

		// Get submissions for these tasks
		var taskIDs []string
		for _, t := range tasks {
			taskIDs = append(taskIDs, t.TaskID)
		}
		subs, _ := store.ListSubmissions(ctx, taskIDs)

		return map[string]interface{}{
			"tasks":         tasks,
			"total_matches": len(tasks),
			"submissions":   subs,
		}, nil

	case "get_task":
		taskID, ok := args["task_id"].(string)
		if !ok {
			return nil, fmt.Errorf("task_id is required")
		}
		task, err := store.GetTask(taskID)
		if err != nil {
			return nil, fmt.Errorf("Failed to get task: %v", err)
		}
		return task, nil

	case "claim_task":
		taskID, ok := args["task_id"].(string)
		if !ok {
			return nil, fmt.Errorf("task_id is required")
		}
		aiIdentifier, ok := args["ai_identifier"].(string)
		if !ok {
			return nil, fmt.Errorf("ai_identifier is required")
		}

		claim, err := store.ClaimTask(taskID, aiIdentifier, nil)
		if err != nil {
			return nil, fmt.Errorf("Failed to claim task: %v", err)
		}

		return map[string]interface{}{
			"success":    true,
			"claim_id":   claim.ClaimID,
			"expires_at": claim.ExpiresAt,
			"message":    "Task reserved. Submit work before expiration.",
		}, nil

	case "submit_work":
		claimID, ok := args["claim_id"].(string)
		if !ok {
			return nil, fmt.Errorf("claim_id is required")
		}

		deliverables := h.toMap(args["deliverables"])
		completionProof := h.toMap(args["completion_proof"])

		if deliverables == nil {
			return nil, fmt.Errorf("deliverables are required")
		}

		sub, err := store.SubmitWork(claimID, deliverables, completionProof)
		if err != nil {
			return nil, fmt.Errorf("Failed to submit work: %v", err)
		}

		return sub, nil

	case "get_task_proof":
		taskID, ok := args["task_id"].(string)
		if !ok {
			return nil, fmt.Errorf("task_id is required")
		}

		proof, err := store.GetTaskProof(taskID)
		if err != nil {
			return nil, fmt.Errorf("Failed to get task proof: %v", err)
		}

		return proof, nil

	case "get_task_status":
		taskID, ok := args["task_id"].(string)
		if !ok {
			return nil, fmt.Errorf("task_id is required")
		}

		status, err := store.TaskStatus(taskID)
		if err != nil {
			return nil, fmt.Errorf("Failed to get task status: %v", err)
		}

		return status, nil

	case "list_skills":
		tasks, err := store.ListTasks(smart_contract.TaskFilter{})
		if err != nil {
			return nil, fmt.Errorf("Failed to list tasks: %v", err)
		}

		skillSet := make(map[string]struct{})
		// Add default skills
		skillSet["contract_bidding"] = struct{}{}
		skillSet["get_open_contracts"] = struct{}{}

		for _, t := range tasks {
			for _, skill := range t.Skills {
				key := h.toString(skill)
				if key == "" {
					continue
				}
				skillSet[key] = struct{}{}
			}
		}
		skills := make([]string, 0, len(skillSet))
		for k := range skillSet {
			skills = append(skills, k)
		}

		return map[string]interface{}{
			"skills": skills,
			"count":  len(skills),
		}, nil

	case "list_proposals":
		var skills []string
		if skillSlice, ok := args["skills"].([]interface{}); ok {
			for _, skill := range skillSlice {
				if skillStr, ok := skill.(string); ok {
					skills = append(skills, skillStr)
				}
			}
		}

		filter := smart_contract.ProposalFilter{
			Status:     h.toString(args["status"]),
			Skills:     skills,
			MinBudget:  h.toInt64(args["min_budget_sats"]),
			ContractID: h.toString(args["contract_id"]),
			MaxResults: int(h.toInt64(args["limit"])),
			Offset:     int(h.toInt64(args["offset"])),
		}

		proposals, err := store.ListProposals(ctx, filter)
		if err != nil {
			return nil, fmt.Errorf("Failed to list proposals: %v", err)
		}

		// Get submissions alongside tasks
		var taskIDs []string
		for _, p := range proposals {
			for _, t := range p.Tasks {
				taskIDs = append(taskIDs, t.TaskID)
			}
		}
		subs, _ := store.ListSubmissions(ctx, taskIDs)

		return map[string]interface{}{
			"proposals":   proposals,
			"total":       len(proposals),
			"submissions": subs,
		}, nil

	case "get_proposal":
		proposalID, ok := args["proposal_id"].(string)
		if !ok {
			return nil, fmt.Errorf("proposal_id is required")
		}

		proposal, err := store.GetProposal(ctx, proposalID)
		if err != nil {
			return nil, fmt.Errorf("Failed to get proposal: %v", err)
		}

		return proposal, nil

	case "create_proposal":
		// Extract required fields
		title := h.toString(args["title"])
		if strings.TrimSpace(title) == "" {
			return nil, fmt.Errorf("title is required")
		}

		id := h.toString(args["id"])
		if id == "" {
			id = "proposal-" + strconv.FormatInt(time.Now().UnixNano(), 10)
		}

		status := h.toString(args["status"])
		if status == "" {
			status = "pending"
		}

		budgetSats := h.toInt64(args["budget_sats"])
		if budgetSats == 0 {
			budgetSats = scstore.DefaultBudgetSats()
		}

		metadata := h.toMap(args["metadata"])
		if metadata == nil {
			metadata = map[string]interface{}{}
		}

		contractID := h.toString(args["contract_id"])
		if contractID != "" {
			metadata["contract_id"] = contractID
		}

		// Check if creating from ingestion or manual with scan metadata
		ingestionID := h.toString(args["ingestion_id"])
		if ingestionID != "" && h.ingestionSvc != nil {
			// Create from ingestion record
			rec, err := h.ingestionSvc.Get(ingestionID)
			if err != nil {
				return nil, fmt.Errorf("ingestion not found")
			}

			// Build proposal from ingestion
			proposalBody := scmiddleware.ProposalCreateBody{
				ID:               id,
				IngestionID:      ingestionID,
				ContractID:       contractID,
				Title:            title,
				DescriptionMD:    h.toString(args["description_md"]),
				VisiblePixelHash: h.toString(args["visible_pixel_hash"]),
				BudgetSats:       budgetSats,
				Status:           status,
				Metadata:         metadata,
			}

			proposal, err := scmiddleware.BuildProposalFromIngestion(proposalBody, rec)
			if err != nil {
				return nil, err
			}

			if err := store.CreateProposal(ctx, proposal); err != nil {
				return nil, err
			}

			// Create SmartContractImage to witness the visible pixel hash
			if proposal.VisiblePixelHash != "" && h.smartContractSvc != nil {
				contractID := proposal.VisiblePixelHash // Use hash directly as contract ID

				createReq := models.CreateContractRequest{
					ContractID:   contractID,
					BlockHeight:  0, // Will be updated when mined
					ContractType: "steganographic",
					Metadata: map[string]interface{}{
						"visible_pixel_hash": proposal.VisiblePixelHash,
						"proposal_id":        proposal.ID,
						"ingestion_id":       rec.ID,
						"embedded_message":   rec.Metadata["embedded_message"],
						"stego_image_url":    "/uploads/" + rec.Filename,
					},
				}

				if _, err := h.smartContractSvc.CreateContract(createReq); err != nil {
					log.Printf("Warning: failed to create SmartContractImage: %v", err)
					// Don't fail the proposal creation for this
				}
			}

			result := map[string]interface{}{
				"proposal_id": proposal.ID,
				"status":      proposal.Status,
				"message":     "proposal created from pending ingestion",
			}

			return result, nil
		}

		// Manual creation - requires image scan metadata
		visiblePixelHash := h.toString(args["visible_pixel_hash"])
		hasScanMetadata := visiblePixelHash != "" || metadata["image_scan_data"] != nil

		if !hasScanMetadata {
			return nil, fmt.Errorf("proposals must include image scan metadata (visible_pixel_hash or image_scan_data in metadata)")
		}

		// Manual creation with tasks
		var tasks []smart_contract.Task
		if taskSlice, ok := args["tasks"].([]interface{}); ok {
			for i, taskInterface := range taskSlice {
				if taskMap, ok := taskInterface.(map[string]interface{}); ok {
					task := smart_contract.Task{
						TaskID:      h.toString(taskMap["task_id"]),
						ContractID:  h.toString(taskMap["contract_id"]),
						GoalID:      h.toString(taskMap["goal_id"]),
						Title:       h.toString(taskMap["title"]),
						Description: h.toString(taskMap["description"]),
						BudgetSats:  h.toInt64(taskMap["budget_sats"]),
						Status:      h.toString(taskMap["status"]),
					}

					if task.TaskID == "" {
						task.TaskID = id + "-task-" + strconv.Itoa(i+1)
					}
					if task.ContractID == "" && contractID != "" {
						task.ContractID = contractID
					}
					if task.Status == "" {
						task.Status = "available"
					}

					tasks = append(tasks, task)
				}
			}
		}

		proposal := smart_contract.Proposal{
			ID:               id,
			Title:            title,
			DescriptionMD:    h.toString(args["description_md"]),
			VisiblePixelHash: h.toString(args["visible_pixel_hash"]),
			BudgetSats:       budgetSats,
			Status:           status,
			CreatedAt:        time.Now(),
			Tasks:            tasks,
			Metadata:         metadata,
		}

		if err := store.CreateProposal(ctx, proposal); err != nil {
			return nil, err
		}

		result := map[string]interface{}{
			"proposal_id": proposal.ID,
			"status":      proposal.Status,
			"tasks":       len(proposal.Tasks),
			"budget_sats": proposal.BudgetSats,
		}

		return result, nil

	case "approve_proposal":
		proposalID, ok := args["proposal_id"].(string)
		if !ok {
			return nil, fmt.Errorf("proposal_id is required")
		}

		if err := store.ApproveProposal(ctx, proposalID); err != nil {
			return nil, fmt.Errorf("Failed to approve proposal: %v", err)
		}

		// Publish tasks for this proposal
		if err := h.publishProposalTasks(ctx, proposalID); err != nil {
			return nil, fmt.Errorf("Failed to publish tasks: %v", err)
		}

		return map[string]interface{}{
			"proposal_id": proposalID,
			"status":      "approved",
			"message":     "Proposal approved; tasks published.",
		}, nil

	case "publish_proposal":
		proposalID, ok := args["proposal_id"].(string)
		if !ok {
			return nil, fmt.Errorf("proposal_id is required")
		}

		// Check that all tasks for this proposal have approved submissions
		proposal, err := store.GetProposal(ctx, proposalID)
		if err != nil {
			return nil, fmt.Errorf("Failed to get proposal: %v", err)
		}
		if proposal.Status != "approved" {
			return nil, fmt.Errorf("proposal must be approved before publishing")
		}

		contractID := h.toString(proposal.Metadata["contract_id"])
		if contractID == "" {
			contractID = proposalID // fallback
		}

		// Get all tasks for this contract
		tasks, err := store.ListTasks(smart_contract.TaskFilter{ContractID: contractID})
		if err != nil {
			return nil, fmt.Errorf("Failed to list tasks: %v", err)
		}

		// Check each task has an approved submission
		for _, task := range tasks {
			submissions, err := store.ListSubmissions(ctx, []string{task.TaskID})
			if err != nil {
				return nil, fmt.Errorf("Failed to list submissions for task %s: %v", task.TaskID, err)
			}
			hasApproved := false
			for _, sub := range submissions {
				if strings.EqualFold(sub.Status, "approved") {
					hasApproved = true
					break
				}
			}
			if !hasApproved {
				return nil, fmt.Errorf("task %s does not have an approved submission", task.TaskID)
			}
		}

		if err := store.PublishProposal(ctx, proposalID); err != nil {
			return nil, fmt.Errorf("Failed to publish proposal: %v", err)
		}

		return map[string]interface{}{
			"proposal_id": proposalID,
			"status":      "published",
			"message":     "Proposal published.",
		}, nil

	case "list_submissions":
		return map[string]interface{}{
			"submissions": map[string]interface{}{},
			"total":       0,
			"message":     "list_submissions tool found!",
		}, nil

	case "get_submission":
		submissionID, ok := args["submission_id"].(string)
		if !ok {
			return nil, fmt.Errorf("submission_id is required")
		}

		// Get all tasks to find submission
		tasks, err := store.ListTasks(smart_contract.TaskFilter{})
		if err != nil {
			return nil, fmt.Errorf("Failed to get tasks: %v", err)
		}

		taskIDs := make([]string, len(tasks))
		for i, task := range tasks {
			taskIDs[i] = task.TaskID
		}

		submissions, err := store.ListSubmissions(ctx, taskIDs)
		if err != nil {
			return nil, fmt.Errorf("Failed to get submissions: %v", err)
		}

		for _, sub := range submissions {
			if sub.SubmissionID == submissionID {
				return sub, nil
			}
		}

		return nil, fmt.Errorf("submission not found")

	case "review_submission":
		submissionID, ok := args["submission_id"].(string)
		if !ok {
			return nil, fmt.Errorf("submission_id is required")
		}

		action, ok := args["action"].(string)
		if !ok {
			return nil, fmt.Errorf("action is required")
		}

		// Validate action
		validActions := map[string]bool{
			"review":  true,
			"approve": true,
			"reject":  true,
		}
		if !validActions[action] {
			return nil, fmt.Errorf("invalid action. must be: review, approve, or reject")
		}

		// Update submission status
		var newStatus string
		switch action {
		case "review":
			newStatus = "reviewed"
		case "approve":
			newStatus = "approved"
		case "reject":
			newStatus = "rejected"
		}

		err := store.UpdateSubmissionStatus(ctx, submissionID, newStatus)
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				return nil, fmt.Errorf("submission not found")
			}
			return nil, fmt.Errorf("Failed to update submission: %v", err)
		}

		return map[string]interface{}{
			"message":       fmt.Sprintf("submission %sd successfully", action),
			"status":        newStatus,
			"submission_id": submissionID,
		}, nil

	case "rework_submission":
		submissionID, ok := args["submission_id"].(string)
		if !ok {
			return nil, fmt.Errorf("submission_id is required")
		}

		deliverables := h.toMap(args["deliverables"])
		notes := h.toString(args["notes"])

		if deliverables == nil && notes == "" {
			return nil, fmt.Errorf("deliverables or notes must be provided")
		}

		// Get the original submission
		tasks, err := store.ListTasks(smart_contract.TaskFilter{})
		if err != nil {
			return nil, fmt.Errorf("Failed to get tasks: %v", err)
		}

		taskIDs := make([]string, len(tasks))
		for i, task := range tasks {
			taskIDs[i] = task.TaskID
		}

		submissions, err := store.ListSubmissions(ctx, taskIDs)
		if err != nil {
			return nil, fmt.Errorf("Failed to get submissions: %v", err)
		}

		var originalSubmission *smart_contract.Submission
		for _, sub := range submissions {
			if sub.SubmissionID == submissionID {
				originalSubmission = &sub
				break
			}
		}

		if originalSubmission == nil {
			return nil, fmt.Errorf("submission not found")
		}

		// Update deliverables if provided
		if deliverables != nil {
			originalSubmission.Deliverables = deliverables
		}

		// Add rework notes to deliverables
		if notes != "" {
			if originalSubmission.Deliverables == nil {
				originalSubmission.Deliverables = make(map[string]interface{})
			}
			originalSubmission.Deliverables["rework_notes"] = notes
			originalSubmission.Deliverables["reworked_at"] = time.Now().Format(time.RFC3339)
		}

		// Reset status to pending_review
		err = store.UpdateSubmissionStatus(ctx, submissionID, "pending_review")
		if err != nil {
			return nil, fmt.Errorf("Failed to update submission: %v", err)
		}

		return map[string]interface{}{
			"message":       "rework submitted successfully",
			"status":        "pending_review",
			"submission_id": submissionID,
		}, nil

	case "list_events":
		// For now, return empty events since the original server has in-memory events
		events := []smart_contract.Event{}

		return map[string]interface{}{
			"events": events,
			"total":  len(events),
		}, nil

	case "scan_image":
		imageDataStr, ok := args["image_data"].(string)
		if !ok {
			return nil, fmt.Errorf("image_data is required (base64 encoded)")
		}

		// Decode base64 image data
		imageData, err := base64.StdEncoding.DecodeString(imageDataStr)
		if err != nil {
			return nil, fmt.Errorf("invalid base64 image_data: %v", err)
		}

		if h.scannerManager == nil {
			return nil, fmt.Errorf("scanner not available")
		}

		options := core.ScanOptions{
			ExtractMessage:      h.toBool(args["extract_message"]),
			ConfidenceThreshold: h.toFloat64(args["confidence_threshold"]),
			IncludeMetadata:     h.toBool(args["include_metadata"]),
		}
		if options.ConfidenceThreshold == 0 {
			options.ConfidenceThreshold = 0.5 // default
		}

		result, err := h.scannerManager.ScanImage(imageData, options)
		if err != nil {
			return nil, fmt.Errorf("failed to scan image: %v", err)
		}

		return result, nil

	case "scan_block":
		blockHeight, ok := args["block_height"].(float64)
		if !ok {
			return nil, fmt.Errorf("block_height is required")
		}

		if h.scannerManager == nil {
			return nil, fmt.Errorf("scanner not available")
		}

		options := core.ScanOptions{
			ExtractMessage:      h.toBool(args["extract_message"]),
			ConfidenceThreshold: h.toFloat64(args["confidence_threshold"]),
			IncludeMetadata:     h.toBool(args["include_metadata"]),
		}
		if options.ConfidenceThreshold == 0 {
			options.ConfidenceThreshold = 0.5 // default
		}

		result, err := h.scannerManager.ScanBlock(int64(blockHeight), options)
		if err != nil {
			return nil, fmt.Errorf("failed to scan block: %v", err)
		}

		return result, nil

	case "extract_message":
		imageDataStr, ok := args["image_data"].(string)
		if !ok {
			return nil, fmt.Errorf("image_data is required (base64 encoded)")
		}

		method := h.toString(args["method"])
		if method == "" {
			method = "alpha" // default method
		}

		// Decode base64 image data
		imageData, err := base64.StdEncoding.DecodeString(imageDataStr)
		if err != nil {
			return nil, fmt.Errorf("invalid base64 image_data: %v", err)
		}

		if h.scannerManager == nil {
			return nil, fmt.Errorf("scanner not available")
		}

		result, err := h.scannerManager.ExtractMessage(imageData, method)
		if err != nil {
			return nil, fmt.Errorf("failed to extract message: %v", err)
		}

		return result, nil

	case "get_scanner_info":
		if h.scannerManager == nil {
			return map[string]interface{}{
				"scanner_info":   nil,
				"health_status":  nil,
				"scanner_type":   "none",
				"is_initialized": false,
				"error":          "scanner manager not configured",
			}, nil
		}

		info := h.scannerManager.GetScannerInfo()
		health := h.scannerManager.GetHealthStatus()

		return map[string]interface{}{
			"scanner_info":   info,
			"health_status":  health,
			"scanner_type":   h.scannerManager.GetScannerType(),
			"is_initialized": h.scannerManager.IsInitialized(),
		}, nil

	default:
		return map[string]interface{}{
			"message":          fmt.Sprintf("DEBUG: tool '%s' not found - function reached", toolName),
			"tool_name":        toolName,
			"tool_name_length": len(toolName),
		}, nil
	}
}

func (h *HTTPMCPServer) sendError(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(MCPResponse{
		Success: false,
		Error:   message,
	})
}

// Helper functions
func (h *HTTPMCPServer) toString(val interface{}) string {
	if val == nil {
		return ""
	}
	if str, ok := val.(string); ok {
		return str
	}
	return fmt.Sprintf("%v", val)
}

func (h *HTTPMCPServer) toInt64(val interface{}) int64 {
	if i, ok := val.(int64); ok {
		return i
	}
	if i, ok := val.(int); ok {
		return int64(i)
	}
	if f, ok := val.(float64); ok {
		return int64(f)
	}
	return 0
}

func (h *HTTPMCPServer) toBool(val interface{}) bool {
	if b, ok := val.(bool); ok {
		return b
	}
	if s, ok := val.(string); ok {
		return strings.ToLower(s) == "true"
	}
	return false
}

func (h *HTTPMCPServer) toFloat64(val interface{}) float64 {
	if f, ok := val.(float64); ok {
		return f
	}
	if i, ok := val.(int); ok {
		return float64(i)
	}
	if i, ok := val.(int64); ok {
		return float64(i)
	}
	return 0.0
}

func (h *HTTPMCPServer) toMap(val interface{}) map[string]interface{} {
	if m, ok := val.(map[string]interface{}); ok {
		return m
	}
	return nil
}

// publishProposalTasks publishes the tasks stored in a proposal into MCP tasks
func (h *HTTPMCPServer) publishProposalTasks(ctx context.Context, proposalID string) error {
	p, err := h.store.GetProposal(ctx, proposalID)
	if err != nil {
		return err
	}
	if len(p.Tasks) == 0 {
		// Try to derive tasks from metadata embedded_message
		if em, ok := p.Metadata["embedded_message"].(string); ok && em != "" {
			p.Tasks = scstore.BuildTasksFromMarkdown(p.ID, em, p.VisiblePixelHash, p.BudgetSats, scstore.FundingAddressFromMeta(p.Metadata))
		}
		if len(p.Tasks) == 0 {
			return nil
		}
	}
	// Build a contract from the proposal, then upsert tasks
	contract := smart_contract.Contract{
		ContractID:          p.ID,
		Title:               p.Title,
		TotalBudgetSats:     p.BudgetSats,
		GoalsCount:          1,
		AvailableTasksCount: len(p.Tasks),
		Status:              "active",
	}
	// Preserve hashes/funding if present
	fundingAddr := scstore.FundingAddressFromMeta(p.Metadata)
	tasks := make([]smart_contract.Task, 0, len(p.Tasks))
	for _, t := range p.Tasks {
		task := t
		if task.ContractID == "" {
			task.ContractID = p.ID
		}
		if task.MerkleProof == nil && p.VisiblePixelHash != "" {
			task.MerkleProof = &smart_contract.MerkleProof{
				VisiblePixelHash:   p.VisiblePixelHash,
				FundedAmountSats:   p.BudgetSats / int64(len(p.Tasks)),
				FundingAddress:     fundingAddr,
				ConfirmationStatus: "provisional",
			}
		}
		if task.MerkleProof != nil && task.MerkleProof.FundingAddress == "" {
			task.MerkleProof.FundingAddress = fundingAddr
		}
		tasks = append(tasks, task)
	}
	if pg, ok := h.store.(interface {
		UpsertContractWithTasks(context.Context, smart_contract.Contract, []smart_contract.Task) error
	}); ok {
		if err := pg.UpsertContractWithTasks(ctx, contract, tasks); err != nil {
			return err
		}
		return nil
	}
	return nil
}
