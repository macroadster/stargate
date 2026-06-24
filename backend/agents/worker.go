package agents

import (
	"context"
	"fmt"
	"html"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"stargate-backend/core/smart_contract"
	scmiddleware "stargate-backend/middleware/smart_contract"
)

// Worker is responsible for discovering open wishes (contracts), creating proposals,
// claiming tasks, executing work via Executor, and submitting results.
type Worker struct {
	cfg      Config
	store    scmiddleware.Store
	executor Executor
	aiID     string
	donation string

	mu              sync.Mutex
	seenWishes      map[string]bool
	recentProposals map[string]bool
	activeTasks     map[string]bool   // taskID currently being worked on
	rejectedTasks   map[string]string // taskID -> last rejection reason
	state           *FileState
}

func NewWorker(cfg Config, store scmiddleware.Store, executor Executor) *Worker {
	if executor == nil {
		executor = NewAutoDetectExecutor(cfg.UploadsDir)
	}
	w := &Worker{
		cfg:             cfg,
		store:           store,
		executor:        executor,
		aiID:            cfg.AIIdentifier,
		donation:        cfg.DonationAddress,
		seenWishes:      make(map[string]bool),
		recentProposals: make(map[string]bool),
		activeTasks:     make(map[string]bool),
		rejectedTasks:   make(map[string]string),
		state:           NewFileState("worker_state_" + sanitizeID(cfg.AIIdentifier) + ".json"),
	}
	w.loadState()
	return w
}

func sanitizeID(id string) string {
	return strings.ToLower(strings.ReplaceAll(id, "-", "_"))
}

func (w *Worker) loadState() {
	if sets := w.state.GetSet("seen_wishes"); len(sets) > 0 {
		w.seenWishes = sets
	}
	if sets := w.state.GetSet("recent_proposals"); len(sets) > 0 {
		w.recentProposals = sets
	}
	if m := w.state.GetMap("rejected_tasks"); len(m) > 0 {
		w.rejectedTasks = m
	}
}

func (w *Worker) saveState() {
	w.state.PutSet("seen_wishes", w.seenWishes)
	w.state.PutSet("recent_proposals", w.recentProposals)
	w.state.PutMap("rejected_tasks", w.rejectedTasks)
	_ = w.state.Save()
}

