package smart_contract

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"stargate-backend/core/smart_contract"
	scstore "stargate-backend/storage/smart_contract"
)

// recordEvent appends an event to the in-memory log with a small bounded buffer.
func (s *Server) recordEvent(evt smart_contract.Event) {
	s.processEvent(evt, true)
}

func (s *Server) processEvent(evt smart_contract.Event, shouldPublish bool) {
	const maxEvents = 200
	if evt.CreatedAt.IsZero() {
		evt.CreatedAt = time.Now()
	}
	s.eventsMu.Lock()
	s.events = append([]smart_contract.Event{evt}, s.events...)
	if len(s.events) > maxEvents {
		s.events = s.events[:maxEvents]
	}
	s.eventsMu.Unlock()
	s.broadcastEvent(evt)

	if shouldPublish {
		go s.publishSyncEvent(evt)
	}

	// When the local oracle confirms a contract, download sandbox artifacts.
	if evt.Type == "contract_confirmed" && evt.EntityID != "" {
		go s.downloadSandboxArtifacts(context.Background(), evt.EntityID)
	}
}

// mergeSandboxMetadata propagates sandbox_hash (and sandbox_tarball_cid for
// backward compat) from a synced contract into the local proposal and contract
// metadata so downloadSandboxArtifacts can find the tarball by hash.
func (s *Server) mergeSandboxMetadata(ctx context.Context, c *smart_contract.Contract) {
	if c == nil || c.Metadata == nil {
		return
	}
	sandboxHash, _ := c.Metadata["sandbox_hash"].(string)
	sandboxCID, _ := c.Metadata["sandbox_tarball_cid"].(string)
	if strings.TrimSpace(sandboxHash) == "" && strings.TrimSpace(sandboxCID) == "" {
		return
	}

	// Merge into contract metadata.
	if local, err := s.store.GetContract(c.ContractID); err == nil {
		meta := local.Metadata
		if meta == nil {
			meta = map[string]interface{}{}
		}
		changed := false
		if h := strings.TrimSpace(sandboxHash); h != "" && strings.TrimSpace(toString(meta["sandbox_hash"])) == "" {
			meta["sandbox_hash"] = h
			changed = true
		}
		if cid := strings.TrimSpace(sandboxCID); cid != "" && strings.TrimSpace(toString(meta["sandbox_tarball_cid"])) == "" {
			meta["sandbox_tarball_cid"] = cid
			changed = true
		}
		if changed {
			local.Metadata = meta
			_ = s.store.UpsertContractWithTasks(ctx, local, nil)
			log.Printf("sandbox: merged sandbox metadata for contract %s (hash=%s)", c.ContractID, sandboxHash)
		}
	}

	// Also merge into the associated proposal if one exists.
	for _, id := range []string{c.ContractID, strings.TrimPrefix(scstore.NormalizeContractID(c.ContractID), "wish-")} {
		if p, err := s.store.GetProposal(ctx, id); err == nil {
			pmeta := p.Metadata
			if pmeta == nil {
				pmeta = map[string]interface{}{}
			}
			changed := false
			if h := strings.TrimSpace(sandboxHash); h != "" && strings.TrimSpace(toString(pmeta["sandbox_hash"])) == "" {
				pmeta["sandbox_hash"] = h
				changed = true
			}
			if cid := strings.TrimSpace(sandboxCID); cid != "" && strings.TrimSpace(toString(pmeta["sandbox_tarball_cid"])) == "" {
				pmeta["sandbox_tarball_cid"] = cid
				changed = true
			}
			if changed {
				_ = s.store.UpdateProposalMetadata(ctx, p.ID, pmeta)
			}
			break
		}
	}
}

