package mcp

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

// registerListProposalsTool creates a tool for listing proposals
func (s *MCPServer) registerListProposalsTool() {
	tool := mcp.NewTool("list_proposals",
		mcp.WithDescription("List proposals with optional filtering"),
		mcp.WithString("status", mcp.Description("Filter by proposal status")),
		mcp.WithArray("skills", mcp.Description("Filter by required skills")),
		mcp.WithNumber("min_budget_sats", mcp.Description("Minimum budget in satoshis")),
		mcp.WithString("contract_id", mcp.Description("Filter by contract ID")),
		mcp.WithNumber("limit", mcp.Description("Maximum number of proposals to return")),
		mcp.WithNumber("offset", mcp.Description("Number of proposals to skip")),
	)

	s.mcpServer.AddTool(tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.GetArguments()

		var skills []string
		if skillSlice, ok := args["skills"].([]interface{}); ok {
			for _, skill := range skillSlice {
				if skillStr, ok := skill.(string); ok {
					skills = append(skills, skillStr)
				}
			}
		}

		filter := ProposalFilter{
			Status:     toString(args["status"]),
			Skills:     skills,
			MinBudget:  toInt64(args["min_budget_sats"]),
			ContractID: toString(args["contract_id"]),
			MaxResults: int(toInt64(args["limit"])),
			Offset:     int(toInt64(args["offset"])),
		}

		proposals, err := s.store.ListProposals(ctx, filter)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to list proposals: %v", err)), nil
		}

		// Get submissions alongside tasks
		var taskIDs []string
		for _, p := range proposals {
			for _, t := range p.Tasks {
				taskIDs = append(taskIDs, t.TaskID)
			}
		}
		subs, _ := s.store.ListSubmissions(ctx, taskIDs)

		result := map[string]interface{}{
			"proposals":   proposals,
			"total":       len(proposals),
			"submissions": subs,
		}

		return mcp.NewToolResultText(fmt.Sprintf("Found %d proposals:\n\n%+v", len(proposals), result)), nil
	})
}

// registerGetProposalTool creates a tool for getting a specific proposal
func (s *MCPServer) registerGetProposalTool() {
	tool := mcp.NewTool("get_proposal",
		mcp.WithDescription("Get details of a specific proposal"),
		mcp.WithString("proposal_id", mcp.Required(), mcp.Description("ID of proposal to retrieve")),
	)

	s.mcpServer.AddTool(tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		proposalID, err := request.RequireString("proposal_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		proposal, err := s.store.GetProposal(ctx, proposalID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to get proposal: %v", err)), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("Proposal details:\n\n%+v", proposal)), nil
	})
}

