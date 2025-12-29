package smart_contract

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"stargate-backend/core/smart_contract"
)

// MemoryStore holds in-memory MCP data. It is intentionally simple for the MVP and can be swapped for persistent storage later.
type MemoryStore struct {
	mu          sync.RWMutex
	contracts   map[string]smart_contract.Contract
	tasks       map[string]smart_contract.Task
	claims      map[string]smart_contract.Claim
	submissions map[string]smart_contract.Submission
	proposals   map[string]smart_contract.Proposal
	claimTTL    time.Duration
}

// NewMemoryStore seeds fixtures and returns a MemoryStore.
func NewMemoryStore(claimTTL time.Duration) *MemoryStore {
	contracts, tasks := SeedData()
	cMap := make(map[string]smart_contract.Contract, len(contracts))
	for _, c := range contracts {
		cMap[c.ContractID] = c
	}
	tMap := make(map[string]smart_contract.Task, len(tasks))
	for _, t := range tasks {
		tMap[t.TaskID] = t
	}
	store := &MemoryStore{
		contracts:   cMap,
		tasks:       tMap,
		claims:      make(map[string]smart_contract.Claim),
		submissions: make(map[string]smart_contract.Submission),
		proposals:   make(map[string]smart_contract.Proposal),
		claimTTL:    claimTTL,
	}

	// Create missing tasks for contracts that should have them
	store.createMissingTasks()

	return store
}

func containsSkill(all []string, skills []string) bool {
	for _, want := range skills {
		if slices.ContainsFunc(all, func(s string) bool { return strings.EqualFold(s, want) }) {
			return true
		}
	}
	return len(skills) == 0
}

func proposalHasSkills(p smart_contract.Proposal, skills []string) bool {
	if len(skills) == 0 {
		return true
	}
	for _, t := range p.Tasks {
		if containsSkill(t.Skills, skills) {
			return true
		}
	}
	return false
}

func proposalMatchesContract(p smart_contract.Proposal, contractID string) bool {
	if strings.EqualFold(p.ID, contractID) {
		return true
	}
	if p.Metadata == nil {
		return false
	}
	if v, ok := p.Metadata["contract_id"].(string); ok {
		return strings.EqualFold(strings.TrimSpace(v), contractID)
	}
	return false
}

