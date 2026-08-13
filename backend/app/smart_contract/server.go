package smart_contract

import (
	"net/http"
	"sync"

	scservices "stargate-backend/app/smart_contract/services"
	"stargate-backend/bitcoin"
	"stargate-backend/core/smart_contract"
	"stargate-backend/middleware"
	"stargate-backend/services"
	auth "stargate-backend/storage/auth"
)

// Server wires handlers for MCP endpoints.
type Server struct {
	store        Store
	apiKeys      auth.APIKeyValidator
	ingestionSvc *services.IngestionService
	events       []smart_contract.Event
	eventsMu     sync.Mutex
	listenersMu  sync.Mutex
	listeners    []chan smart_contract.Event
	mempool      bitcoin.UTXOClient
	escort       *smart_contract.EscortService

	// Domain services (business logic extracted from HTTP handlers).
	eventSvc      *scservices.EventService
	psbtSvc       *scservices.PSBTService
	taskSvc       *scservices.TaskService
	claimSvc      *scservices.ClaimService
	proposalSvc   *scservices.ProposalService
	submissionSvc *scservices.SubmissionService

	// Shared with MCP /mcp/call. Nil disables action rate limiting.
	actionLimiter *middleware.ActionLimiter
}

// SetEscortService sets the escort service for the server.
func (s *Server) SetEscortService(escort *smart_contract.EscortService) {
	s.escort = escort
}

// SetActionLimiter installs the shared claim/submit/review limiter.
func (s *Server) SetActionLimiter(limiter *middleware.ActionLimiter) {
	s.actionLimiter = limiter
}

// proposalCreateBody captures POST payload for creating proposals.
type ProposalCreateBody struct {
	ID               string                 `json:"id"`
	IngestionID      string                 `json:"ingestion_id"`
	ContractID       string                 `json:"contract_id"`
	Title            string                 `json:"title"`
	DescriptionMD    string                 `json:"description_md"`
	VisiblePixelHash string                 `json:"visible_pixel_hash"`
	BudgetSats       int64                  `json:"budget_sats"`
	Status           string                 `json:"status"`
	Metadata         map[string]interface{} `json:"metadata"`
	Tasks            []smart_contract.Task  `json:"tasks"`
}

// ProposalUpdateBody captures PATCH/PUT payload for updating proposals.
type ProposalUpdateBody struct {
	Title            *string                 `json:"title"`
	DescriptionMD    *string                 `json:"description_md"`
	VisiblePixelHash *string                 `json:"visible_pixel_hash"`
	BudgetSats       *int64                  `json:"budget_sats"`
	ContractID       *string                 `json:"contract_id"`
	Metadata         *map[string]interface{} `json:"metadata"`
	Tasks            *[]smart_contract.Task  `json:"tasks"`
}

// NewServer builds a Server with the given store.
// Default UTXO client is Esplora (for unit tests). Production wiring must call
// SetUTXOClient with the local btcd ChainBackend.
func NewServer(store Store, apiKeys auth.APIKeyValidator, ingest *services.IngestionService) *Server {
	mempool := bitcoin.NewMempoolClient()
	srv := &Server{
		store:        store,
		apiKeys:      apiKeys,
		ingestionSvc: ingest,
		mempool:      mempool,
		eventSvc:     scservices.NewEventService(store, nil),
		psbtSvc:      scservices.NewPSBTService(store, mempool, ingest),
		taskSvc:      scservices.NewTaskService(store, ingest),
		claimSvc:     scservices.NewClaimService(store),
	}
	srv.eventSvc.SetRecorder(srv.recordEvent)
	srv.proposalSvc = scservices.NewProposalService(store, ingest, apiKeys, srv.recordEvent, srv.eventSvc.PublishProposalTasks, srv.archiveWishContract)
	srv.submissionSvc = scservices.NewSubmissionService(store, srv.recordEvent)
	RegisterEventSink(srv.recordEvent)
	return srv
}

