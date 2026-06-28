package services

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"stargate-backend/core/smart_contract"
	appservices "stargate-backend/services"
	auth "stargate-backend/storage/auth"
	scstore "stargate-backend/storage/smart_contract"
)

// ProposalCreateInput is the body for creating proposals.
type ProposalCreateInput struct {
	ID               string
	IngestionID      string
	ContractID       string
	Title            string
	DescriptionMD    string
	VisiblePixelHash string
	BudgetSats       int64
	Status           string
	Metadata         map[string]interface{}
	Tasks            []smart_contract.Task
	APIKey           string
}

// ProposalUpdateInput is the body for updating proposals.
type ProposalUpdateInput struct {
	Title            *string
	DescriptionMD    *string
	VisiblePixelHash *string
	BudgetSats       *int64
	ContractID       *string
	Metadata         *map[string]interface{}
	Tasks            *[]smart_contract.Task
}

// ProposalListQuery holds list filters and pagination.
type ProposalListQuery struct {
	Status           string
	Skills           []string
	MinBudget        int64
	ContractID       string
	Limit            int
	Offset           int
	IncludeConfirmed bool
}

// ProposalListResult is a paginated proposal list with submissions.
type ProposalListResult struct {
	Proposals   []smart_contract.Proposal
	Total       int
	HasMore     bool
	Limit       int
	Offset      int
	Submissions []smart_contract.Submission
}

// ProposalService encapsulates proposal domain operations.
type ProposalService struct {
	store        scstore.Store
	ingestionSvc *appservices.IngestionService
	apiKeys      auth.APIKeyValidator
	record       EventRecorder
	publishTasks func(ctx context.Context, proposalID string) error
	archiveWish  func(ctx context.Context, visibleHash string)
}

// NewProposalService constructs a ProposalService.
func NewProposalService(
	store scstore.Store,
	ingestionSvc *appservices.IngestionService,
	apiKeys auth.APIKeyValidator,
	record EventRecorder,
	publishTasks func(ctx context.Context, proposalID string) error,
	archiveWish func(ctx context.Context, visibleHash string),
) *ProposalService {
	return &ProposalService{
		store:        store,
		ingestionSvc: ingestionSvc,
		apiKeys:      apiKeys,
		record:       record,
		publishTasks: publishTasks,
		archiveWish:  archiveWish,
	}
}

// SetRecorder updates the event sink.
func (s *ProposalService) SetRecorder(record EventRecorder) { s.record = record }

// SetPublishTasks wires task publishing (typically EventService.PublishProposalTasks).
func (s *ProposalService) SetPublishTasks(fn func(ctx context.Context, proposalID string) error) {
	s.publishTasks = fn
}

// SetArchiveWish wires wish archival after approval.
func (s *ProposalService) SetArchiveWish(fn func(ctx context.Context, visibleHash string)) {
	s.archiveWish = fn
}

func (s *ProposalService) emit(evt smart_contract.Event) {
	if s.record != nil {
		s.record(evt)
	}
}

