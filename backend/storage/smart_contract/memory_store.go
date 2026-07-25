package smart_contract

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"stargate-backend/core/identity"
	"stargate-backend/core/smart_contract"
)

// MemoryStore holds in-memory MCP data with proper concurrency control.
// The single RWMutex ensures atomic operations across multiple maps.
// This prevents race conditions when operations need to modify related data.
type MemoryStore struct {
	mu           sync.RWMutex
	contracts    map[string]smart_contract.Contract
	tasks        map[string]smart_contract.Task
	claims       map[string]smart_contract.Claim
	submissions  map[string]smart_contract.Submission
	proposals    map[string]smart_contract.Proposal
	escortStatus map[string]smart_contract.EscortStatus
	claimTTL     time.Duration
}

// NewMemoryStore seeds fixtures and returns a MemoryStore.
func NewMemoryStore(claimTTL time.Duration) *MemoryStore {
	contracts, tasks := SeedData()
	now := time.Now()
	cMap := make(map[string]smart_contract.Contract, len(contracts))
	for _, c := range contracts {
		if c.CreatedAt.IsZero() {
			c.CreatedAt = now
		}
		cMap[c.ContractID] = c
	}
	tMap := make(map[string]smart_contract.Task, len(tasks))
	for _, t := range tasks {
		tMap[t.TaskID] = t
	}
	store := &MemoryStore{
		contracts:    cMap,
		tasks:        tMap,
		claims:       make(map[string]smart_contract.Claim),
		submissions:  make(map[string]smart_contract.Submission),
		proposals:    make(map[string]smart_contract.Proposal),
		escortStatus: make(map[string]smart_contract.EscortStatus),
		claimTTL:     claimTTL,
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

// ListContracts returns all contracts filtered by status and skill with pagination.
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
		if len(filter.Statuses) > 0 {
			matched := false
			for _, st := range filter.Statuses {
				if strings.EqualFold(st, c.Status) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		} else if filter.Status != "" && !strings.EqualFold(filter.Status, c.Status) {
			continue
		}
		if len(filter.Skills) > 0 && !containsSkill(c.Skills, filter.Skills) {
			continue
		}
		if !matchesContractMeta(c.ContractID, s.proposals, filter) {
			continue
		}

		// Cursor pagination by height
		if filter.CursorHeight != nil && *filter.CursorHeight > 0 {
			if c.ConfirmedBlockHeight == nil || *c.ConfirmedBlockHeight >= *filter.CursorHeight {
				continue
			}
		}

		// Cursor pagination by date (created_at for open lists, confirmed_at otherwise)
		if filter.CursorDate != nil {
			var t *time.Time
			if filter.OrderByCreatedAt {
				if !c.CreatedAt.IsZero() {
					t = &c.CreatedAt
				}
			} else if c.ConfirmedAt != nil {
				t = c.ConfirmedAt
			}
			if t == nil {
				continue
			}
			if strings.EqualFold(filter.CursorType, "after") {
				if !t.After(*filter.CursorDate) {
					continue
				}
			} else {
				if !t.Before(*filter.CursorDate) {
					continue
				}
			}
		}

		c.AvailableTasksCount = availableCounts[c.ContractID]
		out = append(out, c)
	}

	// Sort based on filter preference
	if filter.OrderByCreatedAt {
		sort.Slice(out, func(i, j int) bool {
			if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
				return out[i].CreatedAt.After(out[j].CreatedAt)
			}
			return out[i].ContractID > out[j].ContractID
		})
	} else if filter.OrderByConfirmedAt {
		sort.Slice(out, func(i, j int) bool {
			if out[i].ConfirmedAt == nil {
				return false
			}
			if out[j].ConfirmedAt == nil {
				return true
			}
			return out[i].ConfirmedAt.After(*out[j].ConfirmedAt)
		})
	} else {
		sort.Slice(out, func(i, j int) bool {
			h1 := 0
			if out[i].ConfirmedBlockHeight != nil {
				h1 = *out[i].ConfirmedBlockHeight
			}
			h2 := 0
			if out[j].ConfirmedBlockHeight != nil {
				h2 = *out[j].ConfirmedBlockHeight
			}
			if h1 != h2 {
				return h1 > h2
			}
			return out[i].ContractID > out[j].ContractID
		})
	}

	// Apply pagination
	if filter.Offset > 0 && filter.Offset < len(out) {
		out = out[filter.Offset:]
	}
	if filter.Limit > 0 && filter.Limit < len(out) {
		out = out[:filter.Limit]
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

		// Add time-based filtering for UpdatedSince
		if filter.UpdatedSince != nil {
			if t.MerkleProof == nil {
				continue
			}
			proof := t.MerkleProof
			hasRecentActivity := proof.SeenAt.After(*filter.UpdatedSince) ||
				(proof.SweepAttemptedAt != nil && proof.SweepAttemptedAt.After(*filter.UpdatedSince))
			if !hasRecentActivity {
				continue
			}
		}

		// Add time-based filtering for LastActivitySince
		if filter.LastActivitySince != nil {
			if t.MerkleProof == nil {
				continue
			}
			proof := t.MerkleProof
			hasRecentActivity := proof.SeenAt.After(*filter.LastActivitySince) ||
				(proof.ConfirmedAt != nil && proof.ConfirmedAt.After(*filter.LastActivitySince)) ||
				(proof.SweepAttemptedAt != nil && proof.SweepAttemptedAt.After(*filter.LastActivitySince))
			if !hasRecentActivity {
				continue
			}
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

// GetClaim returns a claim by ID.
func (s *MemoryStore) GetClaim(id string) (smart_contract.Claim, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.claims[id]
	if !ok {
		return smart_contract.Claim{}, ErrClaimNotFound
	}
	return c, nil
}

// ClaimTask reserves a task for an AI. It is idempotent if the same AI reclaims before expiry.
func (s *MemoryStore) ClaimTask(taskID, walletAddress string, estimatedCompletion *time.Time) (smart_contract.Claim, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.tasks[taskID]
	if !ok {
		return smart_contract.Claim{}, ErrTaskNotFound
	}

	existing := make([]smart_contract.Claim, 0)
	for _, c := range s.claims {
		if c.TaskID == taskID {
			existing = append(existing, c)
		}
	}
	now := time.Now()
	plan, err := DecideClaim(ClaimInput{
		TaskID:            taskID,
		TaskStatus:        task.Status,
		Wallet:            walletAddress,
		ExistingClaims:    existing,
		CurrentProof:      task.MerkleProof,
		CurrentContractor: task.ContractorWallet,
		ClaimTTL:          s.claimTTL,
		Now:               now,
		NewClaimID:        fmt.Sprintf("CLAIM-%d", now.UnixNano()),
	})
	if err != nil {
		return smart_contract.Claim{}, err
	}

	task.Status = plan.TaskStatus
	task.ClaimedBy = plan.ClaimedBy
	if plan.ClaimedAt != nil {
		task.ClaimedAt = plan.ClaimedAt
	}
	if plan.ClaimExpires != nil {
		task.ClaimExpires = plan.ClaimExpires
	}
	task.ActiveClaimID = plan.Claim.ClaimID
	if plan.UpdateProof {
		task.MerkleProof = plan.Proof
		task.ContractorWallet = plan.ContractorWallet
	} else if plan.ContractorWallet != "" && task.ContractorWallet == "" {
		task.ContractorWallet = plan.ContractorWallet
	}
	s.tasks[taskID] = task

	if plan.Action == ClaimActionCreate {
		s.claims[plan.Claim.ClaimID] = plan.Claim
	}

	_ = estimatedCompletion // placeholder until persisted in model
	return plan.Claim, nil
}

// SubmitWork records a submission for a claim.
func (s *MemoryStore) SubmitWork(claimID string, deliverables map[string]interface{}, proof map[string]interface{}) (smart_contract.Submission, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	claim, ok := s.claims[claimID]
	if !ok {
		return smart_contract.Submission{}, ErrClaimNotFound
	}

	var statuses []string
	for _, sub := range s.submissions {
		if sub.ClaimID == claimID {
			statuses = append(statuses, sub.Status)
		}
	}
	now := time.Now()
	plan, err := DecideSubmit(SubmitInput{
		Claim:                      claim,
		ExistingSubmissionStatuses: statuses,
		Deliverables:               deliverables,
		Proof:                      proof,
		Now:                        now,
		NewSubmissionID:            fmt.Sprintf("SUB-%d", now.UnixNano()),
	})
	if plan.MarkClaimExpired {
		claim.Status = "expired"
		s.claims[claimID] = claim
	}
	if err != nil {
		return smart_contract.Submission{}, err
	}

	s.submissions[plan.Submission.SubmissionID] = plan.Submission

	task := s.tasks[claim.TaskID]
	task.Status = "submitted"
	task.ActiveClaimID = claimID
	s.tasks[claim.TaskID] = task

	claim.Status = "submitted"
	s.claims[claimID] = claim

	return plan.Submission, nil
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
	if proof != nil {
		existingWallet := strings.TrimSpace(t.ContractorWallet)
		if existingWallet == "" && t.MerkleProof != nil {
			existingWallet = strings.TrimSpace(t.MerkleProof.ContractorWallet)
		}
		if existingWallet != "" && strings.TrimSpace(proof.ContractorWallet) == "" {
			cp := *proof
			cp.ContractorWallet = existingWallet
			proof = &cp
		}
		if strings.TrimSpace(t.ContractorWallet) == "" && strings.TrimSpace(proof.ContractorWallet) != "" {
			t.ContractorWallet = strings.TrimSpace(proof.ContractorWallet)
		}
		t.MerkleProof = proof
	}
	s.tasks[taskID] = t
	return nil
}

// UpdateContractStatus updates the status for a contract.
func (s *MemoryStore) UpdateContractStatus(ctx context.Context, contractID, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	contractID = strings.TrimSpace(contractID)
	status = strings.TrimSpace(status)
	if contractID == "" || status == "" {
		return nil
	}
	contract, ok := s.contracts[contractID]
	if !ok {
		return fmt.Errorf("contract %s not found", contractID)
	}
	contract.Status = status
	s.contracts[contractID] = contract
	if strings.EqualFold(status, "confirmed") {
		normalized := NormalizeContractID(contractID)
		for id, proposal := range s.proposals {
			proposalCID := NormalizeContractID(contractIDFromMeta(proposal.Metadata, proposal.ID))
			if proposalCID == normalized && strings.EqualFold(proposal.Status, "approved") {
				proposal.Status = "confirmed"
				s.proposals[id] = proposal
			}
		}
	}
	return nil
}

// ConfirmContract confirms a contract and records confirmation details.
func (s *MemoryStore) ConfirmContract(ctx context.Context, contractID string, blockHeight int, txid string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	contractID = strings.TrimSpace(contractID)
	if contractID == "" {
		return nil
	}

	apply := BuildConfirmApply(contractID, blockHeight, "")
	plan := apply.Plan
	normalized := apply.Normalized
	wishID := apply.WishID

	var contract smart_contract.Contract
	var foundID string
	for _, id := range plan.ConfirmTryOrder(contractID) {
		if c, ok := s.contracts[id]; ok {
			contract = c
			foundID = id
			break
		}
	}
	if foundID == "" {
		return fmt.Errorf("contract %s not found", contractID)
	}
	targetID := foundID
	if plan.IsPixelHash {
		targetID = wishID
	}

	ApplyConfirmToContract(&contract, targetID, blockHeight, txid, apply.StegoImageURL, time.Now())
	s.contracts[targetID] = contract

	if plan.IsPixelHash {
		for _, alias := range apply.AliasesToForceSupersede {
			if alias == targetID {
				continue
			}
			if c, ok := s.contracts[alias]; ok && !strings.EqualFold(c.Status, "superseded") {
				c.Status = "superseded"
				s.contracts[alias] = c
			}
		}
	} else if apply.SupersedeWishIfNonPixel {
		if wishContract, wok := s.contracts[wishID]; wok && ContractStatusMaySupersede(wishContract.Status) {
			wishContract.Status = "superseded"
			s.contracts[wishID] = wishContract
		}
	}

	for pid, proposal := range s.proposals {
		if ProposalShouldConfirmOnChain(proposal.Status, proposal.ID, proposal.VisiblePixelHash, proposal.Metadata, normalized, wishID) {
			proposal.Status = "confirmed"
			s.proposals[pid] = proposal
		}
	}
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
					normalizedContract := strings.TrimPrefix(metaContract, "wish-")
					if metaHash != normalizedContract {
						return fmt.Errorf("visible_pixel_hash must match contract_id when both are set (normalized: %s)", normalizedContract)
					}
				}
			}
		}
	}

	// Comprehensive security validation
	if err := ValidateProposalInput(&p); err != nil {
		return fmt.Errorf("proposal validation failed: %v", err)
	}

	// Validate status field
	if p.Status == "" {
		p.Status = "pending" // Default to pending
	} else if !isValidProposalStatus(p.Status) {
		return fmt.Errorf("invalid proposal status: %s (must be one of: pending, approved, rejected, published)", p.Status)
	}

	// Check for duplicate visible_pixel_hash or max limit
	visibleHash := strings.TrimSpace(p.VisiblePixelHash)
	if visibleHash == "" {
		if v, ok := p.Metadata["visible_pixel_hash"].(string); ok {
			visibleHash = strings.TrimSpace(v)
		}
	}
	if visibleHash != "" {
		count := 0
		for _, prop := range s.proposals {
			if prop.VisiblePixelHash == visibleHash && prop.ID != p.ID {
				if strings.EqualFold(prop.Status, "approved") || strings.EqualFold(prop.Status, "published") {
					return fmt.Errorf("a proposal with visible_pixel_hash=%s is already approved/published (id=%s)", visibleHash, prop.ID)
				}
				count++
			}
		}
		if count >= 5 {
			return fmt.Errorf("maximum of 5 proposals reached for wish %s", visibleHash)
		}
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
			wishID := identity.ToWishID(visible)
			if contract, ok := s.contracts[wishID]; ok {
				if !IsNonSupersedableContractStatus(contract.Status) && !strings.EqualFold(contract.Status, "superseded") {
					contract.Status = "superseded"
					s.contracts[wishID] = contract
				}
			}
		}
	}
	return nil
}