// ProcessWishes scans for pending wishes and creates proposals (respecting all rate limits and dedup rules).
// This is the Go equivalent of the Python Worker.process_wishes().
func (w *Worker) ProcessWishes(ctx context.Context) {
	// Gate: do not create new proposals while we have active work
	if w.hasActiveWork() {
		return
	}

	// 1. Load proposals to compute per-wish counts and "mine"
	proposals, err := w.store.ListProposals(ctx, smart_contract.ProposalFilter{})
	if err != nil {
		log.Printf("agents/worker: list proposals failed: %v", err)
		return
	}

	myHash := w.aiID // we use aiID directly for metadata checks (simpler than sha256 of key)
	myProposalsForWish := make(map[string]bool)
	countPerWish := make(map[string]int)

	for _, p := range proposals {
		vph := strings.TrimPrefix(p.VisiblePixelHash, "wish-")
		if vph == "" {
			continue
		}
		countPerWish[vph]++

		if p.Metadata != nil {
			if c, ok := p.Metadata["creator_ai_identifier"].(string); ok && strings.EqualFold(c, myHash) {
				myProposalsForWish[vph] = true
			}
			if c, ok := p.Metadata["creator_api_key_hash"].(string); ok && strings.EqualFold(c, myHash) {
				myProposalsForWish[vph] = true
			}
		}
	}

	// 2. Get open wishes (pending contracts)
	contracts, err := w.store.ListContracts(smart_contract.ContractFilter{Status: "pending"})
	if err != nil {
		log.Printf("agents/worker: list open contracts failed: %v", err)
		return
	}

	created := 0
	maxPerCycle := w.cfg.MaxProposalsPerCycle
	if maxPerCycle <= 0 {
		maxPerCycle = 1
	}
	maxPerWish := w.cfg.MaxProposalsPerWish
	if maxPerWish <= 0 {
		maxPerWish = 5
	}

	for _, c := range contracts {
		if created >= maxPerCycle {
			break
		}
		wid := strings.TrimPrefix(c.ContractID, "wish-")
		if wid == "" {
			wid = c.ContractID
		}
		status := strings.ToLower(c.Status)
		if status != "pending" {
			continue
		}

		if myProposalsForWish[wid] {
			continue
		}
		if w.recentProposals[wid] {
			continue
		}
		if countPerWish[wid] >= maxPerWish {
			continue
		}

		// Create proposal
		title := "Proposal for: " + firstLine(c.Title)
		if len(title) > 100 {
			title = title[:97] + "..."
		}
		desc := "I propose to fulfill the wish: '" + c.Title + "' by executing a systematic implementation plan.\n\n### Task 1: Build Solution\nExecute the technical requirements to fulfill the original wish."

		prop := smart_contract.Proposal{
			ID:               "", // store will assign or we can generate
			Title:            title,
			DescriptionMD:    desc,
			VisiblePixelHash: wid,
			BudgetSats:       1000,
			Status:           "pending",
			CreatedAt:        time.Now(),
			Metadata: map[string]any{
				"creator_ai_identifier": w.aiID,
				"contract_id":           c.ContractID,
			},
		}

		if err := w.store.CreateProposal(ctx, prop); err != nil {
			log.Printf("agents/worker: failed to create proposal for %s: %v", c.ContractID, err)
			continue
		}

		myProposalsForWish[wid] = true
		w.recentProposals[wid] = true
		countPerWish[wid]++
		created++
		log.Printf("agents/worker: created proposal for wish %s", c.ContractID)

		w.saveState()
	}

	if created > 0 {
		log.Printf("agents/worker: created %d proposal(s) this cycle", created)
	}
}

func (w *Worker) hasActiveWork() bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	if len(w.activeTasks) > 0 {
		return true
	}

	// Check store for tasks we have claimed that are still in progress.
	// Query by both aiID and donation (if set) because we may claim "as" the donation address.
	toCheck := []string{w.aiID}
	if w.donation != "" && !strings.EqualFold(w.donation, w.aiID) {
		toCheck = append(toCheck, w.donation)
	}
	for _, cb := range toCheck {
		tasks, err := w.store.ListTasks(smart_contract.TaskFilter{
			ClaimedBy: cb,
			Limit:     50,
		})
		if err != nil {
			continue
		}
		for _, t := range tasks {
			st := strings.ToLower(t.Status)
			if st == "claimed" || st == "in_progress" || st == "rework" || st == "rejected" {
				if strings.EqualFold(t.ClaimedBy, w.aiID) || (w.donation != "" && strings.EqualFold(t.ClaimedBy, w.donation)) {
					// Skip expired claims so we don't treat stale work as "active" forever (blocks proposals, etc).
					if details := w.getTaskStatus(t.TaskID); details != nil {
						if cid := getString(details, "claim_id"); cid != "" {
							if cl, cerr := w.store.GetClaim(cid); cerr == nil {
								if cl.Status != "active" || time.Now().After(cl.ExpiresAt) {
									continue
								}
							}
						}
					}
					return true
				}
			}
		}
	}
	return false
}

// claimWallet returns the wallet/address to use when calling ClaimTask.
// When a donation address is configured, we claim "as" that address (it also
// gives auditor powers and is how some tasks get attributed to the agent).
func (w *Worker) claimWallet() string {
	if w.donation != "" {
		return w.donation
	}
	return w.aiID
}

func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if t != "" {
			return t
		}
	}
	return s
}

// ======================= Task Execution (Phase 3) =======================