func (s *Server) publishSyncEvent(evt smart_contract.Event) {
	// TEMPORARY FIX: Add basic rate limiting to prevent sync storms
	if evt.Type == "contract_upsert" || evt.Type == "publish" {
		// Add small delay to reduce rapid firing
		time.Sleep(100 * time.Millisecond)
	}

	ann := &syncAnnouncement{
		Type:  evt.Type,
		Event: &evt,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	switch evt.Type {
	case "claim", "task_proof_update":
		// EntityID is TaskID
		task, err := s.store.GetTask(evt.EntityID)
		if err == nil {
			ann.Task = &task
		} else {
			// For claim events, log error but still publish to maintain sync flow
			// The receiving instance will retry getting task data during reconciliation
			if evt.Type == "claim" {
				log.Printf("WARNING: Failed to get task %s for claim sync: %v", evt.EntityID, err)
			}
		}
	case "contract_confirmed":
		// EntityID is ContractID
		contract, err := s.store.GetContract(evt.EntityID)
		if err == nil {
			ann.Contract = &contract
		}
	case "submit":
		// EntityID is ClaimID. Wait, handleSubmissions says parts[0] is claimID but EntityID recorded is claimID.
		// Let's find the submission that was just created.
		// Actually, recordEvent is called after SubmitWork returns.
		// If EntityID is claimID, we might need to find the latest submission for it.
		// But in handleSubmissions:
		// s.recordEvent(smart_contract.Event{ Type: "submit", EntityID: claimID, ... })
		// We should probably find the latest submission for this claim.
		tasks, _ := s.store.ListTasks(smart_contract.TaskFilter{})
		taskIDs := make([]string, len(tasks))
		for i, t := range tasks {
			taskIDs[i] = t.TaskID
		}
		subs, _ := s.store.ListSubmissions(ctx, taskIDs)
		var latest *smart_contract.Submission
		for _, sub := range subs {
			if sub.ClaimID == evt.EntityID {
				if latest == nil || sub.CreatedAt.After(latest.CreatedAt) {
					s := sub
					latest = &s
				}
			}
		}
		if latest != nil {
			ann.Submission = latest
			// Also include the task because its status changed to "submitted"
			task, err := s.store.GetTask(latest.TaskID)
			if err == nil {
				ann.Task = &task
			}
		}
	case "review", "rework":
		// EntityID is submissionID
		sub, err := s.store.GetSubmission(ctx, evt.EntityID)
		if err == nil {
			ann.Submission = &sub
			// Also include the task because its status might have changed
			task, err := s.store.GetTask(sub.TaskID)
			if err == nil {
				ann.Task = &task
			}
		}
	case "approve", "publish", "update", "proposal_create":
		p, err := s.store.GetProposal(ctx, evt.EntityID)
		if err == nil {
			ann.Proposal = &p
		}
	}

	if err := s.PublishSyncAnnouncement(ctx, ann); err != nil {
		log.Printf("failed to publish sync announcement: %v", err)
	}
}

func (s *Server) ReconcileSyncAnnouncement(ctx context.Context, ann *syncAnnouncement) error {
	if ann == nil {
		return nil
	}

	log.Printf("Reconciling sync announcement: type=%s from issuer=%s", ann.Type, ann.Issuer)

	var err error
	switch ann.Type {
	case "approve", "publish", "update", "proposal_create":
		if ann.Proposal != nil {
			// Normalize proposal ID for sync: find by visible_pixel_hash to handle wish- prefix changes
			originalID := ann.Proposal.ID
			if ann.Proposal.VisiblePixelHash != "" {
				// Try to find existing proposal by visible_pixel_hash
				filter := smart_contract.ProposalFilter{
					ContractID: ann.Proposal.VisiblePixelHash,
					MaxResults: 1,
				}
				existing, findErr := s.store.ListProposals(ctx, filter)
				if findErr == nil && len(existing) > 0 {
					// Found existing proposal with same visible_pixel_hash, use local ID
					ann.Proposal.ID = existing[0].ID
					log.Printf("sync: normalizing proposal ID from %s to %s (visible_pixel_hash=%s)", originalID, ann.Proposal.ID, ann.Proposal.VisiblePixelHash)
				}
			}

			if ann.Type == "approve" {
				// For approve type, call ApproveProposal to ensure all validation and side effects
				err = s.store.ApproveProposal(ctx, ann.Proposal.ID)
				if err != nil {
					// If already approved or published, treat as success (idempotent sync)
					if strings.Contains(err.Error(), "already") && strings.Contains(err.Error(), "approved") {
						log.Printf("sync: proposal %s already approved locally", ann.Proposal.ID)
						err = nil
						// Still publish tasks to ensure consistency
						_ = s.PublishProposalTasks(ctx, ann.Proposal.ID)
					} else if strings.Contains(err.Error(), "already") && strings.Contains(err.Error(), "published") {
						log.Printf("sync: proposal %s already published locally", ann.Proposal.ID)
						err = nil
					}
				}
				if err == nil {
					// Publish tasks after approval
					_ = s.PublishProposalTasks(ctx, ann.Proposal.ID)
				}
			} else if ann.Type == "publish" {
				// For publish type, call PublishProposal
				err = s.store.PublishProposal(ctx, ann.Proposal.ID)
				if err != nil && strings.Contains(err.Error(), "must be approved") {
					// If proposal needs approval first, try to approve it
					if approveErr := s.store.ApproveProposal(ctx, ann.Proposal.ID); approveErr == nil {
						// Retry publish after approval
						err = s.store.PublishProposal(ctx, ann.Proposal.ID)
					}
				}
			} else {
				// For update and proposal_create, use CreateProposal (upsert)
				err = s.store.CreateProposal(ctx, *ann.Proposal)
			}
		}
	case "claim", "task_proof_update", "task_update":
		if ann.Task != nil {
			err = s.store.UpsertTask(ctx, *ann.Task)
		}
	case "contract_confirmed":
		if ann.Contract != nil {
			err = s.store.UpdateContractStatus(ctx, ann.Contract.ContractID, "confirmed")
			// Merge sandbox_hash from the synced contract metadata so the
			// local node can locate the tarball by hash in UPLOADS_DIR.
			s.mergeSandboxMetadata(ctx, ann.Contract)
			// Download sandbox artifacts now that the contract is confirmed.
			go s.downloadSandboxArtifacts(context.Background(), ann.Contract.ContractID)
		}
	case "submit":
		if ann.Submission != nil {
			err = s.store.SyncSubmission(ctx, *ann.Submission)
		}
		if ann.Task != nil {
			err = s.store.UpsertTask(ctx, *ann.Task)
		}
	case "contract_upsert":
		if ann.Contract != nil {
			// BUG FIX: Skip contract_upsert sync that has empty tasks, as this can reset task statuses to available
			// This prevents PSBT build from inadvertently resetting task status
			log.Printf("Skipping contract_upsert sync for contract %s (would reset task statuses)", ann.Contract.ContractID)
			err = nil
		}
	case "escort_validation":
		if ann.EscortStatus != nil {
			err = s.store.SyncEscortStatus(ctx, *ann.EscortStatus)
		}
	}

	if ann.Event != nil {
		s.processEvent(*ann.Event, false)
	}

	return err
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	filterType := strings.TrimSpace(r.URL.Query().Get("type"))
	filterActor := strings.TrimSpace(r.URL.Query().Get("actor"))
	filterEntity := strings.TrimSpace(r.URL.Query().Get("entity_id"))

	// SSE support
	if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		flusher, ok := w.(http.Flusher)
		if !ok {
			Error(w, http.StatusInternalServerError, "streaming unsupported")
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		// Send recent buffer first
		s.eventsMu.Lock()
		initial := make([]smart_contract.Event, len(s.events))
		copy(initial, s.events)
		s.eventsMu.Unlock()
		for i := len(initial) - 1; i >= 0; i-- { // oldest first
			if !eventMatches(initial[i], filterType, filterActor, filterEntity) {
				continue
			}
			b, _ := json.Marshal(initial[i])
			w.Write([]byte("event: mcp\n"))
			w.Write([]byte("data: " + string(b) + "\n\n"))
		}
		flusher.Flush()

		ch := make(chan smart_contract.Event, 10)
		s.listenersMu.Lock()
		s.listeners = append(s.listeners, ch)
		s.listenersMu.Unlock()

		notify := r.Context().Done()
		for {
			select {
			case <-notify:
				s.removeListener(ch)
				return
			case evt := <-ch:
				if !eventMatches(evt, filterType, filterActor, filterEntity) {
					continue
				}
				b, _ := json.Marshal(evt)
				w.Write([]byte("event: mcp\n"))
				w.Write([]byte("data: " + string(b) + "\n\n"))
				flusher.Flush()
			}
		}
	}

	limit := intFromQuery(r, "limit", 50)
	if limit < 0 {
		limit = 0
	}
	s.eventsMu.Lock()
	events := make([]smart_contract.Event, len(s.events))
	copy(events, s.events)
	s.eventsMu.Unlock()
	filtered := make([]smart_contract.Event, 0, len(events))
	for _, evt := range events {
		if eventMatches(evt, filterType, filterActor, filterEntity) {
			filtered = append(filtered, evt)
		}
	}
	if limit > 0 && limit < len(filtered) {
		filtered = filtered[:limit]
	}
	JSON(w, http.StatusOK, map[string]interface{}{
		"events": filtered,
		"total":  len(filtered),
	})
}

// broadcastEvent pushes an event to connected listeners without blocking.
func (s *Server) broadcastEvent(evt smart_contract.Event) {
	s.listenersMu.Lock()
	defer s.listenersMu.Unlock()
	for _, ch := range s.listeners {
		select {
		case ch <- evt:
		default:
			// drop if slow consumer
		}
	}
}

// tryPublishTasksForTaskID attempts to find a proposal that contains the given taskID and publish its tasks.
func (s *Server) tryPublishTasksForTaskID(ctx context.Context, taskID string) error {
	proposals, err := s.store.ListProposals(ctx, smart_contract.ProposalFilter{})
	if err != nil {
		return err
	}
	for _, p := range proposals {
		for _, t := range p.Tasks {
			if t.TaskID == taskID {
				return s.PublishProposalTasks(ctx, p.ID)
			}
		}
	}
	return ErrTaskNotFound
}

func (s *Server) removeListener(ch chan smart_contract.Event) {
	s.listenersMu.Lock()
	defer s.listenersMu.Unlock()
	for i, c := range s.listeners {
		if c == ch {
			close(c)
			s.listeners = append(s.listeners[:i], s.listeners[i+1:]...)
			break
		}
	}
}

func eventMatches(evt smart_contract.Event, t string, actor string, entity string) bool {
	if t != "" && !strings.EqualFold(evt.Type, t) {
		return false
	}
	if actor != "" && !strings.EqualFold(evt.Actor, actor) {
		return false
	}
	if entity != "" && evt.EntityID != entity {
		return false
	}
	return true
}

// PublishProposalTasks publishes the tasks stored in a proposal into MCP tasks.
func (s *Server) PublishProposalTasks(ctx context.Context, proposalID string) error {
	if s.eventSvc == nil {
		return nil
	}
	return s.eventSvc.PublishProposalTasks(ctx, proposalID)
}
