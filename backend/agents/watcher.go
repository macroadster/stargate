package agents

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"stargate-backend/core/smart_contract"
	scmiddleware "stargate-backend/middleware/smart_contract"
	scstore "stargate-backend/storage/smart_contract"
)

// Watcher audits proposals and submissions, and finds available tasks for the worker.
// This is the Go port of the Python WatcherAgent logic.
type Watcher struct {
	cfg      Config
	store    scmiddleware.Store
	aiID     string
	donation string

	mu                sync.Mutex
	seenProposals     map[string]bool
	rejectedProposals map[string]bool
	seenSubmissions   map[string]bool
	rejectionCache    map[string]string // proposal_id -> reason
	state             *FileState
}

func NewWatcher(cfg Config, store scmiddleware.Store) *Watcher {
	w := &Watcher{
		cfg:               cfg,
		store:             store,
		aiID:              cfg.AIIdentifier,
		donation:          cfg.DonationAddress,
		seenProposals:     make(map[string]bool),
		rejectedProposals: make(map[string]bool),
		seenSubmissions:   make(map[string]bool),
		rejectionCache:    make(map[string]string),
		state:             NewFileState("watcher_state_" + sanitizeID(cfg.AIIdentifier) + ".json"),
	}
	w.loadState()
	return w
}

func (w *Watcher) loadState() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if sets := w.state.GetSet("seen_proposals"); len(sets) > 0 {
		w.seenProposals = sets
	}
	if sets := w.state.GetSet("rejected_proposals"); len(sets) > 0 {
		w.rejectedProposals = sets
	}
	if sets := w.state.GetSet("seen_submissions"); len(sets) > 0 {
		w.seenSubmissions = sets
	}
	if m := w.state.GetMap("rejection_cache"); len(m) > 0 {
		w.rejectionCache = m
	}
}

func (w *Watcher) saveState() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.state.PutSet("seen_proposals", w.seenProposals)
	w.state.PutSet("rejected_proposals", w.rejectedProposals)
	w.state.PutSet("seen_submissions", w.seenSubmissions)
	w.state.PutMap("rejection_cache", w.rejectionCache)
	_ = w.state.Save()
}

// RunOnce runs the full watcher cycle (proposal audits + submission audits) and returns actionable tasks.
func (w *Watcher) RunOnce(ctx context.Context) []smart_contract.Task {
	if w == nil {
		return nil
	}
	// Light resource check (can be expanded)
	w.checkResources()

	w.processPendingProposals(ctx)
	w.processSubmissions(ctx)
	w.saveState()

	return w.findAvailableTasks(ctx)
}

// processPendingProposals audits pending proposals and rejects obviously bad ones.
// Good proposals are left pending (or auto-approved if we are configured as global auditor).
func (w *Watcher) processPendingProposals(ctx context.Context) {
	proposals, err := w.store.ListProposals(ctx, smart_contract.ProposalFilter{Status: "pending"})
	if err != nil {
		log.Printf("agents/watcher: list pending proposals failed: %v", err)
		return
	}
	if len(proposals) == 0 {
		return
	}

	// Cache open contracts for budget/scope checks
	openContracts, _ := w.store.ListContracts(smart_contract.ContractFilter{Status: "pending"})
	contractMap := make(map[string]smart_contract.Contract)
	for _, c := range openContracts {
		contractMap[c.ContractID] = c
		// Also index by visible hash variants
		if c.Metadata != nil {
			if vph, ok := c.Metadata["visible_pixel_hash"].(string); ok && vph != "" {
				contractMap["wish-"+strings.TrimPrefix(vph, "wish-")] = c
			}
		}
	}

	for _, p := range proposals {
		pid := p.ID
		if pid == "" {
			continue
		}
		status := strings.ToLower(p.Status)
		if status != "pending" {
			w.seenProposals[pid] = true
			continue
		}
		if w.seenProposals[pid] {
			continue
		}
		if w.rejectedProposals[pid] {
			w.seenProposals[pid] = true
			continue
		}

		// Budget check
		contractBudget := int64(0)
		if c, ok := contractMap[p.VisiblePixelHash]; ok {
			contractBudget = c.TotalBudgetSats
		} else if c, ok := contractMap["wish-"+p.VisiblePixelHash]; ok {
			contractBudget = c.TotalBudgetSats
		}

		if ok, reason := w.validateBudgetSanity(p, contractBudget); !ok {
			log.Printf("agents/watcher: rejecting proposal %s (budget): %s", pid, reason)
			w.rejectProposal(ctx, p, reason)
			continue
		}

		// Semantic / quality audit (heuristic + basic checks)
		if ok, reason := w.auditProposal(ctx, p); !ok {
			log.Printf("agents/watcher: rejecting proposal %s (audit): %s", pid, reason)
			w.rejectProposal(ctx, p, reason)
			continue
		}

		// Passed our audit
		log.Printf("agents/watcher: proposal %s passed audit", pid)
		w.seenProposals[pid] = true

		// Auto-approve if we are acting as global auditor (donation address configured)
		if w.shouldAutoApprove() {
			if err := w.approveProposal(ctx, pid); err != nil {
				log.Printf("agents/watcher: auto-approve failed for %s: %v", pid, err)
			} else {
				log.Printf("agents/watcher: auto-approved proposal %s", pid)
			}
		}
	}
}