// registerCreateProposalTool creates a tool for creating a new proposal
func (s *MCPServer) registerCreateProposalTool() {
	tool := mcp.NewTool("create_proposal",
		mcp.WithDescription("Create a new proposal"),
		mcp.WithString("id", mcp.Description("Unique ID for the proposal")),
		mcp.WithString("ingestion_id", mcp.Description("ID of ingestion to derive from")),
		mcp.WithString("contract_id", mcp.Description("Associated contract ID")),
		mcp.WithString("title", mcp.Description("Title of the proposal")),
		mcp.WithString("description_md", mcp.Description("Markdown description")),
		mcp.WithString("visible_pixel_hash", mcp.Description("Visible pixel hash")),
		mcp.WithNumber("budget_sats", mcp.Description("Budget in satoshis")),
		mcp.WithString("status", mcp.Description("Status of the proposal")),
		mcp.WithObject("metadata", mcp.Description("Additional metadata")),
		mcp.WithArray("tasks", mcp.Description("List of tasks for the proposal")),
	)

	s.mcpServer.AddTool(tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.GetArguments()

		// Extract required fields
		title := toString(args["title"])
		if strings.TrimSpace(title) == "" {
			return mcp.NewToolResultError("title is required"), nil
		}

		id := toString(args["id"])
		if id == "" {
			id = "proposal-" + strconv.FormatInt(time.Now().UnixNano(), 10)
		}

		status := toString(args["status"])
		if status == "" {
			status = "pending"
		}

		budgetSats := toInt64(args["budget_sats"])
		if budgetSats == 0 {
			budgetSats = defaultBudgetSats()
		}

		metadata := toMap(args["metadata"])
		if metadata == nil {
			metadata = map[string]interface{}{}
		}

		contractID := toString(args["contract_id"])
		if contractID != "" {
			metadata["contract_id"] = contractID
		}

		// Handle ingestion-based creation
		ingestionID := toString(args["ingestion_id"])
		if ingestionID != "" && s.ingestionSvc != nil {
			rec, err := s.ingestionSvc.Get(ingestionID)
			if err != nil {
				return mcp.NewToolResultError("ingestion not found"), nil
			}

			// Build proposal from ingestion
			proposalBody := proposalCreateBody{
				ID:               id,
				IngestionID:      ingestionID,
				ContractID:       contractID,
				Title:            title,
				DescriptionMD:    toString(args["description_md"]),
				VisiblePixelHash: toString(args["visible_pixel_hash"]),
				BudgetSats:       budgetSats,
				Status:           status,
				Metadata:         metadata,
			}

			proposal, err := buildProposalFromIngestion(proposalBody, rec)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			if err := s.store.CreateProposal(ctx, proposal); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			result := map[string]interface{}{
				"proposal_id": proposal.ID,
				"status":      proposal.Status,
				"message":     "proposal created from pending ingestion",
			}

			return mcp.NewToolResultText(fmt.Sprintf("Proposal created successfully:\n\n%+v", result)), nil
		}

		// Manual creation
		var tasks []Task
		if taskSlice, ok := args["tasks"].([]interface{}); ok {
			for i, taskInterface := range taskSlice {
				if taskMap, ok := taskInterface.(map[string]interface{}); ok {
					task := Task{
						TaskID:      toString(taskMap["task_id"]),
						ContractID:  toString(taskMap["contract_id"]),
						GoalID:      toString(taskMap["goal_id"]),
						Title:       toString(taskMap["title"]),
						Description: toString(taskMap["description"]),
						BudgetSats:  toInt64(taskMap["budget_sats"]),
						Status:      toString(taskMap["status"]),
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

		proposal := Proposal{
			ID:               id,
			Title:            title,
			DescriptionMD:    toString(args["description_md"]),
			VisiblePixelHash: toString(args["visible_pixel_hash"]),
			BudgetSats:       budgetSats,
			Status:           status,
			CreatedAt:        time.Now(),
			Tasks:            tasks,
			Metadata:         metadata,
		}

		if err := s.store.CreateProposal(ctx, proposal); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		result := map[string]interface{}{
			"proposal_id": proposal.ID,
			"status":      proposal.Status,
			"tasks":       len(proposal.Tasks),
			"budget_sats": proposal.BudgetSats,
		}

		return mcp.NewToolResultText(fmt.Sprintf("Proposal created successfully:\n\n%+v", result)), nil
	})
}

// registerApproveProposalTool creates a tool for approving a proposal
func (s *MCPServer) registerApproveProposalTool() {
	tool := mcp.NewTool("approve_proposal",
		mcp.WithDescription("Approve a proposal and publish its tasks"),
		mcp.WithString("proposal_id", mcp.Required(), mcp.Description("ID of proposal to approve")),
	)

	s.mcpServer.AddTool(tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		proposalID, err := request.RequireString("proposal_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		if err := s.store.ApproveProposal(ctx, proposalID); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to approve proposal: %v", err)), nil
		}

		// Publish tasks for this proposal
		if err := s.publishProposalTasks(ctx, proposalID); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to publish tasks: %v", err)), nil
		}

		result := map[string]interface{}{
			"proposal_id": proposalID,
			"status":      "approved",
			"message":     "Proposal approved; tasks published.",
		}

		return mcp.NewToolResultText(fmt.Sprintf("Proposal approved successfully:\n\n%+v", result)), nil
	})
}

// registerPublishProposalTool creates a tool for publishing a proposal
func (s *MCPServer) registerPublishProposalTool() {
	tool := mcp.NewTool("publish_proposal",
		mcp.WithDescription("Publish a proposal without approval"),
		mcp.WithString("proposal_id", mcp.Required(), mcp.Description("ID of proposal to publish")),
	)

	s.mcpServer.AddTool(tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		proposalID, err := request.RequireString("proposal_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		if err := s.store.PublishProposal(ctx, proposalID); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to publish proposal: %v", err)), nil
		}

		result := map[string]interface{}{
			"proposal_id": proposalID,
			"status":      "published",
			"message":     "Proposal published.",
		}

		return mcp.NewToolResultText(fmt.Sprintf("Proposal published successfully:\n\n%+v", result)), nil
	})
}