// Approve approves a proposal and publishes tasks.
func (s *ProposalService) Approve(ctx context.Context, id, apiKey string, creatorOK bool) (map[string]interface{}, error) {
	if s.store == nil {
		return nil, Fail(http.StatusBadRequest, "store unavailable")
	}
	proposal, err := s.store.GetProposal(ctx, id)
	if err != nil {
		return nil, Fail(http.StatusBadRequest, err.Error())
	}
	if proposal.Metadata == nil {
		proposal.Metadata = map[string]interface{}{}
	}
	if !creatorOK {
		return nil, Fail(http.StatusForbidden, "creator approval required")
	}
	if err := s.requireWishForApproval(ctx, proposal); err != nil {
		return nil, Fail(http.StatusBadRequest, err.Error())
	}
	meta := proposal.Metadata
	if meta == nil {
		meta = map[string]interface{}{}
	}
	fundingMode := strings.ToLower(strings.TrimSpace(metaString(meta, "funding_mode")))
	if fundingMode == "" && (LooksLikeRaiseFund(proposal.Title) || LooksLikeRaiseFund(proposal.DescriptionMD)) {
		fundingMode = "raise_fund"
		meta["funding_mode"] = fundingMode
	}
	if IsRaiseFund(fundingMode) {
		payoutAddr := strings.TrimSpace(metaString(meta, "payout_address"))
		fundingAddr := strings.TrimSpace(metaString(meta, "funding_address"))
		if payoutAddr == "" || fundingAddr == "" {
			if s.apiKeys == nil {
				return nil, Fail(http.StatusBadRequest, "missing payout address; API key wallet binding required for fundraiser")
			}
			rec, ok := s.apiKeys.Get(strings.TrimSpace(apiKey))
			if !ok || strings.TrimSpace(rec.Wallet) == "" {
				return nil, Fail(http.StatusBadRequest, "missing payout address; API key wallet binding required for fundraiser")
			}
			meta["payout_address"] = rec.Wallet
			meta["funding_address"] = rec.Wallet
		}
	}
	proposal.Metadata = meta
	if err := s.store.UpdateProposal(ctx, proposal); err != nil {
		return nil, Fail(http.StatusBadRequest, err.Error())
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
				visible = strings.TrimSpace(metaString(proposal.Metadata, "visible_pixel_hash"))
			}
			proposal.Tasks = scstore.BuildTasksFromMarkdown(proposal.ID, desc, visible, proposal.BudgetSats, scstore.FundingAddressFromMeta(proposal.Metadata))
			if err := s.store.UpdateProposal(ctx, proposal); err != nil {
				return nil, Fail(http.StatusBadRequest, err.Error())
			}
		}
	}
	if err := s.store.ApproveProposal(ctx, id); err != nil {
		return nil, Fail(http.StatusBadRequest, err.Error())
	}
	if s.publishTasks != nil {
		if err := s.publishTasks(ctx, id); err != nil {
			log.Printf("failed to publish tasks for proposal %s: %v", id, err)
		}
	}
	visibleHash := strings.TrimSpace(proposal.VisiblePixelHash)
	if visibleHash == "" {
		visibleHash = strings.TrimSpace(metaString(proposal.Metadata, "visible_pixel_hash"))
	}
	if visibleHash != "" && s.archiveWish != nil {
		s.archiveWish(ctx, visibleHash)
	}
	s.emit(smart_contract.Event{
		Type: "approve", EntityID: id, Actor: "approver",
		Message: "proposal approved", CreatedAt: time.Now(),
	})
	return map[string]interface{}{
		"proposal_id": id,
		"status":      "approved",
		"message":     "Proposal approved.",
	}, nil
}

// Publish marks a proposal published.
func (s *ProposalService) Publish(ctx context.Context, id string) (map[string]interface{}, error) {
	if err := s.store.PublishProposal(ctx, id); err != nil {
		return nil, Fail(http.StatusBadRequest, err.Error())
	}
	s.emit(smart_contract.Event{
		Type: "publish", EntityID: id, Actor: "approver",
		Message: "proposal published", CreatedAt: time.Now(),
	})
	return map[string]interface{}{
		"proposal_id": id,
		"status":      "published",
		"message":     "Proposal published.",
	}, nil
}

// Create creates a proposal (from ingestion or manually).
func (s *ProposalService) Create(ctx context.Context, body ProposalCreateInput) (map[string]interface{}, int, error) {
	if body.IngestionID != "" && s.ingestionSvc != nil {
		rec, err := s.ingestionSvc.Get(body.IngestionID)
		if err != nil {
			return nil, 0, Fail(http.StatusNotFound, "ingestion not found")
		}
		proposal, err := BuildProposalFromIngestion(body, rec)
		if err != nil {
			return nil, 0, Fail(http.StatusBadRequest, err.Error())
		}
		if err := s.requireWishForCreation(ctx, proposal); err != nil {
			return nil, 0, Fail(http.StatusBadRequest, err.Error())
		}
		metaContractID, _ := proposal.Metadata["contract_id"].(string)
		metaVisiblePixelHash, _ := proposal.Metadata["visible_pixel_hash"].(string)
		if strings.TrimSpace(metaContractID) == "" || strings.TrimSpace(metaVisiblePixelHash) == "" {
			return nil, 0, Fail(http.StatusBadRequest, "contract_id and visible_pixel_hash are required for proposal creation so the UI can display it; set both to the same 64-char hash if needed")
		}
		applyCreatorWallet(proposal.Metadata, body.APIKey, s.apiKeys)
		if err := s.store.CreateProposal(ctx, proposal); err != nil {
			return nil, 0, Fail(http.StatusBadRequest, err.Error())
		}
		s.emit(smart_contract.Event{
			Type: "proposal_create", EntityID: proposal.ID, Actor: "creator",
			Message: "proposal created from ingestion", CreatedAt: time.Now(),
		})
		return map[string]interface{}{
			"proposal_id": proposal.ID,
			"status":      proposal.Status,
			"message":     "proposal created from pending ingestion",
		}, http.StatusCreated, nil
	}

	if strings.TrimSpace(body.Title) == "" {
		return nil, 0, Fail(http.StatusBadRequest, "title is required")
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
	applyCreatorWallet(body.Metadata, body.APIKey, s.apiKeys)
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
		return nil, 0, Fail(http.StatusBadRequest, "visible_pixel_hash is required for proposal creation")
	}
	if contractID == "" {
		contractID = visiblePixelHash
		body.Metadata["contract_id"] = contractID
	}
	if contractID != visiblePixelHash {
		return nil, 0, Fail(http.StatusBadRequest, "contract_id must match visible_pixel_hash for wish proposals")
	}
	wishID := "wish-" + visiblePixelHash
	if _, err := s.store.GetContract(wishID); err != nil {
		return nil, 0, Fail(http.StatusNotFound, "wish not found for visible_pixel_hash")
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
		ID: body.ID, Title: body.Title, DescriptionMD: body.DescriptionMD,
		VisiblePixelHash: visiblePixelHash, BudgetSats: body.BudgetSats,
		Status: body.Status, CreatedAt: time.Now(), Tasks: body.Tasks, Metadata: body.Metadata,
	}
	if err := s.store.CreateProposal(ctx, p); err != nil {
		return nil, 0, Fail(http.StatusBadRequest, err.Error())
	}
	s.emit(smart_contract.Event{
		Type: "proposal_create", EntityID: p.ID, Actor: "creator",
		Message: fmt.Sprintf("proposal created with %d tasks", len(p.Tasks)), CreatedAt: time.Now(),
	})
	return map[string]interface{}{
		"proposal_id": p.ID, "status": p.Status, "tasks": len(p.Tasks), "budget_sats": p.BudgetSats,
	}, http.StatusCreated, nil
}