func metaString(meta map[string]interface{}, key string) string {
	if meta == nil {
		return ""
	}
	if v, ok := meta[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func matchesContractMeta(contractID string, proposals map[string]smart_contract.Proposal, filter smart_contract.ContractFilter) bool {
	if strings.TrimSpace(filter.Creator) == "" && strings.TrimSpace(filter.AiIdentifier) == "" {
		return true
	}
	for _, p := range proposals {
		if !proposalMatchesContract(p, contractID) {
			continue
		}
		if filter.Creator != "" && !strings.EqualFold(metaString(p.Metadata, "creator"), filter.Creator) {
			continue
		}
		if filter.AiIdentifier != "" && !strings.EqualFold(metaString(p.Metadata, "ai_identifier"), filter.AiIdentifier) {
			continue
		}
		return true
	}
	return false
}

// ListContracts returns all contracts filtered by status and skill.
func (s *MemoryStore) ListContracts(filter smart_contract.ContractFilter) ([]smart_contract.Contract, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	fmt.Printf("DEBUG: ListContracts called on %p, contracts: %d\n", s, len(s.contracts))
	for id := range s.contracts {
		fmt.Printf("DEBUG: ListContracts - Contract ID: %s\n", id)
	}
	availableCounts := make(map[string]int)
	for _, t := range s.tasks {
		if strings.EqualFold(t.Status, "available") {
			availableCounts[t.ContractID]++
		}
	}
	out := make([]smart_contract.Contract, 0, len(s.contracts))
	for _, c := range s.contracts {
		if filter.Status != "" && !strings.EqualFold(filter.Status, c.Status) {
			continue
		}
		if len(filter.Skills) > 0 && !containsSkill(c.Skills, filter.Skills) {
			continue
		}
		if !matchesContractMeta(c.ContractID, s.proposals, filter) {
			continue
		}
		c.AvailableTasksCount = availableCounts[c.ContractID]
		out = append(out, c)
	}
	return out, nil
}

// ListTasks returns tasks filtered by a TaskFilter.
func (s *MemoryStore) ListTasks(filter smart_contract.TaskFilter) ([]smart_contract.Task, error) {
	s.mu.RLock()
	fmt.Printf("DEBUG: ListTasks called on %p, contracts: %d, tasks: %d\n", s, len(s.contracts), len(s.tasks))
	// Check if we need to create missing tasks
	needTasks := false
	for _, contract := range s.contracts {
		if contract.AvailableTasksCount > 0 {
			// Check if this contract has any tasks
			hasTasks := false
			for _, task := range s.tasks {
				if task.ContractID == contract.ContractID {
					hasTasks = true
					break
				}
			}
			if !hasTasks {
				needTasks = true
				break
			}
		}
	}
	s.mu.RUnlock()

	if needTasks {
		s.createMissingTasks()
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]smart_contract.Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		if filter.Status != "" && !strings.EqualFold(filter.Status, t.Status) {
			continue
		}
		if filter.ContractID != "" && !strings.EqualFold(filter.ContractID, t.ContractID) {
			continue
		}
		if filter.ClaimedBy != "" && !strings.EqualFold(filter.ClaimedBy, t.ClaimedBy) {
			continue
		}
		if len(filter.Skills) > 0 && !containsSkill(t.Skills, filter.Skills) {
			continue
		}
		if filter.MinBudgetSats > 0 && t.BudgetSats < filter.MinBudgetSats {
			continue
		}
		out = append(out, t)
	}

	start := filter.Offset
	if start < 0 {
		start = 0
	}
	end := start + filter.Limit
	if filter.Limit == 0 || end > len(out) {
		end = len(out)
	}
	return out[start:end], nil
}

// GetTask returns a task by ID.
func (s *MemoryStore) GetTask(id string) (smart_contract.Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tasks[id]
	if !ok {
		return smart_contract.Task{}, ErrTaskNotFound
	}
	return t, nil
}

// GetContract returns a contract by ID.
func (s *MemoryStore) GetContract(id string) (smart_contract.Contract, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.contracts[id]
	if !ok {
		return smart_contract.Contract{}, fmt.Errorf("contract %s not found", id)
	}
	return c, nil
}

// ClaimTask reserves a task for an AI. It is idempotent if the same AI reclaims before expiry.
func (s *MemoryStore) ClaimTask(taskID, aiID, contractorWallet string, estimatedCompletion *time.Time) (smart_contract.Claim, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.tasks[taskID]
	if !ok {
		return smart_contract.Claim{}, ErrTaskNotFound
	}
	if strings.EqualFold(task.Status, "approved") || strings.EqualFold(task.Status, "completed") || strings.EqualFold(task.Status, "published") || strings.EqualFold(task.Status, "claimed") || strings.EqualFold(task.Status, "submitted") {
		return smart_contract.Claim{}, ErrTaskUnavailable
	}
	normalizedWallet := strings.TrimSpace(contractorWallet)

	// Existing claim?
	for _, c := range s.claims {
		if c.TaskID == taskID {
			if c.AiIdentifier == aiID && c.Status == "active" && time.Now().Before(c.ExpiresAt) {
				if normalizedWallet != "" && task.ContractorWallet == "" {
					task.ContractorWallet = normalizedWallet
					if task.MerkleProof == nil {
						task.MerkleProof = &smart_contract.MerkleProof{}
					}
					task.MerkleProof.ContractorWallet = normalizedWallet
					s.tasks[taskID] = task
				}
				return c, nil
			}
			if c.Status == "active" && time.Now().Before(c.ExpiresAt) {
				return smart_contract.Claim{}, ErrTaskTaken
			}
		}
	}

	claimID := fmt.Sprintf("CLAIM-%d", time.Now().UnixNano())
	expires := time.Now().Add(s.claimTTL)
	claim := smart_contract.Claim{
		ClaimID:      claimID,
		TaskID:       taskID,
		AiIdentifier: aiID,
		Status:       "active",
		ExpiresAt:    expires,
		CreatedAt:    time.Now(),
	}
	task.Status = "claimed"
	task.ClaimedBy = aiID
	task.ClaimedAt = &claim.CreatedAt
	task.ClaimExpires = &expires
	task.ActiveClaimID = claimID
	if normalizedWallet != "" {
		task.ContractorWallet = normalizedWallet
		if task.MerkleProof == nil {
			task.MerkleProof = &smart_contract.MerkleProof{}
		}
		task.MerkleProof.ContractorWallet = normalizedWallet
	}

	s.claims[claimID] = claim
	s.tasks[taskID] = task

	_ = estimatedCompletion // placeholder until persisted in model
	return claim, nil
}

// SubmitWork records a submission for a claim.
func (s *MemoryStore) SubmitWork(claimID string, deliverables map[string]interface{}, proof map[string]interface{}) (smart_contract.Submission, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	claim, ok := s.claims[claimID]
	if !ok {
		return smart_contract.Submission{}, ErrClaimNotFound
	}
	// Allow submissions on active claims OR submitted claims with existing rejected/reviewed submissions
	if claim.Status != "active" && claim.Status != "submitted" {
		return smart_contract.Submission{}, fmt.Errorf("claim %s not active or submitted", claimID)
	}

	// For submitted claims, check if there's an existing submission that allows resubmission
	if claim.Status == "submitted" {
		for _, sub := range s.submissions {
			if sub.ClaimID == claimID && (sub.Status == "rejected" || sub.Status == "reviewed") {
				// Allow resubmission - reactivate the claim for new submission
				claim.Status = "active"
				s.claims[claimID] = claim
				goto SubmitWork
			}
		}
		// If submitted claim has no rejected/reviewed submissions, don't allow new submission
		return smart_contract.Submission{}, fmt.Errorf("claim %s already submitted with no eligible resubmission", claimID)
	}

SubmitWork:
	if time.Now().After(claim.ExpiresAt) {
		claim.Status = "expired"
		s.claims[claimID] = claim
		return smart_contract.Submission{}, fmt.Errorf("claim %s expired", claimID)
	}

	subID := fmt.Sprintf("SUB-%d", time.Now().UnixNano())
	sub := smart_contract.Submission{
		SubmissionID:    subID,
		ClaimID:         claimID,
		Status:          "pending_review",
		Deliverables:    deliverables,
		CompletionProof: proof,
		CreatedAt:       time.Now(),
	}
	s.submissions[subID] = sub

	// Update task/claim state to submitted.
	task := s.tasks[claim.TaskID]
	task.Status = "submitted"
	task.ActiveClaimID = claimID
	s.tasks[claim.TaskID] = task

	claim.Status = "submitted"
	s.claims[claimID] = claim

	return sub, nil
}

// ListSubmissions returns submissions for the provided task IDs.
func (s *MemoryStore) ListSubmissions(ctx context.Context, taskIDs []string) ([]smart_contract.Submission, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(taskIDs) == 0 {
		return nil, nil
	}
	taskSet := make(map[string]struct{}, len(taskIDs))
	for _, id := range taskIDs {
		taskSet[id] = struct{}{}
	}
	out := make([]smart_contract.Submission, 0)
	for _, sub := range s.submissions {
		if claim, ok := s.claims[sub.ClaimID]; ok {
			if _, hit := taskSet[claim.TaskID]; hit {
				sub.TaskID = claim.TaskID
				out = append(out, sub)
			}
		}
	}
	return out, nil
}

// TaskStatus returns task status, including claim info if present.
func (s *MemoryStore) TaskStatus(taskID string) (map[string]interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	task, ok := s.tasks[taskID]
	if !ok {
		return nil, ErrTaskNotFound
	}

	var claim *smart_contract.Claim
	for _, c := range s.claims {
		if c.TaskID != taskID {
			continue
		}
		if c.Status != "active" && c.Status != "submitted" && c.Status != "pending_review" {
			continue
		}
		if claim == nil || c.CreatedAt.After(claim.CreatedAt) {
			cc := c
			claim = &cc
		}
	}

	resp := map[string]interface{}{
		"task_id":           task.TaskID,
		"status":            task.Status,
		"claimed_by":        task.ClaimedBy,
		"claim_expires_at":  task.ClaimExpires,
		"claimed_at":        task.ClaimedAt,
		"time_remaining_hr": nil,
	}
	submissionAttempts := 0
	for _, sub := range s.submissions {
		if c, ok := s.claims[sub.ClaimID]; ok && c.TaskID == taskID {
			submissionAttempts++
		}
	}
	resp["submission_attempts"] = submissionAttempts

	if claim != nil {
		final := strings.EqualFold(task.Status, "published") || strings.EqualFold(task.Status, "approved") || strings.EqualFold(task.Status, "completed")
		remaining := time.Until(claim.ExpiresAt).Hours()
		resp["time_remaining_hr"] = remaining
		resp["claim_id"] = claim.ClaimID
		switch strings.ToLower(claim.Status) {
		case "submitted", "pending_review":
			if !final {
				resp["status"] = "submitted"
			}
		case "active":
			if !final && (task.Status == "" || strings.EqualFold(task.Status, "available") || strings.EqualFold(task.Status, "approved")) {
				resp["status"] = "claimed"
			}
		case "complete":
			resp["status"] = "approved"
		}
	}
	return resp, nil
}

// GetTaskProof returns the Merkle proof for a task.
func (s *MemoryStore) GetTaskProof(taskID string) (*smart_contract.MerkleProof, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	task, ok := s.tasks[taskID]
	if !ok {
		return nil, ErrTaskNotFound
	}
	return task.MerkleProof, nil
}

// ContractFunding returns the contract and any proofs of funding (mocked for MVP).
func (s *MemoryStore) ContractFunding(contractID string) (smart_contract.Contract, []smart_contract.MerkleProof, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	contract, ok := s.contracts[contractID]
	if !ok {
		return smart_contract.Contract{}, nil, fmt.Errorf("contract %s not found", contractID)
	}
	proofs := []smart_contract.MerkleProof{}
	for _, t := range s.tasks {
		if t.ContractID == contractID && t.MerkleProof != nil {
			proofs = append(proofs, *t.MerkleProof)
		}
	}
	return contract, proofs, nil
}

// Close implements Store; nothing to close for memory.
func (s *MemoryStore) Close() {}

// UpdateTaskProof replaces the merkle_proof for a task in memory.
func (s *MemoryStore) UpdateTaskProof(ctx context.Context, taskID string, proof *smart_contract.MerkleProof) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[taskID]
	if !ok {
		return ErrTaskNotFound
	}
	existingWallet := strings.TrimSpace(t.ContractorWallet)
	if existingWallet == "" && t.MerkleProof != nil {
		existingWallet = strings.TrimSpace(t.MerkleProof.ContractorWallet)
	}
	if proof != nil && existingWallet != "" && strings.TrimSpace(proof.ContractorWallet) == "" {
		cp := *proof
		cp.ContractorWallet = existingWallet
		proof = &cp
	}
	if proof != nil && strings.TrimSpace(t.ContractorWallet) == "" && strings.TrimSpace(proof.ContractorWallet) != "" {
		t.ContractorWallet = strings.TrimSpace(proof.ContractorWallet)
	}
	t.MerkleProof = proof
	s.tasks[taskID] = t
	return nil
}