// createMissingTasks creates tasks for contracts that have available_tasks_count > 0 but no actual tasks
func (s *MemoryStore) createMissingTasks() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for contractID, contract := range s.contracts {
		if contract.AvailableTasksCount <= 0 {
			continue
		}

		// Check if contract already has tasks
		hasTasks := false
		for _, task := range s.tasks {
			if task.ContractID == contractID {
				hasTasks = true
				break
			}
		}

		if hasTasks {
			continue
		}

		// Create default tasks for the contract
		for i := 0; i < contract.AvailableTasksCount; i++ {
			taskID := fmt.Sprintf("%s-task-%d", contractID, i+1)
			task := smart_contract.Task{
				TaskID:         taskID,
				ContractID:     contractID,
				GoalID:         fmt.Sprintf("goal-%d", i+1),
				Title:          fmt.Sprintf("Task %d for %s", i+1, contract.Title),
				Description:    fmt.Sprintf("Default task %d for contract %s", i+1, contract.Title),
				BudgetSats:     contract.TotalBudgetSats / int64(contract.AvailableTasksCount),
				Status:         "available",
				Difficulty:     "medium",
				EstimatedHours: 8,
				Skills:         contract.Skills,
			}
			s.tasks[taskID] = task
		}
	}
}

// UpsertContractWithTasks persists a contract and its tasks idempotently.
func (s *MemoryStore) UpsertContractWithTasks(ctx context.Context, contract smart_contract.Contract, tasks []smart_contract.Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Ensure CreatedAt is set
	if contract.CreatedAt.IsZero() {
		contract.CreatedAt = time.Now()
	}

	if existing, ok := s.contracts[contract.ContractID]; ok {
		contract = ProtectContractUpsert(existing, contract)
	}
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
		if filter.ProposalID != "" && filter.ProposalID != p.ID {
			continue
		}
		if filter.Status != "" && !strings.EqualFold(filter.Status, p.Status) {
			continue
		}
		if filter.ContractID != "" {
			var candidates []string
			if v, ok := p.Metadata["contract_id"].(string); ok {
				candidates = append(candidates, v)
			}
			if v, ok := p.Metadata["ingestion_id"].(string); ok {
				candidates = append(candidates, v)
			}
			if v, ok := p.Metadata["visible_pixel_hash"].(string); ok {
				candidates = append(candidates, v)
			}
			candidates = append(candidates, p.VisiblePixelHash, p.ID)

			filterNorm := strings.TrimSpace(filter.ContractID)
			filterBare := strings.TrimPrefix(filterNorm, "wish-")
			filterWish := "wish-" + filterBare

			match := false
			for _, candidate := range candidates {
				c := strings.TrimSpace(candidate)
				if c == filterNorm || c == filterBare || c == filterWish {
					match = true
					break
				}
			}
			if !match {
				continue
			}
		}
		if filter.MinBudget > 0 && p.BudgetSats < filter.MinBudget {
			continue
		}
		if len(filter.Skills) > 0 && !proposalHasSkills(p, filter.Skills) {
			continue
		}

		// Hydrate tasks
		populateProposalTasks(&p)
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

	// Hydrate tasks
	populateProposalTasks(&p)

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

	if p.Status == "" {
		p.Status = existing.Status
	}
	if p.Metadata == nil {
		p.Metadata = map[string]interface{}{}
	}
	if strings.TrimSpace(p.VisiblePixelHash) != "" {
		if vph, ok := p.Metadata["visible_pixel_hash"].(string); !ok || strings.TrimSpace(vph) == "" {
			p.Metadata["visible_pixel_hash"] = p.VisiblePixelHash
		}
	}

	if err := ValidateProposalInput(&p); err != nil {
		return fmt.Errorf("proposal validation failed: %v", err)
	}

	s.proposals[p.ID] = p
	return nil
}