// ProcessTask claims (or resumes) a task and starts background execution.
// Returns true if the task was successfully claimed and work started.
func (w *Worker) ProcessTask(task smart_contract.Task) bool {
	taskID := task.TaskID
	if taskID == "" {
		return false
	}

	w.mu.Lock()
	if len(w.activeTasks) > 0 {
		if _, busy := w.activeTasks[taskID]; !busy {
			w.mu.Unlock()
			return false // hoarding prevention
		}
	}
	if w.activeTasks[taskID] {
		w.mu.Unlock()
		return false
	}
	w.mu.Unlock()

	// Determine if this is rework
	rejectionFeedback := ""
	if task.Requirements != nil {
		if _, has := task.Requirements["_has_contract_rework"]; has {
			rejectionFeedback = "CONTRACT-LEVEL REWORK REQUEST"
		}
	}

	// Check local rejection cache
	w.mu.Lock()
	if reason, ok := w.rejectedTasks[taskID]; ok && rejectionFeedback == "" {
		rejectionFeedback = reason
	}
	w.mu.Unlock()

	// Try to get fresh status and rejection from store
	if details := w.getTaskStatus(taskID); details != nil {
		if r := getString(details, "rejection_reason"); r != "" {
			rejectionFeedback = r
			w.mu.Lock()
			w.rejectedTasks[taskID] = r
			w.mu.Unlock()
		}
		if r := getString(details, "last_rejection_reason"); r != "" && rejectionFeedback == "" {
			rejectionFeedback = r
		}
	}

	// Claim or resume
	claimID := ""
	status := strings.ToLower(task.Status)
	existingClaim := task.ActiveClaimID

	claimedBy := strings.ToLower(task.ClaimedBy)
	isOurs := claimedBy == strings.ToLower(w.aiID) || (w.donation != "" && claimedBy == strings.ToLower(w.donation))

	// Robust resume for tasks that are already claimed by us (including after a previous killed execution)
	// Also support "submitted" by us for retrying failed submissions.
	// IMPORTANT: never resume an *expired* claim for SubmitWork; let ClaimTask issue a fresh one instead.
	if (status == "claimed" || status == "rework" || status == "rejected" || status == "submitted") && isOurs {
		if existingClaim == "" {
			existingClaim = w.findMyExistingClaim(task)
		}
		if existingClaim == "" {
			// Fallback: query fresh task status which includes claim_id for active/submitted claims
			if details := w.getTaskStatus(taskID); details != nil {
				if c := getString(details, "claim_id"); c != "" {
					existingClaim = c
				}
			}
		}
		if existingClaim != "" {
			// Validate the candidate claim is still active and not expired.
			valid := false
			if cl, err := w.store.GetClaim(existingClaim); err == nil {
				if cl.Status == "active" && !time.Now().After(cl.ExpiresAt) {
					valid = true
				} else {
					log.Printf("agents/worker: discarding stale/expired claim %s (status=%s, expires_at=%v) for task %s; will re-claim", existingClaim, cl.Status, cl.ExpiresAt, taskID)
				}
			} else {
				log.Printf("agents/worker: GetClaim(%s) failed for task %s (%v); will re-claim", existingClaim, taskID, err)
			}
			if valid {
				claimID = existingClaim
				log.Printf("agents/worker: resuming task %s with existing claim %s", taskID, claimID)
			}
		}
	}

	if claimID == "" && (status == "available" || status == "rejected" || status == "rework" || ((status == "claimed" || status == "submitted") && isOurs)) {
		log.Printf("agents/worker: claiming/reclaiming task %s (rework=%v, status=%s)", taskID, rejectionFeedback != "", status)
		claim, err := w.store.ClaimTask(taskID, w.claimWallet(), nil)
		if err != nil {
			log.Printf("agents/worker: claim/reclaim failed for %s: %v", taskID, err)
			// Try to find if we already own it (non-expired)
			if existing := w.findMyExistingClaim(task); existing != "" {
				if cl, cerr := w.store.GetClaim(existing); cerr == nil && cl.Status == "active" && !time.Now().After(cl.ExpiresAt) {
					claimID = existing
				}
			}
		} else {
			claimID = claim.ClaimID
		}
	}

	if claimID == "" {
		// Last resort: even if not in our "resume" list, if it's ours try one more time via task status
		if isOurs {
			if details := w.getTaskStatus(taskID); details != nil {
				if c := getString(details, "claim_id"); c != "" {
					if cl, cerr := w.store.GetClaim(c); cerr == nil && cl.Status == "active" && !time.Now().After(cl.ExpiresAt) {
						claimID = c
						log.Printf("agents/worker: recovered claim_id %s for our task %s via TaskStatus", claimID, taskID)
					}
				}
			}
		}
	}

	// Extra safety: if a claimID made it here but is expired/invalid, drop it.
	// This prevents SubmitWork on expired claims (which always fails) and forces a fresh ClaimTask next cycle.
	if claimID != "" {
		if cl, err := w.store.GetClaim(claimID); err != nil || cl.Status != "active" || time.Now().After(cl.ExpiresAt) {
			log.Printf("agents/worker: final guard: dropping expired/invalid claim %s for %s", claimID, taskID)
			claimID = ""
		}
	}

	if claimID == "" {
		log.Printf("agents/worker: could not secure claim for task %s (status=%s, isOurs=%v)", taskID, status, isOurs)
		return false
	}

	// Mark active
	w.mu.Lock()
	w.activeTasks[taskID] = true
	w.mu.Unlock()

	// Enrich task with feedback for background
	if rejectionFeedback != "" {
		if task.Requirements == nil {
			task.Requirements = map[string]string{}
		}
		task.Requirements["rejection_feedback"] = rejectionFeedback
	}

	go w.runTaskBackground(task, claimID)

	return true
}

