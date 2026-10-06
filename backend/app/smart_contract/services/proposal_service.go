package services

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"stargate-backend/core/identity"
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
	Cursor           string
	CursorDate       string
	CursorType       string
	IncludeConfirmed bool
}

// ProposalListResult is a paginated proposal list with submissions.
type ProposalListResult struct {
	Proposals   []smart_contract.Proposal
	Submissions []smart_contract.Submission
	smart_contract.Page
}

// ProposalEditAuthorizer decides whether the holder of an API key may edit or
// publish a proposal, returning the wallet it authorized so the caller records
// the identity that was actually accepted.
//
// Defined here rather than alongside its implementation because services cannot
// import app/smart_contract.
type ProposalEditAuthorizer interface {
	AuthorizeProposalEdit(ctx context.Context, apiKey, proposalID string) (string, error)
}

// ProposalActor identifies who is editing or publishing a proposal. Only the API
// key is taken, for the same reason as ReviewActor: the wallet is whatever the
// authorizer resolves, so a caller cannot name one identity while being
// authorized as another.
type ProposalActor struct {
	APIKey string
}

// ProposalService encapsulates proposal domain operations.
type ProposalService struct {
	store        scstore.Store
	ingestionSvc *appservices.IngestionService
	apiKeys      auth.APIKeyValidator
	record       EventRecorder
	authz        ProposalEditAuthorizer
	publishTasks func(ctx context.Context, proposalID string) error
	archiveWish  func(ctx context.Context, visibleHash string)
}

// NewProposalService constructs a ProposalService.
//
// authz is required by Update and Publish, which refuse without it rather than
// trusting their caller. Pass nil only where neither is reachable, or in tests
// asserting that refusal.
func NewProposalService(
	store scstore.Store,
	ingestionSvc *appservices.IngestionService,
	apiKeys auth.APIKeyValidator,
	record EventRecorder,
	authz ProposalEditAuthorizer,
	publishTasks func(ctx context.Context, proposalID string) error,
	archiveWish func(ctx context.Context, visibleHash string),
) *ProposalService {
	return &ProposalService{
		store:        store,
		ingestionSvc: ingestionSvc,
		apiKeys:      apiKeys,
		record:       record,
		authz:        authz,
		publishTasks: publishTasks,
		archiveWish:  archiveWish,
	}
}

// authorizeEdit resolves the wallet allowed to change proposalID, or an error
// describing the refusal. Update and Publish share it so the two cannot drift.
func (s *ProposalService) authorizeEdit(ctx context.Context, actor ProposalActor, proposalID, action string) (string, error) {
	if s.authz == nil {
		// Refuse rather than proceed unauthorized: an unwired service must not be
		// the difference between a guarded proposal and an open one.
		return "", Fail(http.StatusInternalServerError, "proposal edit authorizer not configured")
	}
	wallet, err := s.authz.AuthorizeProposalEdit(ctx, actor.APIKey, proposalID)
	if err != nil {
		return "", Fail(http.StatusForbidden, fmt.Sprintf("cannot %s proposal %s: %v", action, proposalID, err))
	}
	return wallet, nil
}

// SetRecorder updates the event sink.

// SetPublishTasks wires task publishing (typically EventService.PublishProposalTasks).

// SetArchiveWish wires wish archival after approval.

func (s *ProposalService) emit(evt smart_contract.Event) {
	if s.record != nil {
		s.record(evt)
	}
}

