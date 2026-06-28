package smart_contract

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"stargate-backend/core/smart_contract"
	"stargate-backend/services"
	scstore "stargate-backend/storage/smart_contract"
)

func normalizeWishText(text string) string {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "#")
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	return strings.ToLower(strings.Join(strings.Fields(text), " "))
}

func preferCanonicalContractID(existing, candidate string) string {
	existing = strings.TrimSpace(existing)
	candidate = strings.TrimSpace(candidate)
	if existing == "" {
		return candidate
	}
	if candidate == "" {
		return existing
	}
	if strings.HasPrefix(existing, "wish-") && !strings.HasPrefix(candidate, "wish-") {
		return candidate
	}
	return existing
}

func looksLikeStegoManifestText(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "schema_version:") &&
		strings.Contains(lower, "proposal_id:") &&
		strings.Contains(lower, "visible_pixel_hash:")
}

func proposalVisibleHash(p smart_contract.Proposal) string {
	if strings.TrimSpace(p.VisiblePixelHash) != "" {
		return strings.TrimSpace(p.VisiblePixelHash)
	}
	if v, ok := p.Metadata["visible_pixel_hash"].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return ""
}

func (s *Server) requireWishForProposalCreation(ctx context.Context, proposal smart_contract.Proposal) error {
	if s.store == nil {
		return fmt.Errorf("wish store unavailable")
	}
	visible := proposalVisibleHash(proposal)
	if visible == "" {
		return fmt.Errorf("visible_pixel_hash is required to create proposal")
	}

	// Try both with and without wish prefix for flexibility
	wishID := scstore.ToWishID(visible)
	if _, err := s.store.GetContract(wishID); err != nil {
		// Try without prefix in case the contract was created differently
		if _, err2 := s.store.GetContract(visible); err2 != nil {
			return fmt.Errorf("wish not found for visible_pixel_hash (tried %s and %s): %v", wishID, visible, err)
		}
		// If found without prefix, update the wish ID for consistency
		wishID = visible
	}
	return nil
}

func (s *Server) requireWishForApproval(ctx context.Context, proposal smart_contract.Proposal) error {
	visible := proposalVisibleHash(proposal)
	if visible == "" {
		return fmt.Errorf("visible_pixel_hash is required for approval")
	}

	// Try both with and without wish prefix for flexibility
	wishID := scstore.ToWishID(visible)
	if _, err := s.store.GetContract(wishID); err != nil {
		// Try without prefix in case the contract was created differently
		if _, err2 := s.store.GetContract(visible); err2 != nil {
			return fmt.Errorf("wish not found for visible_pixel_hash (tried %s and %s): %v", wishID, visible, err)
		}
		// If found without prefix, update the wish ID for consistency
		wishID = visible
	}
	return nil
}

func proofConfirmed(proof *smart_contract.MerkleProof) bool {
	if proof == nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(proof.ConfirmationStatus), "confirmed") {
		return true
	}
	if proof.ConfirmedAt != nil {
		return true
	}
	return false
}

func proposalHasConfirmedProof(p smart_contract.Proposal) bool {
	for _, t := range p.Tasks {
		if proofConfirmed(t.MerkleProof) {
			return true
		}
	}
	return false
}

func proofsConfirmed(proofs []smart_contract.MerkleProof) bool {
	for i := range proofs {
		if proofConfirmed(&proofs[i]) {
			return true
		}
	}
	return false
}

func proposalMetaConfirmed(meta map[string]interface{}) bool {
	if meta == nil {
		return false
	}
	if txid, ok := meta["confirmed_txid"].(string); ok && strings.TrimSpace(txid) != "" {
		return true
	}
	if status, ok := meta["confirmation_status"].(string); ok && strings.EqualFold(strings.TrimSpace(status), "confirmed") {
		return true
	}
	if height, ok := meta["confirmed_height"].(float64); ok && height > 0 {
		return true
	}
	return false
}