func (w *Worker) getTaskStatus(taskID string) map[string]interface{} {
	details, err := w.store.TaskStatus(taskID)
	if err != nil {
		return nil
	}
	return details
}

func (w *Worker) findMyExistingClaim(task smart_contract.Task) string {
	contractID := task.ContractID
	if contractID == "" {
		contractID = task.GoalID
	}
	if contractID == "" {
		return ""
	}
	tasks, err := w.store.ListTasks(smart_contract.TaskFilter{ContractID: contractID, Limit: 100})
	if err != nil {
		return ""
	}
	for _, t := range tasks {
		if t.TaskID == task.TaskID {
			cb := strings.ToLower(t.ClaimedBy)
			if cb == strings.ToLower(w.aiID) || (w.donation != "" && cb == strings.ToLower(w.donation)) {
				if t.ActiveClaimID != "" {
					// Only return if the claim is still valid
					if cl, cerr := w.store.GetClaim(t.ActiveClaimID); cerr == nil && cl.Status == "active" && !time.Now().After(cl.ExpiresAt) {
						return t.ActiveClaimID
					}
				}
			}
		}
	}

	// Fallback for sqlite (and others) where ListTasks scan does not populate ActiveClaimID:
	// TaskStatus does a direct latest claim query and returns "claim_id".
	if details := w.getTaskStatus(task.TaskID); details != nil {
		if c := getString(details, "claim_id"); c != "" {
			if cl, cerr := w.store.GetClaim(c); cerr == nil && cl.Status == "active" && !time.Now().After(cl.ExpiresAt) {
				return c
			}
		}
	}

	return ""
}

func (w *Worker) runTaskBackground(task smart_contract.Task, claimID string) {
	taskID := task.TaskID
	defer func() {
		w.mu.Lock()
		delete(w.activeTasks, taskID)
		w.mu.Unlock()
		// Clear rejection for this task after run
		w.mu.Lock()
		delete(w.rejectedTasks, taskID)
		w.mu.Unlock()
	}()

	deliverables := w.performWork(task)

	log.Printf("agents/worker: submitting work for task %s (claim %s)", taskID, claimID)
	if _, err := w.store.SubmitWork(claimID, deliverables, nil); err != nil {
		log.Printf("agents/worker: submit_work failed for %s: %v", taskID, err)
	} else {
		log.Printf("agents/worker: task %s submitted successfully", taskID)
		// Clear rejection cache on success
		w.mu.Lock()
		delete(w.rejectedTasks, taskID)
		w.mu.Unlock()
	}
}