// CreateProposal stores a new proposal with validation.
func (s *MemoryStore) CreateProposal(ctx context.Context, p smart_contract.Proposal) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if p.Metadata == nil {
		p.Metadata = map[string]interface{}{}
	}
	if strings.TrimSpace(p.VisiblePixelHash) != "" {
		if vph, ok := p.Metadata["visible_pixel_hash"].(string); !ok || strings.TrimSpace(vph) == "" {
			p.Metadata["visible_pixel_hash"] = p.VisiblePixelHash
		}
	}
	if metaContract, ok := p.Metadata["contract_id"].(string); ok {
		metaContract = strings.TrimSpace(metaContract)
		if metaContract != "" {
			if metaHash, ok2 := p.Metadata["visible_pixel_hash"].(string); ok2 {
				metaHash = strings.TrimSpace(metaHash)
				if metaHash != "" && metaHash != metaContract {
					return fmt.Errorf("visible_pixel_hash must match contract_id when both are set")
				}
			}
		}
	}

	// Comprehensive security validation
	if err := ValidateProposalInput(p); err != nil {
		return fmt.Errorf("proposal validation failed: %v", err)
	}

	// Validate status field
	if p.Status == "" {
		p.Status = "pending" // Default to pending
	} else if !isValidProposalStatus(p.Status) {
		return fmt.Errorf("invalid proposal status: %s (must be one of: pending, approved, rejected, published)", p.Status)
	}

	s.proposals[p.ID] = p
	if strings.EqualFold(p.Status, "approved") || strings.EqualFold(p.Status, "published") {
		visible := strings.TrimSpace(p.VisiblePixelHash)
		if visible == "" {
			if v, ok := p.Metadata["visible_pixel_hash"].(string); ok {
				visible = strings.TrimSpace(v)
			}
		}
		if visible == "" {
			if v, ok := p.Metadata["contract_id"].(string); ok {
				visible = strings.TrimSpace(v)
			}
		}
		if visible != "" {
			wishID := "wish-" + visible
			if contract, ok := s.contracts[wishID]; ok {
				contract.Status = "superseded"
				s.contracts[wishID] = contract
			}
		}
	}
	return nil
}