// handleProposals supports listing, getting, and approving proposals.
func (s *Server) handleProposals(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/smart_contract/proposals")
	path = strings.Trim(path, "/")

	switch r.Method {
	case http.MethodPost:
		// POST /mcp/v1/proposals/{id}/approve is handled separately.
		parts := strings.Split(path, "/")
		if len(parts) == 2 && parts[1] == "approve" {
			id := parts[0]
			proposal, err := s.store.GetProposal(r.Context(), id)
			if err != nil {
				Error(w, http.StatusBadRequest, err.Error())
				return
			}
			if proposal.Metadata == nil {
				proposal.Metadata = map[string]interface{}{}
			}
			// NOTE: Do NOT backfill creator_wallet here from the approver's API key.
			// The proposal's creator_wallet should only be set during creation.
			// Stamping the approver's wallet would write incorrect data.
			if err := s.enforceCreatorApproval(r, proposal); err != nil {
				Error(w, http.StatusForbidden, err.Error())
				return
			}
			if err := s.requireWishForApproval(r.Context(), proposal); err != nil {
				Error(w, http.StatusBadRequest, err.Error())
				return
			}
			meta := proposal.Metadata
			if meta == nil {
				meta = map[string]interface{}{}
			}
			fundingMode := strings.ToLower(strings.TrimSpace(toString(meta["funding_mode"])))
			if fundingMode == "" && (looksLikeRaiseFund(proposal.Title) || looksLikeRaiseFund(proposal.DescriptionMD)) {
				fundingMode = "raise_fund"
				meta["funding_mode"] = fundingMode
			}
			if isRaiseFund(fundingMode) {
				payoutAddr := strings.TrimSpace(toString(meta["payout_address"]))
				fundingAddr := strings.TrimSpace(toString(meta["funding_address"]))
				if payoutAddr == "" || fundingAddr == "" {
					if s.apiKeys == nil {
						Error(w, http.StatusBadRequest, "missing payout address; API key wallet binding required for fundraiser")
						return
					}
					apiKey := strings.TrimSpace(r.Header.Get("X-API-Key"))
					rec, ok := s.apiKeys.Get(apiKey)
					if !ok || strings.TrimSpace(rec.Wallet) == "" {
						Error(w, http.StatusBadRequest, "missing payout address; API key wallet binding required for fundraiser")
						return
					}
					meta["payout_address"] = rec.Wallet
					meta["funding_address"] = rec.Wallet
				}
			}
			proposal.Metadata = meta
			if err := s.store.UpdateProposal(r.Context(), proposal); err != nil {
				Error(w, http.StatusBadRequest, err.Error())
				return
			}
			if len(proposal.Tasks) == 0 {
				desc := strings.TrimSpace(proposal.DescriptionMD)
				if desc != "" {
					if proposal.Metadata == nil {
						proposal.Metadata = map[string]interface{}{}
					}
					if _, ok := proposal.Metadata["embedded_message"].(string); !ok {
						proposal.Metadata["embedded_message"] = desc
					}
					visible := strings.TrimSpace(proposal.VisiblePixelHash)
					if visible == "" {
						visible = strings.TrimSpace(toString(proposal.Metadata["visible_pixel_hash"]))
					}
					proposal.Tasks = scstore.BuildTasksFromMarkdown(proposal.ID, desc, visible, proposal.BudgetSats, scstore.FundingAddressFromMeta(proposal.Metadata))
					if err := s.store.UpdateProposal(r.Context(), proposal); err != nil {
						Error(w, http.StatusBadRequest, err.Error())
						return
					}
				}
			}
			if err := s.store.ApproveProposal(r.Context(), id); err != nil {
				Error(w, http.StatusBadRequest, err.Error())
				return
			}
			// Publish tasks for this proposal if available.
			if err := s.PublishProposalTasks(r.Context(), id); err != nil {
				log.Printf("failed to publish tasks for proposal %s: %v", id, err)
			}
			visibleHash := strings.TrimSpace(proposal.VisiblePixelHash)
			if visibleHash == "" {
				visibleHash = strings.TrimSpace(toString(proposal.Metadata["visible_pixel_hash"]))
			}
			if visibleHash != "" {
				s.archiveWishContract(r.Context(), visibleHash)
			}

			// Approve the proposal first. Stego/IPFS replication (contract to peers)
			// is intentionally deferred until after PSBT is built. This ensures
			// wish hash, stego (product) hash, and AI artifact hashes are all
			// committed before remote nodes can ingest the full contract.
			s.recordEvent(smart_contract.Event{
				Type:      "approve",
				EntityID:  id,
				Actor:     "approver",
				Message:   "proposal approved",
				CreatedAt: time.Now(),
			})

			JSON(w, http.StatusOK, map[string]interface{}{
				"proposal_id": id,
				"status":      "approved",
				"message":     "Proposal approved.",
			})
			return
		}
		if len(parts) == 2 && parts[1] == "publish" {
			id := parts[0]
			if err := s.store.PublishProposal(r.Context(), id); err != nil {
				Error(w, http.StatusBadRequest, err.Error())
				return
			}
			s.recordEvent(smart_contract.Event{
				Type:      "publish",
				EntityID:  id,
				Actor:     "approver",
				Message:   "proposal published",
				CreatedAt: time.Now(),
			})
			JSON(w, http.StatusOK, map[string]interface{}{
				"proposal_id": id,
				"status":      "published",
				"message":     "Proposal published.",
			})
			return
		}

		// Create a proposal, optionally derived from a pending ingestion.
		if ct := r.Header.Get("Content-Type"); ct != "" && !strings.Contains(ct, "application/json") {
			Error(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
			return
		}

		// CRITICAL: Log entry for every request
		log.Printf("CRITICAL: HandleCreateProposal called at %s from %s, User-Agent: %s", time.Now().Format(time.RFC3339), r.RemoteAddr, r.Header.Get("User-Agent"))

		var body ProposalCreateBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			Error(w, http.StatusBadRequest, "invalid json")
			return
		}
		// If an ingestion_id is provided, pull message/token/budget from that pending record.
		if body.IngestionID != "" && s.ingestionSvc != nil {
			rec, err := s.ingestionSvc.Get(body.IngestionID)
			if err != nil {
				Error(w, http.StatusNotFound, "ingestion not found")
				return
			}
			proposal, err := BuildProposalFromIngestion(body, rec)
			if err != nil {
				Error(w, http.StatusBadRequest, err.Error())
				return
			}
			if err := s.requireWishForProposalCreation(r.Context(), proposal); err != nil {
				Error(w, http.StatusBadRequest, err.Error())
				return
			}
			metaContractID, _ := proposal.Metadata["contract_id"].(string)
			metaVisiblePixelHash, _ := proposal.Metadata["visible_pixel_hash"].(string)
			if strings.TrimSpace(metaContractID) == "" || strings.TrimSpace(metaVisiblePixelHash) == "" {
				Error(w, http.StatusBadRequest, "contract_id and visible_pixel_hash are required for proposal creation so the UI can display it; set both to the same 64-char hash if needed")
				return
			}
			applyCreatorWallet(proposal.Metadata, r.Header.Get("X-API-Key"), s.apiKeys)
			if err := s.store.CreateProposal(r.Context(), proposal); err != nil {
				Error(w, http.StatusBadRequest, err.Error())
				return
			}
			// Note: stego/IPFS replication is deferred until PSBT build (see PSBT handlers).
			// This prevents remote nodes from ingesting an actionable contract before
			// the payer has committed funding via PSBT (wish hash + product hash + payouts).
			s.recordEvent(smart_contract.Event{
				Type:      "proposal_create",
				EntityID:  proposal.ID,
				Actor:     "creator",
				Message:   "proposal created from ingestion",
				CreatedAt: time.Now(),
			})
			JSON(w, http.StatusCreated, map[string]interface{}{
				"proposal_id": proposal.ID,
				"status":      proposal.Status,
				"message":     "proposal created from pending ingestion",
			})
			return
		}

		// Manual creation path (wish must already exist).
		if strings.TrimSpace(body.Title) == "" {
			Error(w, http.StatusBadRequest, "title is required")
			return
		}
		if body.ID == "" {
			body.ID = "proposal-" + strconv.FormatInt(time.Now().UnixNano(), 10)
		}
		if body.Status == "" {
			body.Status = "pending"
		}
		if body.BudgetSats == 0 {
			body.BudgetSats = scstore.DefaultBudgetSats()
		}
		if body.Metadata == nil {
			body.Metadata = map[string]interface{}{}
		}
		applyCreatorWallet(body.Metadata, r.Header.Get("X-API-Key"), s.apiKeys)
		if body.ContractID != "" {
			body.Metadata["contract_id"] = body.ContractID
		}
		if strings.TrimSpace(body.VisiblePixelHash) != "" {
			body.Metadata["visible_pixel_hash"] = body.VisiblePixelHash
		}
		contractID := strings.TrimSpace(body.ContractID)
		if contractID == "" {
			if v, ok := body.Metadata["contract_id"].(string); ok {
				contractID = strings.TrimSpace(v)
			}
		}
		visiblePixelHash := strings.TrimSpace(body.VisiblePixelHash)
		if visiblePixelHash == "" {
			if v, ok := body.Metadata["visible_pixel_hash"].(string); ok {
				visiblePixelHash = strings.TrimSpace(v)
			}
		}
		if visiblePixelHash == "" {
			Error(w, http.StatusBadRequest, "visible_pixel_hash is required for proposal creation")
			return
		}
		if contractID == "" {
			contractID = visiblePixelHash
			body.Metadata["contract_id"] = contractID
		}
		if contractID != visiblePixelHash {
			Error(w, http.StatusBadRequest, "contract_id must match visible_pixel_hash for wish proposals")
			return
		}
		wishID := "wish-" + visiblePixelHash
		if _, err := s.store.GetContract(wishID); err != nil {
			Error(w, http.StatusNotFound, "wish not found for visible_pixel_hash")
			return
		}
		for i := range body.Tasks {
			if body.Tasks[i].TaskID == "" {
				body.Tasks[i].TaskID = body.ID + "-task-" + strconv.Itoa(i+1)
			}
			if body.Tasks[i].ContractID == "" {
				body.Tasks[i].ContractID = body.ID
			}
			if body.Tasks[i].Status == "" {
				body.Tasks[i].Status = "available"
			}
		}
		p := smart_contract.Proposal{
			ID:               body.ID,
			Title:            body.Title,
			DescriptionMD:    body.DescriptionMD,
			VisiblePixelHash: visiblePixelHash,
			BudgetSats:       body.BudgetSats,
			Status:           body.Status,
			CreatedAt:        time.Now(),
			Tasks:            body.Tasks,
			Metadata:         body.Metadata,
		}
		if err := s.store.CreateProposal(r.Context(), p); err != nil {
			Error(w, http.StatusBadRequest, err.Error())
			return
		}
		s.recordEvent(smart_contract.Event{
			Type:      "proposal_create",
			EntityID:  p.ID,
			Actor:     "creator",
			Message:   fmt.Sprintf("proposal created with %d tasks", len(p.Tasks)),
			CreatedAt: time.Now(),
		})
		JSON(w, http.StatusCreated, map[string]interface{}{
			"proposal_id": p.ID,
			"status":      p.Status,
			"tasks":       len(p.Tasks),
			"budget_sats": p.BudgetSats,
		})
		return
	case http.MethodPut, http.MethodPatch:
		parts := strings.Split(path, "/")
		if len(parts) < 1 || parts[0] == "" {
			Error(w, http.StatusBadRequest, "proposal id required")
			return
		}
		if ct := r.Header.Get("Content-Type"); ct != "" && !strings.Contains(ct, "application/json") {
			Error(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
			return
		}
		var body ProposalUpdateBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			Error(w, http.StatusBadRequest, "invalid json")
			return
		}
		id := parts[0]
		existing, err := s.store.GetProposal(r.Context(), id)
		if err != nil {
			Error(w, http.StatusNotFound, err.Error())
			return
		}
		if !strings.EqualFold(existing.Status, "pending") {
			Error(w, http.StatusBadRequest, fmt.Sprintf("proposal %s must be pending to update, current status: %s", id, existing.Status))
			return
		}
		updated := existing
		changed := false

		if body.Title != nil {
			if strings.TrimSpace(*body.Title) == "" {
				Error(w, http.StatusBadRequest, "title cannot be empty")
				return
			}
			updated.Title = *body.Title
			changed = true
		}
		if body.DescriptionMD != nil {
			updated.DescriptionMD = *body.DescriptionMD
			changed = true
		}
		if body.VisiblePixelHash != nil {
			if strings.TrimSpace(*body.VisiblePixelHash) == "" {
				Error(w, http.StatusBadRequest, "visible_pixel_hash cannot be empty")
				return
			}
			updated.VisiblePixelHash = strings.TrimSpace(*body.VisiblePixelHash)
			changed = true
		}
		if body.BudgetSats != nil {
			updated.BudgetSats = *body.BudgetSats
			changed = true
		}
		if body.Metadata != nil {
			updated.Metadata = copyMeta(*body.Metadata)
			changed = true
		}

		if updated.Metadata == nil {
			updated.Metadata = map[string]interface{}{}
		}
		if body.ContractID != nil && strings.TrimSpace(*body.ContractID) != "" {
			updated.Metadata["contract_id"] = strings.TrimSpace(*body.ContractID)
			changed = true
		}
		if strings.TrimSpace(updated.VisiblePixelHash) != "" {
			if vph, ok := updated.Metadata["visible_pixel_hash"].(string); !ok || strings.TrimSpace(vph) == "" {
				updated.Metadata["visible_pixel_hash"] = updated.VisiblePixelHash
			}
		}
		if metaContract, ok := updated.Metadata["contract_id"].(string); ok {
			metaContract = strings.TrimSpace(metaContract)
			if metaContract != "" {
				if metaHash, ok2 := updated.Metadata["visible_pixel_hash"].(string); ok2 {
					metaHash = strings.TrimSpace(metaHash)
					if metaHash != "" && metaHash != metaContract {
						Error(w, http.StatusBadRequest, "visible_pixel_hash must match contract_id when both are set")
						return
					}
				}
			}
		}

		if body.Tasks != nil {
			updated.Tasks = *body.Tasks
			contractID := contractIDFromMeta(updated.Metadata, updated.ID)
			for i := range updated.Tasks {
				if updated.Tasks[i].TaskID == "" {
					updated.Tasks[i].TaskID = updated.ID + "-task-" + strconv.Itoa(i+1)
				}
				if updated.Tasks[i].ContractID == "" && contractID != "" {
					updated.Tasks[i].ContractID = contractID
				}
				if updated.Tasks[i].Status == "" {
					updated.Tasks[i].Status = "available"
				}
			}
			changed = true
		}

		if !changed {
			Error(w, http.StatusBadRequest, "no updates provided")
			return
		}

		if err := s.store.UpdateProposal(r.Context(), updated); err != nil {
			Error(w, http.StatusBadRequest, err.Error())
			return
		}
		s.recordEvent(smart_contract.Event{
			Type:      "update",
			EntityID:  updated.ID,
			Actor:     "editor",
			Message:   "proposal updated",
			CreatedAt: time.Now(),
		})
		JSON(w, http.StatusOK, map[string]interface{}{
			"proposal_id": updated.ID,
			"status":      updated.Status,
			"message":     "Proposal updated.",
		})
		return
	case http.MethodGet:
		if path == "" {
			minBudget := int64FromQuery(r, "min_budget_sats", 0)
			limit := intFromQuery(r, "limit", 20)
			offset := intFromQuery(r, "offset", 0)

			// First get total count by fetching all matching proposals without limit
			countFilter := smart_contract.ProposalFilter{
				Status:     r.URL.Query().Get("status"),
				Skills:     splitCSV(r.URL.Query().Get("skills")),
				MinBudget:  minBudget,
				ContractID: r.URL.Query().Get("contract_id"),
			}
			allProposals, err := s.store.ListProposals(r.Context(), countFilter)
			if err != nil {
				Error(w, http.StatusInternalServerError, err.Error())
				return
			}
			if !includeConfirmed(r) {
				filtered := make([]smart_contract.Proposal, 0, len(allProposals))
				for _, p := range allProposals {
					if looksLikeStegoManifestText(p.DescriptionMD) {
						continue
					}
					if strings.EqualFold(strings.TrimSpace(p.Status), "rejected") {
						continue
					}
					filtered = append(filtered, p)
				}
				allProposals = filtered
			}
			total := len(allProposals)

			// Apply pagination
			filter := smart_contract.ProposalFilter{
				Status:     r.URL.Query().Get("status"),
				Skills:     splitCSV(r.URL.Query().Get("skills")),
				MinBudget:  minBudget,
				ContractID: r.URL.Query().Get("contract_id"),
				MaxResults: limit,
				Offset:     offset,
			}
			proposals, err := s.store.ListProposals(r.Context(), filter)
			if err != nil {
				Error(w, http.StatusInternalServerError, err.Error())
				return
			}
			if !includeConfirmed(r) {
				filtered := make([]smart_contract.Proposal, 0, len(proposals))
				for _, p := range proposals {
					if looksLikeStegoManifestText(p.DescriptionMD) {
						continue
					}
					if strings.EqualFold(strings.TrimSpace(p.Status), "rejected") {
						continue
					}
					filtered = append(filtered, p)
				}
				proposals = filtered
			}

			// hydrate tasks and submissions with current state from task store
			var taskIDs []string
			for _, p := range proposals {
				for _, t := range p.Tasks {
					taskIDs = append(taskIDs, t.TaskID)
				}
			}
			tasks, _ := s.store.ListTasks(smart_contract.TaskFilter{})
			taskByID := make(map[string]smart_contract.Task)
			for _, t := range tasks {
				taskByID[t.TaskID] = t
			}
			subs, _ := s.store.ListSubmissions(r.Context(), taskIDs)
			// Hydrate proposal tasks with current task state
			for i := range proposals {
				for j := range proposals[i].Tasks {
					if currentTask, ok := taskByID[proposals[i].Tasks[j].TaskID]; ok {
						proposals[i].Tasks[j] = currentTask
					}
				}
			}

			hasMore := offset+len(proposals) < total
			JSON(w, http.StatusOK, map[string]interface{}{
				"proposals":   proposals,
				"total":       total,
				"has_more":    hasMore,
				"limit":       limit,
				"offset":      offset,
				"submissions": subs,
			})
			return
		}
		// get single
		id := path
		p, err := s.store.GetProposal(r.Context(), id)
		if err != nil {
			Error(w, http.StatusNotFound, err.Error())
			return
		}
		JSON(w, http.StatusOK, p)
	default:
		Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// getStegoMethodForFilename determines the appropriate steganography method based on image format
func getStegoMethodFromFilename(filename string) string {
	// Default to lsb if we can't determine format
	defaultMethod := "lsb"

	// Try to determine from file extension
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".png":
		return "alpha"
	case ".jpg", ".jpeg":
		return "exif"
	case ".gif":
		return "palette"
	}

	return defaultMethod
}

// buildProposalFromIngestion derives a proposal from a pending ingestion record.
func BuildProposalFromIngestion(body ProposalCreateBody, rec *services.IngestionRecord) (smart_contract.Proposal, error) {
	meta := copyMeta(rec.Metadata)
	if meta == nil {
		meta = map[string]interface{}{}
	}
	// Ensure ingestion reference is present for traceability.
	meta["ingestion_id"] = rec.ID
	if body.ContractID != "" {
		meta["contract_id"] = body.ContractID
	}
	if em, ok := meta["embedded_message"].(string); ok && em != "" {
		// keep as-is
	} else {
		meta["embedded_message"] = ""
	}

	id := body.ID
	if id == "" {
		id = "proposal-" + rec.ID
	}
	title := body.Title
	if strings.TrimSpace(title) == "" {
		if em, _ := meta["embedded_message"].(string); em != "" {
			title = strings.Fields(em)[0]
			if title == "" {
				title = "Proposal " + rec.ID
			}
		} else {
			title = "Proposal " + rec.ID
		}
	}
	desc := body.DescriptionMD
	if desc == "" {
		if em, _ := meta["embedded_message"].(string); em != "" {
			desc = em
		}
	}
	budget := body.BudgetSats
	if budget == 0 {
		budget = budgetFromMeta(meta)
	}
	visible := body.VisiblePixelHash
	if visible == "" {
		// Use stego hash from metadata if available
		if stegoHash, ok := meta["visible_pixel_hash"].(string); ok && strings.TrimSpace(stegoHash) != "" {
			visible = stegoHash
		} else if rec.ImageBase64 != "" {
			if h, err := hashBase64(rec.ImageBase64); err == nil {
				visible = h
			}
		}
	}
	if strings.TrimSpace(visible) != "" {
		if vph, ok := meta["visible_pixel_hash"].(string); !ok || strings.TrimSpace(vph) == "" {
			meta["visible_pixel_hash"] = visible
		}
	}
	status := body.Status
	if status == "" {
		status = "pending"
	}

	tasks := body.Tasks
	if len(tasks) == 0 {
		if em, _ := meta["embedded_message"].(string); em != "" {
			tasks = scstore.BuildTasksFromMarkdown(id, em, visible, budget, scstore.FundingAddressFromMeta(meta))
		}
	}
	for i := range tasks {
		if tasks[i].TaskID == "" {
			tasks[i].TaskID = id + "-task-" + strconv.Itoa(i+1)
		}
		if tasks[i].ContractID == "" {
			tasks[i].ContractID = id
		}
		if tasks[i].Status == "" {
			tasks[i].Status = "available"
		}
	}

	p := smart_contract.Proposal{
		ID:               id,
		Title:            title,
		DescriptionMD:    desc,
		VisiblePixelHash: visible,
		BudgetSats:       budget,
		Status:           status,
		CreatedAt:        time.Now(),
		Tasks:            tasks,
		Metadata:         meta,
	}
	return p, nil
}

// handleSubmissions manages submission endpoints for review and rework.
func (s *Server) handleSubmissions(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/smart_contract/submissions")
	parts := strings.Split(strings.Trim(path, "/"), "/")

	switch r.Method {
	case http.MethodGet:
		if path == "" || path == "/" {
			// List submissions with optional filters
			contractID := r.URL.Query().Get("contract_id")
			taskIDs := splitCSV(r.URL.Query().Get("task_ids"))
			status := r.URL.Query().Get("status")

			var submissions []smart_contract.Submission
			var err error

			if len(taskIDs) > 0 {
				submissions, err = s.store.ListSubmissions(r.Context(), taskIDs)
			} else if contractID != "" {
				// Get tasks for contract, then submissions for those tasks
				tasks, err := s.store.ListTasks(smart_contract.TaskFilter{ContractID: contractID})
				if err != nil {
					Error(w, http.StatusInternalServerError, err.Error())
					return
				}
				taskIDs = make([]string, len(tasks))
				for i, task := range tasks {
					taskIDs[i] = task.TaskID
				}
				submissions, err = s.store.ListSubmissions(r.Context(), taskIDs)
			} else {
				// Get all tasks, then all submissions
				tasks, err := s.store.ListTasks(smart_contract.TaskFilter{})
				if err != nil {
					Error(w, http.StatusInternalServerError, err.Error())
					return
				}
				taskIDs = make([]string, len(tasks))
				for i, task := range tasks {
					taskIDs[i] = task.TaskID
				}
				submissions, err = s.store.ListSubmissions(r.Context(), taskIDs)
			}

			if err != nil {
				Error(w, http.StatusInternalServerError, err.Error())
				return
			}

			// Filter by status if provided
			if status != "" {
				filtered := make([]smart_contract.Submission, 0)
				for _, sub := range submissions {
					if strings.EqualFold(sub.Status, status) {
						filtered = append(filtered, sub)
					}
				}
				submissions = filtered
			}

			// Convert to map for easier frontend consumption
			submissionMap := make(map[string]smart_contract.Submission)
			for _, sub := range submissions {
				submissionMap[sub.SubmissionID] = sub
			}

			JSON(w, http.StatusOK, map[string]interface{}{
				"submissions": submissionMap,
				"total":       len(submissions),
			})
			return
		}

		// GET /mcp/v1/submissions/{submissionId}
		if len(parts) >= 1 && parts[0] != "" {
			submissionID := parts[0]
			log.Printf("GET submission by ID: %s", submissionID)

			// Use the efficient GetSubmission method instead of loading all submissions
			submission, err := s.store.GetSubmission(r.Context(), submissionID)
			if err != nil {
				log.Printf("Failed to get submission %s: %v", submissionID, err)
				Error(w, http.StatusInternalServerError, err.Error())
				return
			}

			// Check if submission was found
			if submission.SubmissionID != "" {
				log.Printf("Found submission: %s", submissionID)
				JSON(w, http.StatusOK, submission)
			} else {
				log.Printf("Submission not found: %s", submissionID)
				Error(w, http.StatusNotFound, "submission not found")
			}
			return
		}

		Error(w, http.StatusBadRequest, "invalid submission endpoint")
		return

	case http.MethodPost:
		if len(parts) >= 2 && parts[1] == "review" {
			// POST /mcp/v1/submissions/{submissionId}/review
			submissionID := parts[0]

			var body submissionReviewBody
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				Error(w, http.StatusBadRequest, "invalid json")
				return
			}

			if body.Action == "" {
				Error(w, http.StatusBadRequest, "action is required")
				return
			}

			// Validate action
			validActions := map[string]bool{
				"review":  true,
				"approve": true,
				"reject":  true,
			}
			if !validActions[body.Action] {
				Error(w, http.StatusBadRequest, "invalid action. must be: review, approve, or reject")
				return
			}

			// Update submission status
			var newStatus string
			switch body.Action {
			case "review":
				newStatus = "reviewed"
			case "approve":
				newStatus = "approved"
			case "reject":
				newStatus = "rejected"
			}

			ctx := r.Context()
			rejectionType := ""
			reviewNotes := ""
			if body.Action == "reject" {
				reviewNotes = body.Notes
				rejectionType = body.RejectionType
			}
			err := s.store.UpdateSubmissionStatus(ctx, submissionID, newStatus, reviewNotes, rejectionType)
			if err != nil {
				if strings.Contains(err.Error(), "not found") {
					Error(w, http.StatusNotFound, "submission not found")
					return
				}
				Error(w, http.StatusInternalServerError, err.Error())
				return
			}

			// Auto-resolve rework requests when submission is approved and all tasks are approved
			if newStatus == "approved" {
				submission, err := s.store.GetSubmission(ctx, submissionID)
				if err == nil && submission.TaskID != "" {
					// Get the contract ID from task
					task, err := s.store.GetTask(submission.TaskID)
					if err == nil && task.ContractID != "" {
						// Check if all tasks are approved
						tasks, err := s.store.ListTasks(smart_contract.TaskFilter{ContractID: task.ContractID})
						if err == nil {
							allApproved := true
							for _, t := range tasks {
								if t.Status != "approved" && t.Status != "published" {
									allApproved = false
									break
								}
							}
							// Auto-resolve open rework requests when all tasks are approved
							if allApproved {
								reworkReqs, err := s.store.GetContractReworkRequests(ctx, task.ContractID)
								if err == nil {
									for _, req := range reworkReqs {
										if req.Status == "open" {
											_ = s.store.ResolveContractReworkRequest(ctx, task.ContractID, req.RequestID)
										}
									}
								}
							}
						}
					}
				}
			}

			// Record event
			s.recordEvent(smart_contract.Event{
				Type:      "review",
				EntityID:  submissionID,
				Actor:     "reviewer",
				Message:   fmt.Sprintf("submission %s", body.Action),
				CreatedAt: time.Now(),
			})

			JSON(w, http.StatusOK, map[string]interface{}{
				"message":       fmt.Sprintf("submission %sd successfully", body.Action),
				"status":        newStatus,
				"submission_id": submissionID,
			})
			return
		}

		if len(parts) >= 2 && parts[1] == "rework" {
			// POST /mcp/v1/submissions/{submissionId}/rework
			submissionID := parts[0]

			var body submissionReworkBody
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				Error(w, http.StatusBadRequest, "invalid json")
				return
			}

			if body.Deliverables == nil && body.Notes == "" {
				Error(w, http.StatusBadRequest, "deliverables or notes must be provided")
				return
			}

			// Get the original submission to update it efficiently
			originalSubmission, err := s.store.GetSubmission(r.Context(), submissionID)
			if err != nil {
				log.Printf("Failed to get submission %s for rework: %v", submissionID, err)
				Error(w, http.StatusInternalServerError, err.Error())
				return
			}

			if originalSubmission.SubmissionID == "" {
				log.Printf("Submission not found for rework: %s", submissionID)
				Error(w, http.StatusNotFound, "submission not found")
				return
			}

			// Update deliverables if provided
			if body.Deliverables != nil {
				originalSubmission.Deliverables = body.Deliverables
			}

			// Add rework notes to deliverables
			if body.Notes != "" {
				if originalSubmission.Deliverables == nil {
					originalSubmission.Deliverables = make(map[string]interface{})
				}
				originalSubmission.Deliverables["rework_notes"] = body.Notes
				originalSubmission.Deliverables["reworked_at"] = time.Now().Format(time.RFC3339)
			}

			// Reset status to pending_review and save all changes
			ctx := r.Context()
			originalSubmission.Status = "pending_review"
			err = s.store.UpdateSubmission(ctx, originalSubmission)
			if err != nil {
				Error(w, http.StatusInternalServerError, err.Error())
				return
			}

			// Record event
			s.recordEvent(smart_contract.Event{
				Type:      "rework",
				EntityID:  submissionID,
				Actor:     "claimant",
				Message:   "submission reworked",
				CreatedAt: time.Now(),
			})

			JSON(w, http.StatusOK, map[string]interface{}{
				"message":       "rework submitted successfully",
				"status":        "pending_review",
				"submission_id": submissionID,
			})
			return
		}

		Error(w, http.StatusBadRequest, "invalid submission endpoint")
		return

	default:
		Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
}
