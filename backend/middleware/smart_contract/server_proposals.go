package smart_contract

import (
	"encoding/json"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"stargate-backend/core/smart_contract"
	scservices "stargate-backend/middleware/smart_contract/services"
	"stargate-backend/services"
)

func proposalVisibleHash(p smart_contract.Proposal) string {
	if strings.TrimSpace(p.VisiblePixelHash) != "" {
		return strings.TrimSpace(p.VisiblePixelHash)
	}
	if v, ok := p.Metadata["visible_pixel_hash"].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return ""
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

func proofsConfirmed(proofs []smart_contract.MerkleProof) bool {
	for i := range proofs {
		if proofConfirmed(&proofs[i]) {
			return true
		}
	}
	return false
}

// handleProposals supports listing, getting, and approving proposals.
func (s *Server) handleProposals(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/smart_contract/proposals")
	path = strings.Trim(path, "/")
	switch r.Method {
	case http.MethodPost:
		parts := strings.Split(path, "/")
		if len(parts) == 2 && parts[1] == "approve" {
			s.handleProposalApprove(w, r, parts[0])
			return
		}
		if len(parts) == 2 && parts[1] == "publish" {
			s.handleProposalPublish(w, r, parts[0])
			return
		}
		s.handleProposalCreate(w, r)
	case http.MethodPut, http.MethodPatch:
		parts := strings.Split(path, "/")
		if len(parts) < 1 || parts[0] == "" {
			Error(w, http.StatusBadRequest, "proposal id required")
			return
		}
		s.handleProposalUpdate(w, r, parts[0])
	case http.MethodGet:
		if path == "" {
			s.handleProposalList(w, r)
			return
		}
		s.handleProposalGet(w, r, path)
	default:
		Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) writeServiceErr(w http.ResponseWriter, err error) {
	if se := scservices.AsStatus(err); se != nil {
		Error(w, se.Status, se.Message)
		return
	}
	Error(w, http.StatusInternalServerError, err.Error())
}

func (s *Server) handleProposalApprove(w http.ResponseWriter, r *http.Request, id string) {
	proposal, err := s.store.GetProposal(r.Context(), id)
	if err != nil {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.enforceCreatorApproval(r, proposal); err != nil {
		Error(w, http.StatusForbidden, err.Error())
		return
	}
	resp, err := s.proposalSvc.Approve(r.Context(), id, r.Header.Get("X-API-Key"), true)
	if err != nil {
		s.writeServiceErr(w, err)
		return
	}
	JSON(w, http.StatusOK, resp)
}

func (s *Server) handleProposalPublish(w http.ResponseWriter, r *http.Request, id string) {
	resp, err := s.proposalSvc.Publish(r.Context(), id)
	if err != nil {
		s.writeServiceErr(w, err)
		return
	}
	JSON(w, http.StatusOK, resp)
}

func (s *Server) handleProposalCreate(w http.ResponseWriter, r *http.Request) {
	if ct := r.Header.Get("Content-Type"); ct != "" && !strings.Contains(ct, "application/json") {
		Error(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return
	}
	log.Printf("CRITICAL: HandleCreateProposal called at %s from %s, User-Agent: %s", time.Now().Format(time.RFC3339), r.RemoteAddr, r.Header.Get("User-Agent"))
	var body ProposalCreateBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	resp, status, err := s.proposalSvc.Create(r.Context(), scservices.ProposalCreateInput{
		ID: body.ID, IngestionID: body.IngestionID, ContractID: body.ContractID,
		Title: body.Title, DescriptionMD: body.DescriptionMD, VisiblePixelHash: body.VisiblePixelHash,
		BudgetSats: body.BudgetSats, Status: body.Status, Metadata: body.Metadata, Tasks: body.Tasks,
		APIKey: r.Header.Get("X-API-Key"),
	})
	if err != nil {
		s.writeServiceErr(w, err)
		return
	}
	JSON(w, status, resp)
}

func (s *Server) handleProposalUpdate(w http.ResponseWriter, r *http.Request, id string) {
	if ct := r.Header.Get("Content-Type"); ct != "" && !strings.Contains(ct, "application/json") {
		Error(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return
	}
	var body ProposalUpdateBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	resp, err := s.proposalSvc.Update(r.Context(), id, scservices.ProposalUpdateInput{
		Title: body.Title, DescriptionMD: body.DescriptionMD, VisiblePixelHash: body.VisiblePixelHash,
		BudgetSats: body.BudgetSats, ContractID: body.ContractID, Metadata: body.Metadata, Tasks: body.Tasks,
	})
	if err != nil {
		s.writeServiceErr(w, err)
		return
	}
	JSON(w, http.StatusOK, resp)
}

func (s *Server) handleProposalList(w http.ResponseWriter, r *http.Request) {
	result, err := s.proposalSvc.List(r.Context(), scservices.ProposalListQuery{
		Status: r.URL.Query().Get("status"), Skills: splitCSV(r.URL.Query().Get("skills")),
		MinBudget: int64FromQuery(r, "min_budget_sats", 0), ContractID: r.URL.Query().Get("contract_id"),
		Limit: intFromQuery(r, "limit", 20), Offset: intFromQuery(r, "offset", 0),
		IncludeConfirmed: includeConfirmed(r),
	})
	if err != nil {
		s.writeServiceErr(w, err)
		return
	}
	JSON(w, http.StatusOK, map[string]interface{}{
		"proposals": result.Proposals, "total": result.Total, "has_more": result.HasMore,
		"limit": result.Limit, "offset": result.Offset, "submissions": result.Submissions,
	})
}

func (s *Server) handleProposalGet(w http.ResponseWriter, r *http.Request, id string) {
	p, err := s.proposalSvc.Get(r.Context(), id)
	if err != nil {
		s.writeServiceErr(w, err)
		return
	}
	JSON(w, http.StatusOK, p)
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

// BuildProposalFromIngestion derives a proposal from a pending ingestion record.
func BuildProposalFromIngestion(body ProposalCreateBody, rec *services.IngestionRecord) (smart_contract.Proposal, error) {
	return scservices.BuildProposalFromIngestion(scservices.ProposalCreateInput{
		ID: body.ID, IngestionID: body.IngestionID, ContractID: body.ContractID,
		Title: body.Title, DescriptionMD: body.DescriptionMD, VisiblePixelHash: body.VisiblePixelHash,
		BudgetSats: body.BudgetSats, Status: body.Status, Metadata: body.Metadata, Tasks: body.Tasks,
	}, rec)
}

// handleSubmissions manages submission endpoints for review and rework.
func (s *Server) handleSubmissions(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/smart_contract/submissions")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	switch r.Method {
	case http.MethodGet:
		if path == "" || path == "/" {
			s.handleSubmissionList(w, r)
			return
		}
		if len(parts) >= 1 && parts[0] != "" {
			s.handleSubmissionGet(w, r, parts[0])
			return
		}
		Error(w, http.StatusBadRequest, "invalid submission endpoint")
	case http.MethodPost:
		if len(parts) >= 2 && parts[1] == "review" {
			s.handleSubmissionReview(w, r, parts[0])
			return
		}
		if len(parts) >= 2 && parts[1] == "rework" {
			s.handleSubmissionRework(w, r, parts[0])
			return
		}
		Error(w, http.StatusBadRequest, "invalid submission endpoint")
	default:
		Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleSubmissionList(w http.ResponseWriter, r *http.Request) {
	m, total, err := s.submissionSvc.List(r.Context(), r.URL.Query().Get("contract_id"), splitCSV(r.URL.Query().Get("task_ids")), r.URL.Query().Get("status"))
	if err != nil {
		s.writeServiceErr(w, err)
		return
	}
	JSON(w, http.StatusOK, map[string]interface{}{"submissions": m, "total": total})
}

func (s *Server) handleSubmissionGet(w http.ResponseWriter, r *http.Request, id string) {
	log.Printf("GET submission by ID: %s", id)
	sub, err := s.submissionSvc.Get(r.Context(), id)
	if err != nil {
		log.Printf("Failed to get submission %s: %v", id, err)
		s.writeServiceErr(w, err)
		return
	}
	JSON(w, http.StatusOK, sub)
}

func (s *Server) handleSubmissionReview(w http.ResponseWriter, r *http.Request, id string) {
	var body submissionReviewBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	resp, err := s.submissionSvc.Review(r.Context(), id, scservices.SubmissionReviewInput{
		Action: body.Action, Notes: body.Notes, RejectionType: body.RejectionType,
	})
	if err != nil {
		s.writeServiceErr(w, err)
		return
	}
	JSON(w, http.StatusOK, resp)
}

func (s *Server) handleSubmissionRework(w http.ResponseWriter, r *http.Request, id string) {
	var body submissionReworkBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	resp, err := s.submissionSvc.Rework(r.Context(), id, scservices.SubmissionReworkInput{
		Deliverables: body.Deliverables, Notes: body.Notes,
	})
	if err != nil {
		s.writeServiceErr(w, err)
		return
	}
	JSON(w, http.StatusOK, resp)
}