// createMissingTasks creates tasks for contracts that have available_tasks_count > 0 but no actual tasks
func (s *MemoryStore) createMissingTasks() {
	// Temporarily disabled - contracts exist but tasks creation has issues
}

// UpsertContractWithTasks persists a contract and its tasks idempotently.
func (s *MemoryStore) UpsertContractWithTasks(ctx context.Context, contract smart_contract.Contract, tasks []smart_contract.Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Store the contract
	s.contracts[contract.ContractID] = contract

	// Store all tasks
	for _, task := range tasks {
		s.tasks[task.TaskID] = task
	}

	return nil
}

func (s *MemoryStore) ListProposals(ctx context.Context, filter smart_contract.ProposalFilter) ([]smart_contract.Proposal, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []smart_contract.Proposal
	for _, p := range s.proposals {
		if filter.Status != "" && !strings.EqualFold(filter.Status, p.Status) {
			continue
		}
		if filter.ContractID != "" {
			contractID := p.Metadata["contract_id"]
			if contractID == nil {
				contractID = p.Metadata["ingestion_id"]
			}
			cid, _ := contractID.(string)
			if cid == "" {
				cid = p.ID
			}
			if cid != filter.ContractID {
				continue
			}
		}
		if filter.MinBudget > 0 && p.BudgetSats < filter.MinBudget {
			continue
		}
		if len(filter.Skills) > 0 && !proposalHasSkills(p, filter.Skills) {
			continue
		}
		out = append(out, p)
	}
	if filter.Offset > 0 && filter.Offset < len(out) {
		out = out[filter.Offset:]
	}
	if filter.MaxResults > 0 && filter.MaxResults < len(out) {
		out = out[:filter.MaxResults]
	}
	return out, nil
}