// Approve approves a proposal and publishes tasks.
//
// Authorization runs here rather than being asserted by the caller. This used to
// take a creatorOK bool: its one caller checked the creator and then passed true,
// so authorization was an argument, and a second caller passing true would have
// been authorized by saying so (stargate-az6). That reads as though a check
// happened, which is worse than an obviously absent one.
func (s *ProposalService) Approve(ctx context.Context, id string, actor ProposalActor) (map[string]interface{}, error) {
	if s.store == nil {
		return nil, Fail(http.StatusBadRequest, "store unavailable")
	}
	wallet, err := s.authorizeEdit(ctx, actor, id, "approve")
	if err != nil {
		return nil, err
	}
	apiKey := actor.APIKey
	proposal, err := s.store.GetProposal(ctx, id)
	if err != nil {
		return nil, FailKind(http.StatusBadRequest, KindProposalNotFound, err.Error())
	}
	if proposal.Metadata == nil {
		proposal.Metadata = map[string]interface{}{}
	}
	if err := s.requireWishForApproval(ctx, proposal); err != nil {
		return nil, FailKind(http.StatusBadRequest, KindWishNotFound, err.Error())
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
		Type: "approve", EntityID: id, Actor: wallet,
		Message: "proposal approved", CreatedAt: time.Now(),
	})
	return map[string]interface{}{
		"proposal_id": id,
		"status":      "approved",
		"message":     "Proposal approved.",
	}, nil
}

