package mcp

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"stargate-backend/core/smart_contract"
	scmiddleware "stargate-backend/middleware/smart_contract"
	"stargate-backend/services"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// MCPServer wraps the mcp-go server with our business logic
type MCPServer struct {
	mcpServer    *server.MCPServer
	store        scmiddleware.Store
	apiKey       string
	ingestionSvc *services.IngestionService
}

// NewMCPServer creates a new MCP server using the mcp-go library
func NewMCPServer(store scmiddleware.Store, apiKey string, ingestSvc *services.IngestionService) *MCPServer {
	// Create the MCP server
	mcpServer := server.NewMCPServer(
		"Stargate MCP Server",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	s := &MCPServer{
		mcpServer:    mcpServer,
		store:        store,
		apiKey:       apiKey,
		ingestionSvc: ingestSvc,
	}

	// Register all tools
	s.registerTools()

	return s
}

// GetMCPServer returns the underlying MCP server for transport setup
func (s *MCPServer) GetMCPServer() *server.MCPServer {
	return s.mcpServer
}

// registerTools registers all MCP tools with the server
func (s *MCPServer) registerTools() {
	// Contracts tools
	s.registerListContractsTool()
	s.registerGetContractTool()
	s.registerContractFundingTool()

	// Tasks tools
	s.registerListTasksTool()
	s.registerGetTaskTool()
	s.registerClaimTaskTool()
	s.registerSubmitWorkTool()
	s.registerGetTaskProofTool()
	s.registerGetTaskStatusTool()

	// Skills tool
	s.registerListSkillsTool()

	// Proposals tools
	s.registerListProposalsTool()
	s.registerGetProposalTool()
	s.registerCreateProposalTool()
	s.registerApproveProposalTool()
	s.registerPublishProposalTool()

	// Submissions tools
	s.registerListSubmissionsTool()
	s.registerGetSubmissionTool()
	s.registerReviewSubmissionTool()
	s.registerReworkSubmissionTool()

	// Events tool
	s.registerListEventsTool()
}

// registerListContractsTool creates a tool for listing contracts
func (s *MCPServer) registerListContractsTool() {
	tool := mcp.NewTool("list_contracts",
		mcp.WithDescription("List available contracts with optional filtering"),
		mcp.WithString("status", mcp.Description("Filter by contract status")),
		mcp.WithArray("skills", mcp.Description("Filter by required skills")),
	)

	s.mcpServer.AddTool(tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.GetArguments()

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

		contracts, err := s.store.ListContracts(status, skills)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to list contracts: %v", err)), nil
		}

		result := map[string]interface{}{
			"contracts":   contracts,
			"total_count": len(contracts),
		}

		return mcp.NewToolResultText(fmt.Sprintf("Found %d contracts:\n\n%+v", len(contracts), result)), nil
	})
}

// registerGetContractTool creates a tool for getting a specific contract
func (s *MCPServer) registerGetContractTool() {
	tool := mcp.NewTool("get_contract",
		mcp.WithDescription("Get details of a specific contract"),
		mcp.WithString("contract_id", mcp.Required(), mcp.Description("ID of contract to retrieve")),
	)

	s.mcpServer.AddTool(tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		contractID, err := request.RequireString("contract_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		contract, err := s.store.GetContract(contractID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to get contract: %v", err)), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("Contract details:\n\n%+v", contract)), nil
	})
}

// registerContractFundingTool creates a tool for getting contract funding information
func (s *MCPServer) registerContractFundingTool() {
	tool := mcp.NewTool("get_contract_funding",
		mcp.WithDescription("Get funding information for a contract"),
		mcp.WithString("contract_id", mcp.Required(), mcp.Description("ID of contract")),
	)

	s.mcpServer.AddTool(tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		contractID, err := request.RequireString("contract_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		contract, proofs, err := s.store.ContractFunding(contractID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to get contract funding: %v", err)), nil
		}

		result := map[string]interface{}{
			"contract": contract,
			"proofs":   proofs,
		}

		return mcp.NewToolResultText(fmt.Sprintf("Contract funding information:\n\n%+v", result)), nil
	})
}