func (s *MemoryStore) GetProposal(ctx context.Context, id string) (smart_contract.Proposal, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.proposals[id]
	if !ok {
		return smart_contract.Proposal{}, fmt.Errorf("proposal %s not found", id)
	}
	return p, nil
}

// UpdateProposal updates a pending proposal.
func (s *MemoryStore) UpdateProposal(ctx context.Context, p smart_contract.Proposal) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, ok := s.proposals[p.ID]
	if !ok {
		return fmt.Errorf("proposal %s not found", p.ID)
	}
	if !strings.EqualFold(existing.Status, "pending") {
		return fmt.Errorf("proposal %s must be pending to update, current status: %s", p.ID, existing.Status)
	}

	if p.Title == "" {
		p.Title = existing.Title
	}
	if p.DescriptionMD == "" {
		p.DescriptionMD = existing.DescriptionMD
	}
	if p.VisiblePixelHash == "" {
		p.VisiblePixelHash = existing.VisiblePixelHash
	}
	if p.BudgetSats == 0 {
		p.BudgetSats = existing.BudgetSats
	}
	if p.Metadata == nil {
		p.Metadata = existing.Metadata
	}
	if p.Tasks == nil {
		p.Tasks = existing.Tasks
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = existing.CreatedAt
	}

	p.Status = existing.Status
	if p.Metadata == nil {
		p.Metadata = map[string]interface{}{}
	}
	if strings.TrimSpace(p.VisiblePixelHash) != "" {
		if vph, ok := p.Metadata["visible_pixel_hash"].(string); !ok || strings.TrimSpace(vph) == "" {
			p.Metadata["visible_pixel_hash"] = p.VisiblePixelHash
		}
	}

	if err := ValidateProposalInput(p); err != nil {
		return fmt.Errorf("proposal validation failed: %v", err)
	}

	s.proposals[p.ID] = p
	return nil
}