// Update applies a partial update to a pending proposal.
func (s *ProposalService) Update(ctx context.Context, id string, body ProposalUpdateInput) (map[string]interface{}, error) {
	existing, err := s.store.GetProposal(ctx, id)
	if err != nil {
		return nil, Fail(http.StatusNotFound, err.Error())
	}
	if !strings.EqualFold(existing.Status, "pending") {
		return nil, Fail(http.StatusBadRequest, fmt.Sprintf("proposal %s must be pending to update, current status: %s", id, existing.Status))
	}
	updated := existing
	changed := false
	if body.Title != nil {
		if strings.TrimSpace(*body.Title) == "" {
			return nil, Fail(http.StatusBadRequest, "title cannot be empty")
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
			return nil, Fail(http.StatusBadRequest, "visible_pixel_hash cannot be empty")
		}
		updated.VisiblePixelHash = strings.TrimSpace(*body.VisiblePixelHash)
		changed = true
	}
	if body.BudgetSats != nil {
		updated.BudgetSats = *body.BudgetSats
		changed = true
	}
	if body.Metadata != nil {
		updated.Metadata = copyMetaMap(*body.Metadata)
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
					return nil, Fail(http.StatusBadRequest, "visible_pixel_hash must match contract_id when both are set")
				}
			}
		}
	}
	if body.Tasks != nil {
		updated.Tasks = *body.Tasks
		contractID := ContractIDFromMeta(updated.Metadata, updated.ID)
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
		return nil, Fail(http.StatusBadRequest, "no updates provided")
	}
	if err := s.store.UpdateProposal(ctx, updated); err != nil {
		return nil, Fail(http.StatusBadRequest, err.Error())
	}
	s.emit(smart_contract.Event{
		Type: "update", EntityID: updated.ID, Actor: "editor",
		Message: "proposal updated", CreatedAt: time.Now(),
	})
	return map[string]interface{}{
		"proposal_id": updated.ID, "status": updated.Status, "message": "Proposal updated.",
	}, nil
}

// List returns paginated proposals with optional filtering.
func (s *ProposalService) List(ctx context.Context, q ProposalListQuery) (*ProposalListResult, error) {
	countFilter := smart_contract.ProposalFilter{
		Status: q.Status, Skills: q.Skills, MinBudget: q.MinBudget, ContractID: q.ContractID,
	}
	allProposals, err := s.store.ListProposals(ctx, countFilter)
	if err != nil {
		return nil, Fail(http.StatusInternalServerError, err.Error())
	}
	if !q.IncludeConfirmed {
		allProposals = filterListedProposals(allProposals)
	}
	total := len(allProposals)
	filter := smart_contract.ProposalFilter{
		Status: q.Status, Skills: q.Skills, MinBudget: q.MinBudget, ContractID: q.ContractID,
		MaxResults: q.Limit, Offset: q.Offset,
	}
	proposals, err := s.store.ListProposals(ctx, filter)
	if err != nil {
		return nil, Fail(http.StatusInternalServerError, err.Error())
	}
	if !q.IncludeConfirmed {
		proposals = filterListedProposals(proposals)
	}
	taskByID := map[string]smart_contract.Task{}
	if tasks, err := s.store.ListTasks(smart_contract.TaskFilter{}); err == nil {
		for _, t := range tasks {
			taskByID[t.TaskID] = t
		}
	}
	var taskIDs []string
	for i := range proposals {
		for j := range proposals[i].Tasks {
			tid := proposals[i].Tasks[j].TaskID
			taskIDs = append(taskIDs, tid)
			if currentTask, ok := taskByID[tid]; ok {
				proposals[i].Tasks[j] = currentTask
			}
		}
	}
	subs, _ := s.store.ListSubmissions(ctx, taskIDs)
	return &ProposalListResult{
		Proposals: proposals, Total: total, HasMore: q.Offset+len(proposals) < total,
		Limit: q.Limit, Offset: q.Offset, Submissions: subs,
	}, nil
}