func (w *Worker) performWork(task smart_contract.Task) map[string]interface{} {
	taskID := task.TaskID
	contractID := task.ContractID
	if contractID == "" {
		contractID = task.GoalID
	}
	visible := ""
	if task.MerkleProof != nil {
		visible = task.MerkleProof.VisiblePixelHash
	}
	if visible == "" {
		visible = contractID
	}
	visible = strings.TrimPrefix(visible, "wish-")

	title := task.Title
	desc := task.Description
	rejection := ""
	if task.Requirements != nil {
		rejection = task.Requirements["rejection_feedback"]
	}

	// Fetch proposal context
	proposalContext := ""
	if p, err := w.findProposalForTask(task); err == nil && p != nil {
		proposalContext = p.DescriptionMD
	}

	// Fetch rework requests
	reworkContext := ""
	if contractID != "" {
		if reqs, err := w.store.GetContractReworkRequests(context.Background(), strings.TrimPrefix(contractID, "wish-")); err == nil && len(reqs) > 0 {
			reworkContext = "\nCONTRACT-LEVEL REWORK REQUESTS:\n"
			for i, r := range reqs {
				reworkContext += fmt.Sprintf("%d. %s\n", i+1, r.Notes)
			}
		}
	}

	// Fetch previous work / submissions
	previousWork := ""
	if history := w.fetchSubmissionHistory(taskID, contractID); len(history) > 0 {
		latest := history[0]
		if d, ok := latest["deliverables"].(map[string]interface{}); ok {
			if n, ok := d["notes"].(string); ok {
				previousWork = n
			}
		}
	}

	// Build workdir
	base := w.cfg.UploadsDir
	if base == "" {
		base = os.Getenv("UPLOADS_DIR")
	}
	workdir := filepath.Join(base, "results", visible)
	os.MkdirAll(workdir, 0755)

	// Copy/inject AGENTS.md guide
	w.ensureAgentsGuide(workdir)

	// Prepare execution request
	req := ExecutionRequest{
		ContractID:        contractID,
		VisiblePixelHash:  visible,
		TaskID:            taskID,
		Title:             title,
		Description:       desc,
		ProposalContext:   proposalContext,
		PreviousWork:      previousWork,
		RejectionFeedback: rejection,
		Workdir:           workdir,
	}

	result, err := w.executor.Execute(context.Background(), req)
	if err != nil {
		log.Printf("agents/worker: executor failed for %s: %v", taskID, err)
		result = ExecutionResult{
			Notes:           fmt.Sprintf("Execution failed: %v\n\nTask: %s\nDescription: %s", err, title, desc),
			ResultFile:      fmt.Sprintf("/uploads/results/%s/%s.md", visible, taskID),
			ArtifactsDir:    fmt.Sprintf("/uploads/results/%s/", visible),
			CompletionProof: "error-" + taskID,
		}
	}

	// Write/ensure the report file
	reportPath := filepath.Join(workdir, taskID+".md")
	finalNotes := w.buildFinalReport(task, result.Notes, proposalContext)
	_ = os.WriteFile(reportPath, []byte(finalNotes), 0644)

	// Ensure nice frontend
	w.ensureFrontend(visible, workdir, finalNotes)

	publicURL := fmt.Sprintf("/uploads/results/%s/%s.md", visible, taskID)

	return map[string]interface{}{
		"notes":            finalNotes,
		"result_file":      publicURL,
		"artifacts_dir":    fmt.Sprintf("/uploads/results/%s/", visible),
		"completion_proof": result.CompletionProof,
	}
}

func (w *Worker) buildFinalReport(task smart_contract.Task, coreOutput, proposalContext string) string {
	title := task.Title
	taskID := task.TaskID
	proposalTitle := ""
	if p, _ := w.findProposalForTask(task); p != nil {
		proposalTitle = p.Title
	}

	notes := fmt.Sprintf(`# Task Report: %s

**Agent:** %s
**Proposal:** %s
**Task ID:** %s

## Implementation
%s

---
**Report:** [Download](/uploads/results/%s/%s.md)
`, title, w.aiID, proposalTitle, taskID, coreOutput, strings.TrimPrefix(task.ContractID, "wish-"), taskID)

	return notes
}