func (w *Watcher) shouldAutoApprove() bool {
	return w.donation != ""
}

func (w *Watcher) rejectProposal(ctx context.Context, p smart_contract.Proposal, reason string) {
	pid := p.ID
	w.rejectedProposals[pid] = true
	w.seenProposals[pid] = true
	w.rejectionCache[pid] = reason

	// Persist rejection status when possible (makes it visible)
	p.Status = "rejected"
	if p.Metadata == nil {
		p.Metadata = map[string]any{}
	}
	p.Metadata["rejection_reason"] = reason
	p.Metadata["rejected_by"] = w.aiID
	p.Metadata["rejected_at"] = time.Now().Format(time.RFC3339)
	if err := w.store.UpdateProposal(ctx, p); err != nil {
		log.Printf("agents/watcher: failed to persist reject for proposal %s: %v", pid, err)
	}
}

func (w *Watcher) approveProposal(ctx context.Context, proposalID string) error {
	if err := w.store.ApproveProposal(ctx, proposalID); err != nil {
		return err
	}
	// Try to publish tasks so they become available
	p, err := w.store.GetProposal(ctx, proposalID)
	if err != nil {
		return err
	}
	if len(p.Tasks) == 0 {
		// Derive tasks from the markdown description (same as server publish path)
		fundingAddr := ""
		if p.Metadata != nil {
			if fa, ok := p.Metadata["funding_address"].(string); ok {
				fundingAddr = fa
			}
		}
		p.Tasks = scstore.BuildTasksFromMarkdown(p.ID, p.DescriptionMD, p.VisiblePixelHash, p.BudgetSats, fundingAddr)
	}
	if len(p.Tasks) > 0 {
		contractID := p.ID
		if p.VisiblePixelHash != "" {
			contractID = "wish-" + strings.TrimPrefix(p.VisiblePixelHash, "wish-")
		}
		contract := smart_contract.Contract{
			ContractID:          contractID,
			Title:               p.Title,
			TotalBudgetSats:     p.BudgetSats,
			GoalsCount:          1,
			AvailableTasksCount: len(p.Tasks),
			Status:              "active",
		}
		_ = w.store.UpsertContractWithTasks(ctx, contract, p.Tasks)
	}
	return nil
}

// validateBudgetSanity mirrors Python _validate_budget_sanity
func (w *Watcher) validateBudgetSanity(p smart_contract.Proposal, contractBudgetSats int64) (bool, string) {
	budget := p.BudgetSats
	if budget <= 0 {
		return false, "invalid budget: must be > 0"
	}
	if contractBudgetSats > 0 && budget > contractBudgetSats*10 {
		return false, "budget exceeds wish budget by more than 10x"
	}
	if budget > 100_000_000 { // > 1 BTC suspicious
		return false, "extremely high budget"
	}
	return true, ""
}