// Publish marks a proposal published.
//
// Authorization runs here rather than in the handler: publish sat next to an
// approve path that did check the creator, and the asymmetry was invisible from
// the route table (stargate-irl.7).
func (s *ProposalService) Publish(ctx context.Context, id string, actor ProposalActor) (map[string]interface{}, error) {
	wallet, err := s.authorizeEdit(ctx, actor, id, "publish")
	if err != nil {
		return nil, err
	}
	if err := s.store.PublishProposal(ctx, id); err != nil {
		return nil, Fail(http.StatusBadRequest, err.Error())
	}
	s.emit(smart_contract.Event{
		Type: "publish", EntityID: id, Actor: wallet,
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
			return nil, 0, createStoreError(err)
		}
		s.emit(smart_contract.Event{
			Type: "proposal_create", EntityID: proposal.ID, Actor: s.eventActor(body.APIKey),
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
	omittedBudget := body.BudgetSats == 0
	if omittedBudget {
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
	if identity.Normalize(contractID) != identity.Normalize(visiblePixelHash) {
		return nil, 0, Fail(http.StatusBadRequest, "contract_id must match visible_pixel_hash for wish proposals")
	}
	if n := identity.CanonicalContractID(visiblePixelHash); identity.IsPixelHash(n) {
		body.Metadata["contract_id"] = n
	}
	wish, err := scstore.LookupContract(s.store, visiblePixelHash)
	if err != nil {
		return nil, 0, FailKind(http.StatusNotFound, KindWishNotFound, "wish not found for visible_pixel_hash")
	}
	if wishBudget := scstore.WishBudgetFromContract(wish); wishBudget > 0 {
		if omittedBudget {
			body.BudgetSats = wishBudget
		} else if body.BudgetSats > wishBudget {
			return nil, 0, FailKind(http.StatusBadRequest, KindBudgetExceeded, fmt.Sprintf("proposal budget_sats %d exceeds original wish budget %d", body.BudgetSats, wishBudget))
		}
	}
	if len(body.Tasks) == 0 && strings.TrimSpace(body.DescriptionMD) != "" {
		body.Tasks = scstore.BuildTasksFromMarkdown(body.ID, body.DescriptionMD, visiblePixelHash, body.BudgetSats, scstore.FundingAddressFromMeta(body.Metadata))
	} else if len(body.Tasks) > 0 {
		titles := make([]string, len(body.Tasks))
		explicit := make([]int64, len(body.Tasks))
		for i, t := range body.Tasks {
			titles[i] = t.Title
			if t.BudgetSats > 0 {
				explicit[i] = t.BudgetSats
			}
		}
		amounts := scstore.AllocateTaskBudgets(titles, explicit, body.BudgetSats)
		for i := range body.Tasks {
			body.Tasks[i].BudgetSats = amounts[i]
		}
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
		return nil, 0, createStoreError(err)
	}
	s.emit(smart_contract.Event{
		Type: "proposal_create", EntityID: p.ID, Actor: s.eventActor(body.APIKey),
		Message: fmt.Sprintf("proposal created with %d tasks", len(p.Tasks)), CreatedAt: time.Now(),
	})
	return map[string]interface{}{
		"proposal_id": p.ID, "status": p.Status, "tasks": len(p.Tasks), "budget_sats": p.BudgetSats,
	}, http.StatusCreated, nil
}

// Update applies a partial update to a pending proposal.
//
// Being pending is not permission to edit: the status gate was the only check
// here, so any wallet-bound key could rewrite another creator's budget_sats,
// contract_id and tasks (stargate-irl.7). Ingest and sync paths do not reach
// this method; they write through the store metadata helpers.
func (s *ProposalService) Update(ctx context.Context, id string, body ProposalUpdateInput, actor ProposalActor) (map[string]interface{}, error) {
	wallet, err := s.authorizeEdit(ctx, actor, id, "update")
	if err != nil {
		return nil, err
	}
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
		Type: "update", EntityID: updated.ID, Actor: wallet,
		Message: "proposal updated", CreatedAt: time.Now(),
	})
	return map[string]interface{}{
		"proposal_id": updated.ID, "status": updated.Status, "message": "Proposal updated.",
	}, nil
}

// List returns paginated proposals with optional filtering.
func (s *ProposalService) List(ctx context.Context, q ProposalListQuery) (*ProposalListResult, error) {
	pageQ := smart_contract.NewPageQuery(q.Limit, q.Offset, q.Cursor, q.CursorDate, q.CursorType, smart_contract.DefaultPageLimit)
	filter := smart_contract.ProposalFilter{
		Status: q.Status, Skills: q.Skills, MinBudget: q.MinBudget, ContractID: q.ContractID,
	}
	pageQ.ApplyToProposal(&filter)
	filter.Limit = smart_contract.OverFetchLimit(pageQ.Limit)
	filter.MaxResults = filter.Limit
	proposals, err := s.store.ListProposals(ctx, filter)
	if err != nil {
		return nil, Fail(http.StatusInternalServerError, err.Error())
	}
	fetched := len(proposals)
	if !q.IncludeConfirmed {
		proposals = filterListedProposals(proposals)
	}
	proposals = smart_contract.TrimWindow(proposals, pageQ.Limit)
	lastID, lastDate := "", time.Time{}
	if n := len(proposals); n > 0 {
		lastID = proposals[n-1].ID
		lastDate = proposals[n-1].CreatedAt
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
	var subs []smart_contract.Submission
	if len(taskIDs) > 0 {
		subs, _ = s.store.ListSubmissions(ctx, smart_contract.SubmissionFilter{TaskIDs: taskIDs})
	}
	return &ProposalListResult{
		Proposals:   proposals,
		Submissions: subs,
		Page:        smart_contract.BuildPage(pageQ.Limit, pageQ.Offset, fetched, len(proposals), lastID, lastDate),
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

// eventActor resolves the wallet bound to apiKey, for use as an event actor.
//
// Proposal creation is not restricted to wallet-bound keys, so this can come back
// empty, and empty is the honest answer: the events filter already treats a blank
// actor as "no identity to match on" (server_events.go). The literal "creator" it
// replaces was indistinguishable from a real identity while never being one
// (stargate-az6).
func (s *ProposalService) eventActor(apiKey string) string {
	if s.apiKeys == nil {
		return ""
	}
	rec, ok := s.apiKeys.Get(strings.TrimSpace(apiKey))
	if !ok {
		return ""
	}
	return strings.TrimSpace(rec.Wallet)
}

// createStoreError attaches a Kind to the store's create refusals so a caller
// can tell them apart. All three are 400 to REST, so status cannot separate
// them; the MCP surface used to match on message substrings, which meant
// rewording a store error silently downgraded a specific error code to a generic
// internal one. Classification is by sentinel, and it lives here rather than in
// each surface so the surfaces cannot drift on it.
func createStoreError(err error) error {
	switch {
	case errors.Is(err, scstore.ErrProposalLimitReached):
		return FailKind(http.StatusBadRequest, KindProposalLimitReached, err.Error())
	case errors.Is(err, scstore.ErrProposalAlreadyFinalized):
		return FailKind(http.StatusBadRequest, KindProposalAlreadyFinalized, err.Error())
	case errors.Is(err, scstore.ErrTaskBudgetMismatch):
		return FailKind(http.StatusBadRequest, KindBudgetMismatch, err.Error())
	}
	return Fail(http.StatusBadRequest, err.Error())
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