// SetUTXOClient wires the chain backend used for PSBT UTXO selection and sweeps.
func (s *Server) SetUTXOClient(client bitcoin.UTXOClient) {
	if client == nil {
		return
	}
	s.mempool = client
	s.psbtSvc = scservices.NewPSBTService(s.store, client, s.ingestionSvc)
}

// RegisterRoutes attaches handlers to the mux.
func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	// Health and config endpoints
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/api/smart_contract/config", s.authWrap(s.handleConfig))

	// Contract endpoints
	mux.HandleFunc("/api/smart_contract/contracts", s.authWrap(s.handleContracts))
	mux.HandleFunc("/api/smart_contract/contracts/", s.authWrap(s.handleContracts))

	// Task endpoints
	mux.HandleFunc("/api/smart_contract/tasks", s.authWrap(s.limitAction(s.handleTasks)))
	mux.HandleFunc("/api/smart_contract/tasks/", s.authWrap(s.limitAction(s.handleTasks)))

	// Claim endpoints
	mux.HandleFunc("/api/smart_contract/claims/", s.authWrap(s.limitAction(s.handleClaims)))

	// Skill and discovery endpoints
	mux.HandleFunc("/api/smart_contract/skills", s.authWrap(s.handleSkills))
	mux.HandleFunc("/api/smart_contract/discover", s.authWrap(s.handleDiscover))

	// Proposal endpoints
	mux.HandleFunc("/api/smart_contract/proposals", s.authWrapReadOnly(s.handleProposals))
	mux.HandleFunc("/api/smart_contract/proposals/", s.authWrapReadOnly(s.handleProposals))

	// Submission endpoints
	mux.HandleFunc("/api/smart_contract/submissions", s.authWrap(s.limitAction(s.handleSubmissions)))
	mux.HandleFunc("/api/smart_contract/submissions/", s.authWrap(s.limitAction(s.handleSubmissions)))

	// Event endpoints
	mux.HandleFunc("/api/smart_contract/events", s.authWrapReadOnly(s.handleEvents))

	// Stego endpoints (still using original handlers for now)
	mux.HandleFunc("/api/smart_contract/stego/reconcile", s.authWrap(s.handleStegoReconcile))
	mux.HandleFunc("/api/smart_contract/stego/payload/", s.authWrap(s.handleStegoPayload))
}

func (s *Server) authWrap(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r, ok := s.authenticate(w, r, true)
		if !ok {
			return
		}
		next(w, r)
	}
}

// authWrapReadOnly allows GET requests without authentication but requires auth for other methods
func (s *Server) authWrapReadOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			next(w, r)
			return
		}
		r, ok := s.authenticate(w, r, true)
		if !ok {
			return
		}
		next(w, r)
	}
}

// authenticate applies the same bearer path as middleware.APIAuth and MCP.
// required=false is unused today but keeps the gate in one place.
func (s *Server) authenticate(w http.ResponseWriter, r *http.Request, required bool) (*http.Request, bool) {
	if s.apiKeys == nil {
		return r, true
	}
	key := auth.APIKeyFromRequest(r)
	if key == "" {
		if !required {
			return r, true
		}
		Error(w, http.StatusUnauthorized, "API key required")
		return r, false
	}
	if !s.apiKeys.Validate(key) {
		Error(w, http.StatusForbidden, "invalid api key")
		return r, false
	}
	return r.WithContext(auth.WithAPIKey(r.Context(), key)), true
}

// submissionReviewBody captures POST payload for reviewing submissions.
type submissionReviewBody struct {
	Action        string `json:"action"` // review | approve | reject
	Notes         string `json:"notes"`
	RejectionType string `json:"rejection_type"`
}

// submissionReworkBody captures POST payload for reworking submissions.
type submissionReworkBody struct {
	Deliverables map[string]interface{} `json:"deliverables"`
	Notes        string                 `json:"notes"`
}