// registerListSubmissionsTool creates a tool for listing submissions
func (s *MCPServer) registerListSubmissionsTool() {
	tool := mcp.NewTool("list_submissions",
		mcp.WithDescription("List submissions with optional filtering"),
		mcp.WithString("contract_id", mcp.Description("Filter by contract ID")),
		mcp.WithArray("task_ids", mcp.Description("Filter by specific task IDs")),
		mcp.WithString("status", mcp.Description("Filter by submission status")),
	)

	s.mcpServer.AddTool(tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.GetArguments()

		contractID := toString(args["contract_id"])
		status := toString(args["status"])

		var taskIDs []string
		if taskSlice, ok := args["task_ids"].([]interface{}); ok {
			for _, task := range taskSlice {
				if taskStr, ok := task.(string); ok {
					taskIDs = append(taskIDs, taskStr)
				}
			}
		}

		var submissions []Submission
		var err error

		if len(taskIDs) > 0 {
			submissions, err = s.store.ListSubmissions(ctx, taskIDs)
		} else if contractID != "" {
			// Get tasks for contract, then submissions for those tasks
			tasks, err := s.store.ListTasks(TaskFilter{ContractID: contractID})
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Failed to get tasks: %v", err)), nil
			}
			taskIDs = make([]string, len(tasks))
			for i, task := range tasks {
				taskIDs[i] = task.TaskID
			}
			submissions, err = s.store.ListSubmissions(ctx, taskIDs)
		} else {
			// Get all tasks, then all submissions
			tasks, err := s.store.ListTasks(TaskFilter{})
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Failed to get tasks: %v", err)), nil
			}
			taskIDs = make([]string, len(tasks))
			for i, task := range tasks {
				taskIDs[i] = task.TaskID
			}
			submissions, err = s.store.ListSubmissions(ctx, taskIDs)
		}

		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to list submissions: %v", err)), nil
		}

		// Filter by status if provided
		if status != "" {
			filtered := make([]Submission, 0)
			for _, sub := range submissions {
				if strings.EqualFold(sub.Status, status) {
					filtered = append(filtered, sub)
				}
			}
			submissions = filtered
		}

		// Convert to map for easier consumption
		submissionMap := make(map[string]Submission)
		for _, sub := range submissions {
			submissionMap[sub.SubmissionID] = sub
		}

		result := map[string]interface{}{
			"submissions": submissionMap,
			"total":       len(submissions),
		}

		return mcp.NewToolResultText(fmt.Sprintf("Found %d submissions:\n\n%+v", len(submissions), result)), nil
	})
}

// registerGetSubmissionTool creates a tool for getting a specific submission
func (s *MCPServer) registerGetSubmissionTool() {
	tool := mcp.NewTool("get_submission",
		mcp.WithDescription("Get details of a specific submission"),
		mcp.WithString("submission_id", mcp.Required(), mcp.Description("ID of submission to retrieve")),
	)

	s.mcpServer.AddTool(tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		submissionID, err := request.RequireString("submission_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		// Get all tasks to find submission
		tasks, err := s.store.ListTasks(TaskFilter{})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to get tasks: %v", err)), nil
		}

		taskIDs := make([]string, len(tasks))
		for i, task := range tasks {
			taskIDs[i] = task.TaskID
		}

		submissions, err := s.store.ListSubmissions(ctx, taskIDs)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to get submissions: %v", err)), nil
		}

		for _, sub := range submissions {
			if sub.SubmissionID == submissionID {
				return mcp.NewToolResultText(fmt.Sprintf("Submission details:\n\n%+v", sub)), nil
			}
		}

		return mcp.NewToolResultError("submission not found"), nil
	})
}

// registerReviewSubmissionTool creates a tool for reviewing a submission
func (s *MCPServer) registerReviewSubmissionTool() {
	tool := mcp.NewTool("review_submission",
		mcp.WithDescription("Review a submission (approve, reject, or mark for review)"),
		mcp.WithString("submission_id", mcp.Required(), mcp.Description("ID of submission to review")),
		mcp.WithString("action", mcp.Required(), mcp.Description("Action: review, approve, or reject")),
		mcp.WithString("notes", mcp.Description("Review notes")),
	)

	s.mcpServer.AddTool(tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		submissionID, err := request.RequireString("submission_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		action, err := request.RequireString("action")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		// Validate action
		validActions := map[string]bool{
			"review":  true,
			"approve": true,
			"reject":  true,
		}
		if !validActions[action] {
			return mcp.NewToolResultError("invalid action. must be: review, approve, or reject"), nil
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

		err = s.store.UpdateSubmissionStatus(ctx, submissionID, newStatus)
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				return mcp.NewToolResultError("submission not found"), nil
			}
			return mcp.NewToolResultError(fmt.Sprintf("Failed to update submission: %v", err)), nil
		}

		result := map[string]interface{}{
			"message":       fmt.Sprintf("submission %sd successfully", action),
			"status":        newStatus,
			"submission_id": submissionID,
		}

		return mcp.NewToolResultText(fmt.Sprintf("Submission reviewed successfully:\n\n%+v", result)), nil
	})
}