// auditProposal performs the main governance checks (recursive, scope, quality).
// Returns (pass, reason). Heuristic implementation (no LLM in v1).
func (w *Watcher) auditProposal(ctx context.Context, p smart_contract.Proposal) (bool, string) {
	// 1. Recursive / proxy proposal detection
	if w.isRecursiveProposal(p) {
		return false, "recursive proposal: appears to create another proposal without doing work"
	}

	// 2. Basic quality
	desc := p.DescriptionMD
	title := p.Title
	if len(desc) < 40 && !strings.Contains(desc, "###") {
		return false, "proposal lacks sufficient technical detail or structure"
	}
	if strings.Contains(strings.ToLower(title+desc), "joke") || strings.Contains(strings.ToLower(title+desc), "parody") || strings.Contains(strings.ToLower(title+desc), "test proposal") {
		return false, "appears to be joke, parody or test content"
	}

	// 3. Scope alignment with wish (lightweight)
	if ok, reason := w.validateScope(p); !ok {
		return false, reason
	}

	// 4. Structured content heuristic (has tasks or clear steps)
	if !strings.Contains(desc, "### Task") && !strings.Contains(desc, "- [ ]") && len(strings.Split(desc, "\n")) < 6 {
		return false, "proposal should contain structured implementation steps (e.g. ### Task sections)"
	}

	return true, "passed heuristic audit"
}

func (w *Watcher) isRecursiveProposal(p smart_contract.Proposal) bool {
	t := strings.ToLower(p.Title + " " + p.DescriptionMD)
	indicators := []string{
		"create a proposal", "submit a proposal", "make a proposal", "generate proposal",
		"proposal for the proposal", "create another proposal", "build a proposal",
	}
	for _, ind := range indicators {
		if strings.Contains(t, ind) {
			return true
		}
	}
	// Shallow restatement
	if strings.Contains(t, "fulfill the wish") && len(strings.Fields(p.DescriptionMD)) < 25 {
		return true
	}
	// Tasks that are just proposals
	for _, task := range p.Tasks {
		td := strings.ToLower(task.Title + " " + task.Description)
		if strings.Contains(td, "create proposal") || strings.Contains(td, "write a proposal") {
			return true
		}
	}
	return false
}

func (w *Watcher) validateScope(p smart_contract.Proposal) (bool, string) {
	// Try to find the originating wish text
	wishText := ""
	candidates := []string{p.VisiblePixelHash, "wish-" + p.VisiblePixelHash, p.ID}
	for _, cid := range candidates {
		if c, err := w.store.GetContract(cid); err == nil && c.Title != "" {
			wishText = c.Title
			break
		}
	}
	if wishText == "" {
		// Can't validate strictly
		return true, ""
	}

	// Very lightweight overlap
	wishWords := strings.Fields(strings.ToLower(wishText))
	propWords := strings.Fields(strings.ToLower(p.Title + " " + p.DescriptionMD))
	if len(wishWords) == 0 {
		return true, ""
	}
	overlap := 0
	for _, ww := range wishWords {
		for _, pw := range propWords {
			if ww == pw && len(ww) > 3 {
				overlap++
				break
			}
		}
	}
	ratio := float64(overlap) / float64(len(wishWords))
	if ratio < 0.1 {
		return false, "scope mismatch with original wish (low keyword overlap)"
	}
	return true, ""
}

// processSubmissions audits submitted/pending_review submissions and approves/reworks/rejects.
func (w *Watcher) processSubmissions(ctx context.Context) {
	// Collect candidate task IDs from recent proposals + all tasks (simplified)
	// For efficiency we scan active proposals and their tasks.
	proposals, err := w.store.ListProposals(ctx, smart_contract.ProposalFilter{})
	if err != nil {
		return
	}

	var allTaskIDs []string
	for _, p := range proposals {
		if p.Status == "approved" || p.Status == "published" || p.Status == "pending" {
			tasks, _ := w.store.ListTasks(smart_contract.TaskFilter{ContractID: p.VisiblePixelHash})
			for _, t := range tasks {
				allTaskIDs = append(allTaskIDs, t.TaskID)
			}
			// Also try proposal ID as contract
			if p.ID != "" {
				tasks, _ = w.store.ListTasks(smart_contract.TaskFilter{ContractID: p.ID})
				for _, t := range tasks {
					allTaskIDs = append(allTaskIDs, t.TaskID)
				}
			}
		}
	}

	subs, err := w.store.ListSubmissions(ctx, allTaskIDs)
	if err != nil {
		return
	}

	for _, sub := range subs {
		sid := sub.SubmissionID
		if sid == "" {
			continue
		}
		st := strings.ToLower(sub.Status)
		if (st == "submitted" || st == "pending_review") && !w.seenSubmissions[sid] {
			verdict, notes := w.auditSubmission(sub)
			w.seenSubmissions[sid] = true

			newStatus := "approved"
			rejectionType := ""
			if verdict == "REWORK" {
				newStatus = "reviewed"
			} else if verdict == "FAIL" {
				newStatus = "rejected"
				rejectionType = "audit"
			}

			if err := w.store.UpdateSubmissionStatus(ctx, sid, newStatus, notes, rejectionType); err != nil {
				log.Printf("agents/watcher: failed to update submission %s: %v", sid, err)
				continue
			}
			log.Printf("agents/watcher: submission %s -> %s", sid, newStatus)
		}
	}
}

