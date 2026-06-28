package smart_contract

import (
	"net/http"
	"sync"

	"stargate-backend/bitcoin"
	"stargate-backend/core/smart_contract"
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
	mempool      *bitcoin.MempoolClient
	escort       *smart_contract.EscortService
}

// SetEscortService sets the escort service for the server.
func (s *Server) SetEscortService(escort *smart_contract.EscortService) {
	s.escort = escort
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
func NewServer(store Store, apiKeys auth.APIKeyValidator, ingest *services.IngestionService) *Server {
	srv := &Server{
		store:        store,
		apiKeys:      apiKeys,
		ingestionSvc: ingest,
		mempool:      bitcoin.NewMempoolClient(),
	}
	RegisterEventSink(srv.recordEvent)
	return srv
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
	mux.HandleFunc("/api/smart_contract/tasks", s.authWrap(s.handleTasks))
	mux.HandleFunc("/api/smart_contract/tasks/", s.authWrap(s.handleTasks))

	// Claim endpoints
	mux.HandleFunc("/api/smart_contract/claims/", s.authWrap(s.handleClaims))

	// Skill and discovery endpoints
	mux.HandleFunc("/api/smart_contract/skills", s.authWrap(s.handleSkills))
	mux.HandleFunc("/api/smart_contract/discover", s.authWrap(s.handleDiscover))

	// Proposal endpoints
	mux.HandleFunc("/api/smart_contract/proposals", s.authWrapReadOnly(s.handleProposals))
	mux.HandleFunc("/api/smart_contract/proposals/", s.authWrapReadOnly(s.handleProposals))

	// Submission endpoints
	mux.HandleFunc("/api/smart_contract/submissions", s.authWrap(s.handleSubmissions))
	mux.HandleFunc("/api/smart_contract/submissions/", s.authWrap(s.handleSubmissions))

	// Event endpoints
	mux.HandleFunc("/api/smart_contract/events", s.authWrapReadOnly(s.handleEvents))

	// Stego endpoints (still using original handlers for now)
	mux.HandleFunc("/api/smart_contract/stego/reconcile", s.authWrap(s.handleStegoReconcile))
	mux.HandleFunc("/api/smart_contract/stego/payload/", s.authWrap(s.handleStegoPayload))
}

func (s *Server) authWrap(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.apiKeys != nil {
			key := r.Header.Get("X-API-Key")
			if key == "" || !s.apiKeys.Validate(key) {
				Error(w, http.StatusForbidden, "invalid api key")
				return
			}
		}
		next(w, r)
	}
}

// authWrapReadOnly allows GET requests without authentication but requires auth for other methods
func (s *Server) authWrapReadOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			// Allow GET requests without authentication
			next(w, r)
			return
		}

		// Require authentication for non-GET methods
		if s.apiKeys != nil {
			key := r.Header.Get("X-API-Key")
			if key == "" || !s.apiKeys.Validate(key) {
				Error(w, http.StatusForbidden, "invalid api key")
				return
			}
		}
		next(w, r)
	}
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