// UpdateProposalMetadata updates proposal metadata without status restrictions.
func (s *MemoryStore) UpdateProposalMetadata(ctx context.Context, id string, updates map[string]interface{}) error {
	if strings.TrimSpace(id) == "" || len(updates) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, ok := s.proposals[id]
	if !ok {
		return fmt.Errorf("proposal %s not found", id)
	}
	meta := existing.Metadata
	if meta == nil {
		meta = map[string]interface{}{}
	}
	for k, v := range updates {
		meta[k] = v
	}
	if strings.TrimSpace(existing.VisiblePixelHash) != "" {
		if vph, ok := meta["visible_pixel_hash"].(string); !ok || strings.TrimSpace(vph) == "" {
			meta["visible_pixel_hash"] = existing.VisiblePixelHash
		}
	}
	existing.Metadata = meta
	s.proposals[id] = existing
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

	populateProposalTasks(&p)
	contractTaskCount := 0
	// provisional keys for task count before BuildApprovePlan
	preKeys := ResolveApproveKeys(p.Metadata, id)
	for _, task := range s.tasks {
		if task.ContractID == preKeys.ContractID {
			contractTaskCount++
		}
	}
	plan, err := BuildApprovePlan(id, p.Status, &p, contractTaskCount)
	if err != nil {
		return err
	}
	keys := plan.Keys

	for pid, other := range s.proposals {
		if pid == id {
			continue
		}
		if !ProposalMatchesApproveConflict(other.ID, other.VisiblePixelHash, other.Metadata, keys) {
			continue
		}
		if strings.EqualFold(other.Status, "approved") || strings.EqualFold(other.Status, "published") {
			return ErrApproveConflict(keys.NormalizedContractID)
		}
	}
	for pid, other := range s.proposals {
		if pid == id {
			continue
		}
		if !ProposalMatchesApproveConflict(other.ID, other.VisiblePixelHash, other.Metadata, keys) {
			continue
		}
		if strings.EqualFold(other.Status, "pending") {
			other.Status = "rejected"
			s.proposals[pid] = other
		}
	}

	p.Status = plan.NewProposalStatus
	s.proposals[id] = p

	if plan.WishToSupersede != "" {
		if contract, ok := s.contracts[plan.WishToSupersede]; ok && ContractStatusMaySupersede(contract.Status) {
			contract.Status = "superseded"
			s.contracts[plan.WishToSupersede] = contract
		}
	}

	if plan.SetRelatedTasksApproved {
		for i, t := range s.tasks {
			if t.ContractID == keys.ContractID {
				t.Status = "approved"
				s.tasks[i] = t
			}
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
	plan, err := BuildPublishPlan(id, p.Status, p.Metadata)
	if err != nil {
		return err
	}
	for i, t := range s.tasks {
		if t.ContractID == plan.ContractID && TaskStatusShouldPublishOnPublish(t.Status) {
			t.Status = "published"
			s.tasks[i] = t
		}
	}
	for cid, c := range s.claims {
		task, ok := s.tasks[c.TaskID]
		if !ok || task.ContractID != plan.ContractID {
			continue
		}
		if ClaimStatusShouldCompleteOnPublish(c.Status) {
			c.Status = "complete"
			s.claims[cid] = c
		}
	}
	p.Status = plan.NewProposalStatus
	s.proposals[id] = p
	return nil
}

// SyncClaim persists a claim from another instance.
func (s *MemoryStore) SyncClaim(ctx context.Context, claim smart_contract.Claim) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.claims[claim.ClaimID] = claim
	if t, ok := s.tasks[claim.TaskID]; ok {
		if claim.Status == "active" || claim.Status == "submitted" {
			// Check for conflicting claim: if task already claimed by different user, reject sync
			if t.ClaimedBy != "" && t.ClaimedBy != claim.AiIdentifier {
				return fmt.Errorf("sync conflict: task %s already claimed by %s, cannot overwrite with claim from %s", claim.TaskID, t.ClaimedBy, claim.AiIdentifier)
			}

			if claim.Status == "active" {
				t.Status = "claimed"
			} else {
				t.Status = "submitted"
			}
			t.ClaimedBy = claim.AiIdentifier
			cc := claim.CreatedAt
			t.ClaimedAt = &cc
			ex := claim.ExpiresAt
			t.ClaimExpires = &ex
			t.ActiveClaimID = claim.ClaimID
			s.tasks[claim.TaskID] = t
		}
	}
	return nil
}

// SyncSubmission persists a submission from another instance.
func (s *MemoryStore) SyncSubmission(ctx context.Context, sub smart_contract.Submission) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Safeguard: Ensure internal fields cannot be overridden by external tool calls
	if sub.Deliverables != nil {
		delete(sub.Deliverables, "status")
	}
	if sub.CompletionProof != nil {
		delete(sub.CompletionProof, "status")
	}

	s.submissions[sub.SubmissionID] = sub

	return nil
}