// registerListTasksTool creates a tool for listing tasks
func (s *MCPServer) registerListTasksTool() {
	tool := mcp.NewTool("list_tasks",
		mcp.WithDescription("List available tasks with filtering"),
		mcp.WithArray("skills", mcp.Description("Filter by required skills")),
		mcp.WithString("max_difficulty", mcp.Description("Maximum difficulty level")),
		mcp.WithString("status", mcp.Description("Filter by task status")),
		mcp.WithNumber("limit", mcp.Description("Maximum number of tasks to return")),
		mcp.WithNumber("offset", mcp.Description("Number of tasks to skip")),
		mcp.WithNumber("min_budget_sats", mcp.Description("Minimum budget in satoshis")),
		mcp.WithString("contract_id", mcp.Description("Filter by contract ID")),
		mcp.WithString("claimed_by", mcp.Description("Filter by claimant")),
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

		filter := smart_contract.TaskFilter{
			Skills:        skills,
			MaxDifficulty: toString(args["max_difficulty"]),
			Status:        toString(args["status"]),
			Limit:         int(toInt64(args["limit"])),
			Offset:        int(toInt64(args["offset"])),
			MinBudgetSats: toInt64(args["min_budget_sats"]),
			ContractID:    toString(args["contract_id"]),
			ClaimedBy:     toString(args["claimed_by"]),
		}

		if filter.Limit == 0 {
			filter.Limit = 50
		}

		tasks, err := s.store.ListTasks(filter)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to list tasks: %v", err)), nil
		}

		// Get submissions for these tasks
		var taskIDs []string
		for _, t := range tasks {
			taskIDs = append(taskIDs, t.TaskID)
		}
		subs, _ := s.store.ListSubmissions(ctx, taskIDs)

		result := map[string]interface{}{
			"tasks":         tasks,
			"total_matches": len(tasks),
			"submissions":   subs,
		}

		return mcp.NewToolResultText(fmt.Sprintf("Found %d tasks:\n\n%+v", len(tasks), result)), nil
	})
}

// registerGetTaskTool creates a tool for getting a specific task
func (s *MCPServer) registerGetTaskTool() {
	tool := mcp.NewTool("get_task",
		mcp.WithDescription("Get details of a specific task"),
		mcp.WithString("task_id", mcp.Required(), mcp.Description("ID of task to retrieve")),
	)

	s.mcpServer.AddTool(tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		taskID, err := request.RequireString("task_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		task, err := s.store.GetTask(taskID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to get task: %v", err)), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("Task details:\n\n%+v", task)), nil
	})
}

// registerClaimTaskTool creates a tool for claiming a task
func (s *MCPServer) registerClaimTaskTool() {
	tool := mcp.NewTool("claim_task",
		mcp.WithDescription("Claim a task for work"),
		mcp.WithString("task_id", mcp.Required(), mcp.Description("ID of task to claim")),
		mcp.WithString("ai_identifier", mcp.Required(), mcp.Description("Identifier of the AI claiming the task")),
		mcp.WithString("estimated_completion", mcp.Description("Estimated completion time (ISO 8601 format)")),
	)

	s.mcpServer.AddTool(tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.GetArguments()

		taskID, err := request.RequireString("task_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		aiIdentifier, err := request.RequireString("ai_identifier")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		var estimatedCompletion *time.Time
		if estStr := toString(args["estimated_completion"]); estStr != "" {
			if est, err := time.Parse(time.RFC3339, estStr); err == nil {
				estimatedCompletion = &est
			}
		}

		claim, err := s.store.ClaimTask(taskID, aiIdentifier, estimatedCompletion)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to claim task: %v", err)), nil
		}

		result := map[string]interface{}{
			"success":    true,
			"claim_id":   claim.ClaimID,
			"expires_at": claim.ExpiresAt,
			"message":    "Task reserved. Submit work before expiration.",
		}

		return mcp.NewToolResultText(fmt.Sprintf("Task claimed successfully:\n\n%+v", result)), nil
	})
}