func (w *Worker) findProposalForTask(task smart_contract.Task) (*smart_contract.Proposal, error) {
	// Try by visible pixel hash / contract
	vph := ""
	if task.MerkleProof != nil {
		vph = task.MerkleProof.VisiblePixelHash
	}
	if vph == "" {
		vph = task.ContractID
	}
	vph = strings.TrimPrefix(vph, "wish-")

	props, err := w.store.ListProposals(context.Background(), smart_contract.ProposalFilter{ContractID: vph})
	if err == nil && len(props) > 0 {
		return &props[0], nil
	}

	// Try all and match
	all, _ := w.store.ListProposals(context.Background(), smart_contract.ProposalFilter{})
	for i := range all {
		if all[i].VisiblePixelHash == vph || all[i].ID == task.ContractID {
			return &all[i], nil
		}
	}
	return nil, fmt.Errorf("no proposal found")
}

func (w *Worker) fetchSubmissionHistory(taskID, contractID string) []map[string]interface{} {
	var taskIDs []string
	if taskID != "" {
		taskIDs = []string{taskID}
	} else if contractID != "" {
		tasks, _ := w.store.ListTasks(smart_contract.TaskFilter{ContractID: contractID, Limit: 50})
		for _, t := range tasks {
			taskIDs = append(taskIDs, t.TaskID)
		}
	}
	if len(taskIDs) == 0 {
		return nil
	}
	subs, err := w.store.ListSubmissions(context.Background(), taskIDs)
	if err != nil || len(subs) == 0 {
		return nil
	}
	// Convert to simple maps for previous work extraction (newest first)
	result := make([]map[string]interface{}, 0, len(subs))
	for i := len(subs) - 1; i >= 0; i-- { // rough reverse
		s := subs[i]
		m := map[string]interface{}{
			"submission_id": s.SubmissionID,
			"status":        s.Status,
			"deliverables":  s.Deliverables,
		}
		result = append(result, m)
	}
	return result
}

func (w *Worker) ensureAgentsGuide(dir string) {
	guidePath := filepath.Join(dir, "AGENTS.md")
	if _, err := os.Stat(guidePath); err == nil {
		return
	}

	content := `# AI Agent Working Guide (Stargate Built-in Agent)

You are working inside an isolated sandbox for a specific contract.

## Rules
- Work only inside this directory.
- Produce concrete, working results (code, html, docs).
- Always create or update index.html for navigation.
- Use memory.md for persistent context across tasks.
- Follow security: no external network, no dangerous ops unless explicitly needed for the task.

## Deliverables
- Implementation
- Evidence (tests, screenshots, running demo)
- index.html with links to all artifacts
- Clear summary in your final report

Good luck!
`
	_ = os.WriteFile(guidePath, []byte(content), 0644)
}

func (w *Worker) ensureFrontend(visible, dir, notes string) {
	indexPath := filepath.Join(dir, "index.html")
	if _, err := os.Stat(indexPath); err == nil {
		return
	}

	// Basic listing
	files, _ := os.ReadDir(dir)
	var links []string
	for _, f := range files {
		if f.Name() == "index.html" {
			continue
		}
		links = append(links, fmt.Sprintf(`<li><a href="%s">%s</a></li>`, f.Name(), f.Name()))
	}

	htmlContent := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
	<title>Project %s</title>
	<style>body { font-family: system-ui; margin: 2rem; max-width: 800px; } ul { line-height: 1.8; }</style>
</head>
<body>
	<h1>Project: %s</h1>
	<p>Generated by Stargate built-in agent.</p>
	<h2>Deliverables</h2>
	<ul>%s</ul>
	<div style="margin-top:2rem; padding:1rem; background:#f8f8f8; border-left:4px solid #3498db;">
		Report is also available as <a href="%s.md">%s.md</a>
	</div>
</body>
</html>`, html.EscapeString(visible), html.EscapeString(visible), strings.Join(links, ""), visible, visible)

	_ = os.WriteFile(indexPath, []byte(htmlContent), 0644)
}

// small helper
func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