// UpsertTask persists a single task update.
func (s *MemoryStore) UpsertTask(ctx context.Context, task smart_contract.Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Prevent overwriting claimed tasks with different claim information
	if existing, ok := s.tasks[task.TaskID]; ok {
		if strings.EqualFold(task.Status, "claimed") && task.ClaimedBy != "" {
			if existing.ClaimedBy != "" && existing.ClaimedBy != task.ClaimedBy {
				return fmt.Errorf("task %s already claimed by %s, cannot overwrite with claim from %s", task.TaskID, existing.ClaimedBy, task.ClaimedBy)
			}
		}
	}

	s.tasks[task.TaskID] = task
	return nil
}

// SyncEscortStatus persists escort validation results from another instance.
func (s *MemoryStore) SyncEscortStatus(ctx context.Context, status smart_contract.EscortStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.escortStatus[status.TaskID] = status
	return nil
}

// GetSubmission returns a submission by ID.
func (s *MemoryStore) GetSubmission(ctx context.Context, id string) (smart_contract.Submission, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sub, ok := s.submissions[id]
	if !ok {
		return smart_contract.Submission{}, fmt.Errorf("submission %s not found", id)
	}
	return sub, nil
}

// UpdateSubmissionStatus updates the status of a submission and related entities.
func (s *MemoryStore) UpdateSubmissionStatus(ctx context.Context, submissionID, status, reviewerNotes, rejectionType string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sub, ok := s.submissions[submissionID]
	if !ok {
		return ErrClaimNotFound // close enough
	}

	plan := DecideSubmissionStatusUpdate(status, reviewerNotes, rejectionType, time.Now())
	sub.Status = plan.Status
	sub.RejectionReason = plan.RejectionReason
	sub.RejectionType = plan.RejectionType
	sub.RejectedAt = plan.RejectedAt
	s.submissions[submissionID] = sub

	switch plan.Cascade {
	case SubmissionCascadeApprove:
		claim, ok := s.claims[sub.ClaimID]
		if !ok {
			return nil
		}
		claim.Status = plan.ClaimStatus
		s.claims[sub.ClaimID] = claim
		task, ok := s.tasks[claim.TaskID]
		if !ok {
			return nil
		}
		task.Status = plan.TaskStatus
		s.tasks[claim.TaskID] = task
	case SubmissionCascadeReject:
		claim, ok := s.claims[sub.ClaimID]
		if !ok {
			return nil
		}
		claim.Status = plan.ClaimStatus
		s.claims[sub.ClaimID] = claim
		task, ok := s.tasks[claim.TaskID]
		if ok {
			task.Status = plan.TaskStatus
			if plan.ClearClaimOnTask {
				task.ClaimedBy = ""
				task.ClaimedAt = nil
				task.ClaimExpires = nil
				task.ActiveClaimID = ""
			}
			s.tasks[claim.TaskID] = task
		}
	}

	return nil
}