// ApproveProposal approves a proposal and auto-rejects others for the same contract.
func (s *MemoryStore) ApproveProposal(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, ok := s.proposals[id]
	if !ok {
		return fmt.Errorf("proposal %s not found", id)
	}

	// HACK: temporarily set status to approved to trigger validation
	originalStatus := p.Status
	p.Status = "approved"
	err := ValidateProposalInput(p)
	p.Status = originalStatus // revert status
	if err != nil {
		return err
	}

	// Check if proposal is already in final state
	if strings.EqualFold(p.Status, "approved") || strings.EqualFold(p.Status, "published") {
		return fmt.Errorf("proposal %s is already %s", id, p.Status)
	}

	if !strings.EqualFold(p.Status, "pending") {
		return fmt.Errorf("proposal %s must be pending to approve, current status: %s", id, p.Status)
	}

	contractID := contractIDFromMeta(p.Metadata, id)
	hasTasks := len(p.Tasks) > 0
	if !hasTasks {
		for _, task := range s.tasks {
			if task.ContractID == contractID {
				hasTasks = true
				break
			}
		}
	}
	if !hasTasks {
		return fmt.Errorf("approved proposals must contain at least one task")
	}

	// Reject if another proposal is already approved/published for the same contract.
	for pid, other := range s.proposals {
		if pid == id {
			continue
		}
		otherCID := contractIDFromMeta(other.Metadata, other.ID)
		if otherCID == contractID && (strings.EqualFold(other.Status, "approved") || strings.EqualFold(other.Status, "published")) {
			return fmt.Errorf("another proposal is already approved/published for contract %s", contractID)
		}
	}

	// Auto-reject other pending proposals for this contract.
	for pid, other := range s.proposals {
		if pid == id {
			continue
		}
		otherCID := contractIDFromMeta(other.Metadata, other.ID)
		if otherCID == contractID && strings.EqualFold(other.Status, "pending") {
			other.Status = "rejected"
			s.proposals[pid] = other
		}
	}

	// Update proposal status atomically
	p.Status = "approved"
	s.proposals[id] = p

	visible := strings.TrimSpace(p.VisiblePixelHash)
	if visible == "" {
		if v, ok := p.Metadata["visible_pixel_hash"].(string); ok {
			visible = strings.TrimSpace(v)
		}
	}
	if visible != "" {
		wishID := "wish-" + visible
		if contract, ok := s.contracts[wishID]; ok {
			contract.Status = "superseded"
			s.contracts[wishID] = contract
		}
	}

	// Update related tasks
	for i, t := range s.tasks {
		if t.ContractID == contractID {
			t.Status = "approved"
			s.tasks[i] = t
		}
	}

	return nil
}

// PublishProposal marks tasks as published for the proposal's contract.
func (s *MemoryStore) PublishProposal(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.proposals[id]
	if !ok {
		return fmt.Errorf("proposal %s not found", id)
	}
	if !strings.EqualFold(p.Status, "approved") && !strings.EqualFold(p.Status, "published") {
		return fmt.Errorf("proposal %s must be approved before publish", id)
	}
	contractID := contractIDFromMeta(p.Metadata, id)
	for i, t := range s.tasks {
		if t.ContractID == contractID {
			switch strings.ToLower(t.Status) {
			case "submitted", "pending_review", "claimed", "approved":
				t.Status = "published"
				s.tasks[i] = t
			}
		}
	}
	for id, c := range s.claims {
		task, ok := s.tasks[c.TaskID]
		if !ok || task.ContractID != contractID {
			continue
		}
		if strings.EqualFold(c.Status, "submitted") || strings.EqualFold(c.Status, "pending_review") || strings.EqualFold(c.Status, "active") || strings.EqualFold(c.Status, "approved") {
			c.Status = "complete"
			s.claims[id] = c
		}
	}
	p.Status = "published"
	s.proposals[id] = p
	return nil
}

// UpdateSubmissionStatus updates the status of a submission and related entities.
func (s *MemoryStore) UpdateSubmissionStatus(ctx context.Context, submissionID, status, reviewerNotes, rejectionType string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sub, ok := s.submissions[submissionID]
	if !ok {
		return ErrClaimNotFound // close enough
	}

	sub.Status = status
	if status == "rejected" {
		note := strings.TrimSpace(reviewerNotes)
		rejType := strings.TrimSpace(rejectionType)
		sub.RejectionReason = note
		sub.RejectionType = rejType
		now := time.Now()
		sub.RejectedAt = &now
	} else {
		sub.RejectionReason = ""
		sub.RejectionType = ""
		sub.RejectedAt = nil
	}
	s.submissions[submissionID] = sub

	switch status {
	case "accepted", "approved":
		claim, ok := s.claims[sub.ClaimID]
		if !ok {
			return nil // should not happen
		}
		claim.Status = "complete"
		s.claims[sub.ClaimID] = claim

		task, ok := s.tasks[claim.TaskID]
		if !ok {
			return nil // should not happen
		}
		task.Status = "approved"
		s.tasks[claim.TaskID] = task
	case "rejected":
		claim, ok := s.claims[sub.ClaimID]
		if ok {
			claim.Status = "rejected"
			s.claims[sub.ClaimID] = claim

			task, ok := s.tasks[claim.TaskID]
			if ok {
				task.Status = "available"
				task.ClaimedBy = ""
				task.ClaimedAt = nil
				task.ClaimExpires = nil
				task.ActiveClaimID = ""
				s.tasks[claim.TaskID] = task
			}
		}
	}

	return nil
}