// Get returns a single proposal.
func (s *ProposalService) Get(ctx context.Context, id string) (smart_contract.Proposal, error) {
	p, err := s.store.GetProposal(ctx, id)
	if err != nil {
		return smart_contract.Proposal{}, Fail(http.StatusNotFound, err.Error())
	}
	return p, nil
}

func filterListedProposals(in []smart_contract.Proposal) []smart_contract.Proposal {
	out := make([]smart_contract.Proposal, 0, len(in))
	for _, p := range in {
		if LooksLikeStegoManifestText(p.DescriptionMD) {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(p.Status), "rejected") {
			continue
		}
		out = append(out, p)
	}
	return out
}

func (s *ProposalService) requireWishForCreation(ctx context.Context, proposal smart_contract.Proposal) error {
	return s.requireWish(ctx, proposal, "create proposal")
}

func (s *ProposalService) requireWishForApproval(ctx context.Context, proposal smart_contract.Proposal) error {
	return s.requireWish(ctx, proposal, "approval")
}

func (s *ProposalService) requireWish(ctx context.Context, proposal smart_contract.Proposal, action string) error {
	if s.store == nil {
		return fmt.Errorf("wish store unavailable")
	}
	visible := proposalVisibleHash(proposal)
	if visible == "" {
		return fmt.Errorf("visible_pixel_hash is required for %s", action)
	}
	wishID := scstore.ToWishID(visible)
	if _, err := s.store.GetContract(wishID); err != nil {
		if _, err2 := s.store.GetContract(visible); err2 != nil {
			return fmt.Errorf("wish not found for visible_pixel_hash (tried %s and %s): %v", wishID, visible, err)
		}
	}
	return nil
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

// LooksLikeStegoManifestText detects embedded stego manifest prose.
func LooksLikeStegoManifestText(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "schema_version:") &&
		strings.Contains(lower, "proposal_id:") &&
		strings.Contains(lower, "visible_pixel_hash:")
}

// BuildProposalFromIngestion derives a proposal from a pending ingestion record.
func BuildProposalFromIngestion(body ProposalCreateInput, rec *appservices.IngestionRecord) (smart_contract.Proposal, error) {
	meta := copyMetaMap(rec.Metadata)
	if meta == nil {
		meta = map[string]interface{}{}
	}
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
			fields := strings.Fields(em)
			if len(fields) > 0 {
				title = fields[0]
			}
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
		budget = budgetFromMetaLocal(meta)
	}
	visible := body.VisiblePixelHash
	if visible == "" {
		if stegoHash, ok := meta["visible_pixel_hash"].(string); ok && strings.TrimSpace(stegoHash) != "" {
			visible = stegoHash
		} else if rec.ImageBase64 != "" {
			if h, err := hashBase64Image(rec.ImageBase64); err == nil {
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
	return smart_contract.Proposal{
		ID: id, Title: title, DescriptionMD: desc, VisiblePixelHash: visible,
		BudgetSats: budget, Status: status, CreatedAt: time.Now(),
		Tasks: tasks, Metadata: meta,
	}, nil
}

func hashBase64Image(data string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func budgetFromMetaLocal(meta map[string]interface{}) int64 {
	if meta == nil {
		return scstore.DefaultBudgetSats()
	}
	if budget, ok := meta["budget_sats"].(int64); ok && budget > 0 {
		return budget
	}
	if budget, ok := meta["budget_sats"].(float64); ok && budget > 0 {
		return int64(budget)
	}
	if budgetStr, ok := meta["budget_sats"].(string); ok {
		if budget, err := strconv.ParseInt(budgetStr, 10, 64); err == nil && budget > 0 {
			return budget
		}
	}
	return scstore.DefaultBudgetSats()
}

func applyCreatorWallet(meta map[string]interface{}, apiKey string, apiKeys auth.APIKeyValidator) {
	if meta == nil || apiKeys == nil {
		return
	}
	if _, ok := meta["creator_wallet"]; ok {
		return
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return
	}
	if rec, ok := apiKeys.Get(apiKey); ok && strings.TrimSpace(rec.Wallet) != "" {
		meta["creator_wallet"] = rec.Wallet
	}
}

func copyMetaMap(meta map[string]interface{}) map[string]interface{} {
	if meta == nil {
		return nil
	}
	out := make(map[string]interface{}, len(meta))
	for k, v := range meta {
		out[k] = v
	}
	return out
}