// UpdateSubmission updates a full submission record with internal field protection.
func (s *MemoryStore) UpdateSubmission(ctx context.Context, sub smart_contract.Submission) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Safeguard: Ensure internal fields cannot be overridden by external tool calls
	if sub.Deliverables != nil {
		delete(sub.Deliverables, "status")
	}
	if sub.CompletionProof != nil {
		delete(sub.CompletionProof, "status")
	}

	if _, ok := s.submissions[sub.SubmissionID]; !ok {

		return fmt.Errorf("submission %s not found", sub.SubmissionID)
	}

	s.submissions[sub.SubmissionID] = sub
	return nil
}

func (s *MemoryStore) DeleteWish(ctx context.Context, visiblePixelHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	plan, err := BuildDeleteWishPlan(visiblePixelHash)
	if err != nil {
		return err
	}

	for id, p := range s.proposals {
		if p.VisiblePixelHash == plan.VisiblePixelHash {
			delete(s.proposals, id)
		}
	}

	taskIDs := make(map[string]bool)
	for id, t := range s.tasks {
		if t.ContractID == plan.WishID {
			taskIDs[id] = true
			delete(s.tasks, id)
		}
	}

	for id, c := range s.claims {
		if taskIDs[c.TaskID] {
			delete(s.claims, id)
			for sid, sub := range s.submissions {
				if sub.ClaimID == id {
					delete(s.submissions, sid)
				}
			}
		}
	}

	delete(s.contracts, plan.WishID)
	return nil
}