// registerReworkSubmissionTool creates a tool for reworking a submission
func (s *MCPServer) registerReworkSubmissionTool() {
	tool := mcp.NewTool("rework_submission",
		mcp.WithDescription("Rework a submission with new deliverables or notes"),
		mcp.WithString("submission_id", mcp.Required(), mcp.Description("ID of submission to rework")),
		mcp.WithObject("deliverables", mcp.Description("Updated deliverables")),
		mcp.WithString("notes", mcp.Description("Rework notes")),
	)

	s.mcpServer.AddTool(tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.GetArguments()

		submissionID, err := request.RequireString("submission_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		deliverables := toMap(args["deliverables"])
		notes := toString(args["notes"])

		if deliverables == nil && notes == "" {
			return mcp.NewToolResultError("deliverables or notes must be provided"), nil
		}

		// Get the original submission
		tasks, err := s.store.ListTasks(TaskFilter{})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to get tasks: %v", err)), nil
		}

		taskIDs := make([]string, len(tasks))
		for i, task := range tasks {
			taskIDs[i] = task.TaskID
		}

		submissions, err := s.store.ListSubmissions(ctx, taskIDs)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to get submissions: %v", err)), nil
		}

		var originalSubmission *Submission
		for _, sub := range submissions {
			if sub.SubmissionID == submissionID {
				originalSubmission = &sub
				break
			}
		}

		if originalSubmission == nil {
			return mcp.NewToolResultError("submission not found"), nil
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
		err = s.store.UpdateSubmissionStatus(ctx, submissionID, "pending_review")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to update submission: %v", err)), nil
		}

		result := map[string]interface{}{
			"message":       "rework submitted successfully",
			"status":        "pending_review",
			"submission_id": submissionID,
		}

		return mcp.NewToolResultText(fmt.Sprintf("Submission reworked successfully:\n\n%+v", result)), nil
	})
}

// registerListEventsTool creates a tool for listing events
func (s *MCPServer) registerListEventsTool() {
	tool := mcp.NewTool("list_events",
		mcp.WithDescription("List MCP events with optional filtering"),
		mcp.WithString("type", mcp.Description("Filter by event type")),
		mcp.WithString("actor", mcp.Description("Filter by actor")),
		mcp.WithString("entity_id", mcp.Description("Filter by entity ID")),
		mcp.WithNumber("limit", mcp.Description("Maximum number of events to return")),
	)

	s.mcpServer.AddTool(tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// For now, return empty events since the original server has in-memory events
		// In a real implementation, you'd want to persist events or use the existing event system
		events := []Event{}

		result := map[string]interface{}{
			"events": events,
			"total":  len(events),
		}

		return mcp.NewToolResultText(fmt.Sprintf("Found %d events:\n\n%+v", len(events), result)), nil
	})
}

// publishProposalTasks publishes the tasks stored in a proposal into MCP tasks
func (s *MCPServer) publishProposalTasks(ctx context.Context, proposalID string) error {
	p, err := s.store.GetProposal(ctx, proposalID)
	if err != nil {
		return err
	}
	if len(p.Tasks) == 0 {
		// Try to derive tasks from metadata embedded_message
		if em, ok := p.Metadata["embedded_message"].(string); ok && em != "" {
			p.Tasks = BuildTasksFromMarkdown(p.ID, em, p.VisiblePixelHash, p.BudgetSats, fundingAddressFromMeta(p.Metadata))
		}
		if len(p.Tasks) == 0 {
			return nil
		}
	}
	// Build a contract from the proposal, then upsert tasks
	contract := Contract{
		ContractID:          p.ID,
		Title:               p.Title,
		TotalBudgetSats:     p.BudgetSats,
		GoalsCount:          1,
		AvailableTasksCount: len(p.Tasks),
		Status:              "active",
	}
	// Preserve hashes/funding if present
	fundingAddr := fundingAddressFromMeta(p.Metadata)
	tasks := make([]Task, 0, len(p.Tasks))
	for _, t := range p.Tasks {
		task := t
		if task.ContractID == "" {
			task.ContractID = p.ID
		}
		if task.MerkleProof == nil && p.VisiblePixelHash != "" {
			task.MerkleProof = &MerkleProof{
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
	if pg, ok := s.store.(interface {
		UpsertContractWithTasks(context.Context, Contract, []Task) error
	}); ok {
		if err := pg.UpsertContractWithTasks(ctx, contract, tasks); err != nil {
			return err
		}
		return nil
	}
	return nil
}