// auditSubmission returns (VERDICT, notes). Heuristic only.
func (w *Watcher) auditSubmission(sub smart_contract.Submission) (string, string) {
	notes := ""
	if sub.Deliverables != nil {
		if n, ok := sub.Deliverables["notes"].(string); ok {
			notes = n
		}
	}
	lower := strings.ToLower(notes)

	if len(notes) < 80 {
		return "FAIL", "Submission too brief; needs concrete evidence of work"
	}
	// Look for signs of real work
	hasEvidence := strings.Contains(lower, "```") ||
		strings.Contains(lower, "http") ||
		strings.Contains(lower, "index.html") ||
		strings.Contains(lower, "def ") ||
		strings.Contains(lower, "function ") ||
		strings.Contains(lower, "class ") ||
		strings.Contains(lower, "test") ||
		len(notes) > 400

	if !hasEvidence {
		return "REWORK", "Add code, logs, screenshots, or a working demo/index.html to prove completion"
	}
	return "PASS", ""
}

// findAvailableTasks returns tasks that are available for our worker to claim or resume.
func (w *Watcher) findAvailableTasks(ctx context.Context) []smart_contract.Task {
	var available []smart_contract.Task

	// Scan proposals that can have work
	proposals, _ := w.store.ListProposals(ctx, smart_contract.ProposalFilter{})
	for _, p := range proposals {
		pstatus := strings.ToLower(p.Status)
		if pstatus != "available" && pstatus != "active" && pstatus != "approved" && pstatus != "published" {
			continue
		}

		// Get live tasks for the contract
		contractID := p.VisiblePixelHash
		if contractID == "" {
			contractID = p.ID
		}
		tasks, err := w.store.ListTasks(smart_contract.TaskFilter{ContractID: contractID})
		if err != nil || len(tasks) == 0 {
			// fallback to proposal ID
			tasks, _ = w.store.ListTasks(smart_contract.TaskFilter{ContractID: p.ID})
		}

		// Check for open contract rework requests
		reworkReqs, _ := w.store.GetContractReworkRequests(ctx, strings.TrimPrefix(contractID, "wish-"))
		hasOpenRework := false
		for _, r := range reworkReqs {
			if strings.ToLower(r.Status) == "open" {
				hasOpenRework = true
				break
			}
		}

		for _, task := range tasks {
			status := strings.ToLower(task.Status)
			claimedBy := strings.ToLower(task.ClaimedBy)

			isOurs := claimedBy == strings.ToLower(w.aiID) || (w.donation != "" && claimedBy == strings.ToLower(w.donation))
			claimedByOthers := claimedBy != "" && !isOurs

			isActionable := status == "available" || status == "rejected" || status == "rework" || (status == "claimed" && isOurs)

			if claimedByOthers || !isActionable {
				continue
			}

			// Enrich task for worker (use Requirements for extra signals)
			if hasOpenRework {
				if task.Requirements == nil {
					task.Requirements = map[string]string{}
				}
				task.Requirements["_has_contract_rework"] = "true"
			}
			// Worker can refetch proposal context when needed using task.ContractID / proposal flow
			available = append(available, task)
		}
	}

	if len(available) > 0 {
		log.Printf("agents/watcher: found %d actionable task(s)", len(available))
	}
	return available
}

func (w *Watcher) checkResources() {
	// Lightweight: in real impl we could stat disk. For now no-op.
}