// CreateContractReworkRequest adds a rework request from the wish creator at contract level.
func (s *MemoryStore) CreateContractReworkRequest(ctx context.Context, contractID, requester, notes string) (smart_contract.ContractReworkRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	c, ok := s.contracts[contractID]
	if !ok {
		return smart_contract.ContractReworkRequest{}, fmt.Errorf("contract %s not found", contractID)
	}

	now := time.Now()
	reworkReq, err := BuildReworkRequest(contractID, requester, notes, now, "")
	if err != nil {
		return smart_contract.ContractReworkRequest{}, err
	}

	c.ReworkRequests = append(c.ReworkRequests, reworkReq)
	taskStatus := ReworkTaskStatusOnCreate()
	for tID, t := range s.tasks {
		if t.ContractID == contractID {
			t.Status = taskStatus
			s.tasks[tID] = t
		}
	}
	s.contracts[contractID] = c

	return reworkReq, nil
}

// GetContractReworkRequests returns all rework requests for a contract.
func (s *MemoryStore) GetContractReworkRequests(ctx context.Context, contractID string) ([]smart_contract.ContractReworkRequest, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	c, ok := s.contracts[contractID]
	if !ok {
		return nil, fmt.Errorf("contract %s not found", contractID)
	}

	return c.ReworkRequests, nil
}

// ResolveContractReworkRequest marks a rework request as resolved.
func (s *MemoryStore) ResolveContractReworkRequest(ctx context.Context, contractID, requestID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	c, ok := s.contracts[contractID]
	if !ok {
		return fmt.Errorf("contract %s not found", contractID)
	}

	found := false
	now := time.Now()
	for i, req := range c.ReworkRequests {
		if req.RequestID == requestID {
			c.ReworkRequests[i].Status = smart_contract.ReworkStatusResolved
			c.ReworkRequests[i].ResolvedAt = &now
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("rework request %s not found", requestID)
	}

	s.contracts[contractID] = c
	return nil
}