// registerSubmitWorkTool creates a tool for submitting work
func (s *MCPServer) registerSubmitWorkTool() {
	tool := mcp.NewTool("submit_work",
		mcp.WithDescription("Submit completed work for a claimed task"),
		mcp.WithString("claim_id", mcp.Required(), mcp.Description("ID of the claim")),
		mcp.WithObject("deliverables", mcp.Required(), mcp.Description("Work deliverables")),
		mcp.WithObject("completion_proof", mcp.Description("Proof of completion")),
	)

	s.mcpServer.AddTool(tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.GetArguments()

		claimID, err := request.RequireString("claim_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		deliverables := toMap(args["deliverables"])
		completionProof := toMap(args["completion_proof"])

		if deliverables == nil {
			return mcp.NewToolResultError("deliverables are required"), nil
		}

		sub, err := s.store.SubmitWork(claimID, deliverables, completionProof)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to submit work: %v", err)), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("Work submitted successfully:\n\n%+v", sub)), nil
	})
}

// registerGetTaskProofTool creates a tool for getting task merkle proof
func (s *MCPServer) registerGetTaskProofTool() {
	tool := mcp.NewTool("get_task_proof",
		mcp.WithDescription("Get merkle proof for a task"),
		mcp.WithString("task_id", mcp.Required(), mcp.Description("ID of task")),
	)

	s.mcpServer.AddTool(tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		taskID, err := request.RequireString("task_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		proof, err := s.store.GetTaskProof(taskID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to get task proof: %v", err)), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("Task proof:\n\n%+v", proof)), nil
	})
}

// registerGetTaskStatusTool creates a tool for getting task status
func (s *MCPServer) registerGetTaskStatusTool() {
	tool := mcp.NewTool("get_task_status",
		mcp.WithDescription("Get status of a task"),
		mcp.WithString("task_id", mcp.Required(), mcp.Description("ID of task")),
	)

	s.mcpServer.AddTool(tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		taskID, err := request.RequireString("task_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		status, err := s.store.TaskStatus(taskID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to get task status: %v", err)), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("Task status:\n\n%+v", status)), nil
	})
}

// registerListSkillsTool creates a tool for listing available skills
func (s *MCPServer) registerListSkillsTool() {
	tool := mcp.NewTool("list_skills",
		mcp.WithDescription("List all available skills across tasks"),
	)

	s.mcpServer.AddTool(tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		tasks, err := s.store.ListTasks(smart_contract.TaskFilter{})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to list tasks: %v", err)), nil
		}

		skillSet := make(map[string]struct{})
		// Add default skills
		skillSet["contract_bidding"] = struct{}{}
		skillSet["get_pending_transactions"] = struct{}{}

		for _, t := range tasks {
			for _, skill := range t.Skills {
				key := strings.ToLower(strings.TrimSpace(skill))
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

		result := map[string]interface{}{
			"skills": skills,
			"count":  len(skills),
		}

		return mcp.NewToolResultText(fmt.Sprintf("Available skills (%d total):\n\n%+v", len(skills), result)), nil
	})
}

// Helper function to convert interface{} to string
func toString(val interface{}) string {
	if str, ok := val.(string); ok {
		return str
	}
	return fmt.Sprintf("%v", val)
}

// Helper function to convert interface{} to int64
func toInt64(val interface{}) int64 {
	if i, ok := val.(int64); ok {
		return i
	}
	if i, ok := val.(int); ok {
		return int64(i)
	}
	if f, ok := val.(float64); ok {
		return int64(f)
	}
	if str, ok := val.(string); ok {
		if i, err := strconv.ParseInt(str, 10, 64); err == nil {
			return i
		}
	}
	return 0
}

// Helper function to convert interface{} to map[string]interface{}
func toMap(val interface{}) map[string]interface{} {
	if m, ok := val.(map[string]interface{}); ok {
		return m
	}
	return nil
}
