package smart_contract

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"stargate-backend/bitcoin"
	"stargate-backend/core/smart_contract"
	"stargate-backend/services"
	auth "stargate-backend/storage/auth"
	scstore "stargate-backend/storage/smart_contract"
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

// NewServer builds a Server with the given store.
func NewServer(store Store, apiKeys auth.APIKeyValidator, ingest *services.IngestionService) *Server {
	return &Server{
		store:        store,
		apiKeys:      apiKeys,
		ingestionSvc: ingest,
		mempool:      bitcoin.NewMempoolClient(),
	}
}

// RegisterRoutes attaches handlers to the mux.
func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/api/smart_contract/contracts", s.authWrap(s.handleContracts))
	mux.HandleFunc("/api/smart_contract/contracts/", s.authWrap(s.handleContracts))
	mux.HandleFunc("/api/smart_contract/tasks", s.authWrap(s.handleTasks))
	mux.HandleFunc("/api/smart_contract/tasks/", s.authWrap(s.handleTasks))
	mux.HandleFunc("/api/smart_contract/claims/", s.authWrap(s.handleClaims))
	mux.HandleFunc("/api/smart_contract/skills", s.authWrap(s.handleSkills))
	mux.HandleFunc("/api/smart_contract/discover", s.authWrap(s.handleDiscover))
	mux.HandleFunc("/api/smart_contract/proposals", s.authWrap(s.handleProposals))
	mux.HandleFunc("/api/smart_contract/proposals/", s.authWrap(s.handleProposals))
	mux.HandleFunc("/api/smart_contract/submissions", s.authWrap(s.handleSubmissions))
	mux.HandleFunc("/api/smart_contract/submissions/", s.authWrap(s.handleSubmissions))
	mux.HandleFunc("/api/smart_contract/events", s.authWrap(s.handleEvents))
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

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleContracts(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/smart_contract/contracts")
	parts := strings.Split(strings.Trim(path, "/"), "/")

	switch r.Method {
	case http.MethodGet:
		if path == "" || path == "/" {
			status := r.URL.Query().Get("status")
			skills := splitCSV(r.URL.Query().Get("skills"))
			contracts, err := s.store.ListContracts(status, skills)
			if err != nil {
				Error(w, http.StatusInternalServerError, err.Error())
				return
			}
			JSON(w, http.StatusOK, map[string]interface{}{
				"contracts":   contracts,
				"total_count": len(contracts),
			})
			return
		}

		contractID := parts[0]
		if len(parts) > 1 && parts[1] == "funding" {
			contract, proofs, err := s.store.ContractFunding(contractID)
			if err != nil {
				Error(w, http.StatusNotFound, err.Error())
				return
			}
			JSON(w, http.StatusOK, map[string]interface{}{
				"contract": contract,
				"proofs":   proofs,
			})
			return
		}

		contract, err := s.store.GetContract(contractID)
		if err != nil {
			Error(w, http.StatusNotFound, err.Error())
			return
		}
		JSON(w, http.StatusOK, contract)
	case http.MethodPost:
		if len(parts) > 1 && parts[1] == "psbt" {
			contractID := parts[0]
			s.handleContractPSBT(w, r, contractID)
			return
		}
		Error(w, http.StatusNotFound, "unknown contract action")
	default:
		Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleContractPSBT builds a PSBT to fund the contract payout using the caller's wallet UTXOs.
func (s *Server) handleContractPSBT(w http.ResponseWriter, r *http.Request, contractID string) {
	if r.Header.Get("Content-Type") != "" && !strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		Error(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return
	}
	if s.apiKeys == nil || s.mempool == nil {
		Error(w, http.StatusServiceUnavailable, "psbt builder unavailable")
		return
	}
	payerKey := r.Header.Get("X-API-Key")
	payerRec, ok := s.apiKeys.Get(payerKey)
	if !ok {
		Error(w, http.StatusForbidden, "invalid api key")
		return
	}
	if strings.TrimSpace(payerRec.Wallet) == "" {
		Error(w, http.StatusForbidden, "api key missing wallet binding")
		return
	}

	var body struct {
		ContractorAPIKey string `json:"contractor_api_key"`
		ContractorWallet string `json:"contractor_wallet"`
		BudgetSats       int64  `json:"budget_sats"`
		PixelHash        string `json:"pixel_hash"`
		CommitmentSats   int64  `json:"commitment_sats"`
		FeeRate          int64  `json:"fee_rate_sats_vb"`
		UsePixelHash     *bool  `json:"use_pixel_hash"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "invalid json")
		return
	}

	contract, err := s.store.GetContract(contractID)
	if err != nil {
		Error(w, http.StatusNotFound, err.Error())
		return
	}

	params := &chaincfg.TestNet4Params

	payerAddr, err := btcutil.DecodeAddress(payerRec.Wallet, params)
	if err != nil {
		Error(w, http.StatusBadRequest, fmt.Sprintf("invalid payer wallet: %v", err))
		return
	}

	var contractorAddr btcutil.Address
	if strings.TrimSpace(body.ContractorAPIKey) != "" {
		if rec, ok := s.apiKeys.Get(body.ContractorAPIKey); ok && strings.TrimSpace(rec.Wallet) != "" {
			contractorAddr, err = btcutil.DecodeAddress(rec.Wallet, params)
		}
	}
	if contractorAddr == nil && strings.TrimSpace(body.ContractorWallet) != "" {
		contractorAddr, err = btcutil.DecodeAddress(strings.TrimSpace(body.ContractorWallet), params)
	}
	if err != nil {
		Error(w, http.StatusBadRequest, fmt.Sprintf("invalid contractor wallet: %v", err))
		return
	}

	target := body.BudgetSats
	if target <= 0 {
		target = contract.TotalBudgetSats
	}
	if target <= 0 {
		target = scstore.DefaultBudgetSats()
	}

	normalizePixel := func(b []byte) []byte {
		if l := len(b); l == 20 || l == 32 {
			return b
		}
		return nil
	}
	var pixelBytes []byte
	usePixelHash := false
	if body.UsePixelHash != nil {
		usePixelHash = *body.UsePixelHash
	}
	var ingestionRec *services.IngestionRecord
	if usePixelHash {
		if ph := strings.TrimSpace(body.PixelHash); ph != "" {
			if b, err := hex.DecodeString(ph); err == nil {
				pixelBytes = normalizePixel(b)
			}
		}
		if pixelBytes == nil && s.ingestionSvc != nil {
			if rec, err := s.ingestionSvc.Get(contractID); err == nil {
				ingestionRec = rec
				pixelBytes = resolvePixelHashFromIngestion(rec, normalizePixel)
			}
		}
	}
	if usePixelHash && pixelBytes == nil {
		if h, err := hex.DecodeString(strings.TrimSpace(contractID)); err == nil {
			pixelBytes = normalizePixel(h)
		}
	}

	psbtReq := bitcoin.PSBTRequest{
		PayerAddress:      payerAddr,
		TargetValueSats:   target,
		PixelHash:         pixelBytes,
		CommitmentSats:    body.CommitmentSats,
		ContractorAddress: contractorAddr,
		FeeRateSatPerVB:   body.FeeRate,
	}

	res, err := bitcoin.BuildFundingPSBT(s.mempool, params, psbtReq)
	if err != nil {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if ingestionRec != nil && res.FundingTxID != "" {
		if err := s.ingestionSvc.UpdateMetadata(ingestionRec.ID, map[string]interface{}{
			"funding_txid": res.FundingTxID,
		}); err != nil {
			log.Printf("psbt: failed to store funding_txid for %s: %v", ingestionRec.ID, err)
		}
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"psbt":              res.EncodedHex, // primary: hex for wallet import
		"psbt_hex":          res.EncodedHex,
		"psbt_base64":       res.EncodedBase64,
		"funding_txid":      res.FundingTxID,
		"fee_sats":          res.FeeSats,
		"change_sats":       res.ChangeSats,
		"selected_sats":     res.SelectedSats,
		"payout_script":     hex.EncodeToString(res.PayoutScript),
		"commitment_script": hex.EncodeToString(res.CommitmentScript),
		"commitment_sats":   res.CommitmentSats,
		"pixel_hash":        strings.TrimSpace(body.PixelHash),
		"payer_address":     payerAddr.EncodeAddress(),
		"contract_id":       contractID,
		"pixel_source":      pixelSourceForBytes(pixelBytes),
		"budget_sats":       target,
		"contractor":        contractorAddr.EncodeAddress(),
		"network_params":    params.Name,
	})
}

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/smart_contract/tasks")
	path = strings.Trim(path, "/")

	switch r.Method {
	case http.MethodGet:
		if path == "" {
			filter := smart_contract.TaskFilter{
				Skills:        splitCSV(r.URL.Query().Get("skills")),
				MaxDifficulty: r.URL.Query().Get("max_difficulty"),
				Status:        r.URL.Query().Get("status"),
				Limit:         intFromQuery(r, "limit", 50),
				Offset:        intFromQuery(r, "offset", 0),
				MinBudgetSats: int64FromQuery(r, "min_budget_sats", 0),
				ContractID:    r.URL.Query().Get("contract_id"),
				ClaimedBy:     r.URL.Query().Get("claimed_by"),
			}
			tasks, err := s.store.ListTasks(filter)
			if err != nil {
				Error(w, http.StatusInternalServerError, err.Error())
				return
			}
			// hydrate submissions for these tasks
			var taskIDs []string
			for _, t := range tasks {
				taskIDs = append(taskIDs, t.TaskID)
			}
			subs, _ := s.store.ListSubmissions(r.Context(), taskIDs)
			JSON(w, http.StatusOK, map[string]interface{}{
				"tasks":         tasks,
				"total_matches": len(tasks),
				"submissions":   subs,
			})
			return
		}

		parts := strings.Split(path, "/")
		taskID := parts[0]

		// Nested resources
		if len(parts) > 1 && parts[1] == "merkle-proof" {
			proof, err := s.store.GetTaskProof(taskID)
			if err != nil {
				Error(w, http.StatusNotFound, err.Error())
				return
			}
			JSON(w, http.StatusOK, proof)
			return
		}

		if len(parts) > 1 && parts[1] == "status" {
			status, err := s.store.TaskStatus(taskID)
			if err != nil {
				Error(w, http.StatusNotFound, err.Error())
				return
			}
			JSON(w, http.StatusOK, status)
			return
		}

		task, err := s.store.GetTask(taskID)
		if err != nil {
			Error(w, http.StatusNotFound, err.Error())
			return
		}
		JSON(w, http.StatusOK, task)
	case http.MethodPost:
		parts := strings.Split(path, "/")
		if len(parts) < 2 {
			Error(w, http.StatusBadRequest, "expected /tasks/{task_id}/claim")
			return
		}
		taskID := parts[0]
		switch parts[1] {
		case "claim":
			s.handleClaimTask(w, r, taskID)
		default:
			Error(w, http.StatusNotFound, "unknown task action")
		}
	default:
		Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func resolvePixelHashFromIngestion(rec *services.IngestionRecord, normalize func([]byte) []byte) []byte {
	if rec == nil {
		return nil
	}

	for _, key := range []string{"pixel_hash", "payout_script_hash", "visible_pixel_hash"} {
		if v, ok := rec.Metadata[key].(string); ok {
			if b, err := hex.DecodeString(strings.TrimSpace(v)); err == nil {
				if normalized := normalize(b); normalized != nil {
					return normalized
				}
			}
		}
	}

	message := ""
	if v, ok := rec.Metadata["embedded_message"].(string); ok {
		message = v
	}
	if message == "" {
		if v, ok := rec.Metadata["message"].(string); ok {
			message = v
		}
	}
	if rec.ImageBase64 == "" {
		return nil
	}
	imageBytes, err := base64.StdEncoding.DecodeString(rec.ImageBase64)
	if err != nil {
		return nil
	}

	if message != "" {
		sum := sha256.Sum256(append(imageBytes, []byte(message)...))
		return normalize(sum[:])
	}

	sum := sha256.Sum256(imageBytes)
	return normalize(sum[:])
}

func pixelSourceForBytes(pixel []byte) string {
	switch len(pixel) {
	case 32:
		return "witness_script_hash"
	case 20:
		return "script_hash"
	default:
		return ""
	}
}

func (s *Server) handleClaimTask(w http.ResponseWriter, r *http.Request, taskID string) {
	if ct := r.Header.Get("Content-Type"); ct != "" && !strings.Contains(ct, "application/json") {
		Error(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return
	}
	var body struct {
		AiIdentifier        string     `json:"ai_identifier"`
		Wallet              string     `json:"wallet_address,omitempty"`
		EstimatedCompletion *time.Time `json:"estimated_completion,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.AiIdentifier == "" {
		Error(w, http.StatusBadRequest, "ai_identifier required")
		return
	}

	contractorWallet := strings.TrimSpace(body.Wallet)
	if s.apiKeys != nil {
		if rec, ok := s.apiKeys.Get(r.Header.Get("X-API-Key")); ok && strings.TrimSpace(rec.Wallet) != "" {
			contractorWallet = strings.TrimSpace(rec.Wallet)
		}
	}

	claim, err := s.store.ClaimTask(taskID, body.AiIdentifier, contractorWallet, body.EstimatedCompletion)
	if err != nil {
		if err == ErrTaskNotFound {
			// Attempt to publish tasks lazily from proposals that reference this task id, then retry.
			if s.tryPublishTasksForTaskID(r.Context(), taskID) == nil {
				if retry, retryErr := s.store.ClaimTask(taskID, body.AiIdentifier, contractorWallet, body.EstimatedCompletion); retryErr == nil {
					claim = retry
					err = nil
				} else {
					err = retryErr
				}
			}
			if err == nil {
				goto claim_success
			}
			if err == ErrTaskNotFound {
				Error(w, http.StatusNotFound, err.Error())
				return
			}
		}
		if err == ErrTaskTaken {
			Error(w, http.StatusConflict, err.Error())
			return
		}
		Error(w, http.StatusBadRequest, err.Error())
		return
	}
claim_success:

	JSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"claim_id":   claim.ClaimID,
		"expires_at": claim.ExpiresAt,
		"message":    "Task reserved. Submit work before expiration.",
	})

	s.recordEvent(smart_contract.Event{
		Type:      "claim",
		EntityID:  taskID,
		Actor:     body.AiIdentifier,
		Message:   "task claimed",
		CreatedAt: time.Now(),
	})
}

func (s *Server) handleClaims(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/smart_contract/claims/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		Error(w, http.StatusBadRequest, "claim id required")
		return
	}
	claimID := parts[0]

	if len(parts) < 2 || parts[1] != "submit" {
		Error(w, http.StatusNotFound, "unknown claim action")
		return
	}

	if r.Method != http.MethodPost {
		Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if ct := r.Header.Get("Content-Type"); ct != "" && !strings.Contains(ct, "application/json") {
		Error(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return
	}

	var body struct {
		Deliverables    map[string]interface{} `json:"deliverables"`
		CompletionProof map[string]interface{} `json:"completion_proof"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "invalid json")
		return
	}

	sub, err := s.store.SubmitWork(claimID, body.Deliverables, body.CompletionProof)
	if err != nil {
		if err == ErrClaimNotFound {
			Error(w, http.StatusNotFound, err.Error())
			return
		}
		Error(w, http.StatusBadRequest, err.Error())
		return
	}

	actor := "claimant"
	if who, ok := body.Deliverables["submitted_by"].(string); ok && who != "" {
		actor = who
	}
	s.recordEvent(smart_contract.Event{
		Type:      "submit",
		EntityID:  claimID,
		Actor:     actor,
		Message:   "submission created",
		CreatedAt: time.Now(),
	})

	JSON(w, http.StatusOK, sub)
}

// handleSkills returns a unique list of skills across all tasks for quick capability checks by agents.
func (s *Server) handleSkills(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	tasks, err := s.store.ListTasks(smart_contract.TaskFilter{})
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	skillSet := make(map[string]struct{})
	// Add default skills
	skillSet["contract_bidding"] = struct{}{}
	skillSet["get_open_contracts"] = struct{}{}

	for _, t := range tasks {
		for _, skill := range t.Skills {
			key := strings.ToLower(strings.TrimSpace(skill))
			if key == "" {
				continue
			}
			skillSet[key] = struct{}{}
		}
	}
	skills := make([]string, 0, len(skillSet))
	for k := range skillSet {
		skills = append(skills, k)
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"skills": skills,
		"count":  len(skills),
	})
}

// handleDiscover advertises API endpoints and MCP tool surface for clients.
func (s *Server) handleDiscover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	base := fmt.Sprintf("http://%s", r.Host)
	resp := map[string]interface{}{
		"version": "1.0",
		"base_urls": map[string]string{
			"api": base + "/api/smart_contract",
			"mcp": base + "/mcp",
		},
		"endpoints": []string{
			"/api/smart_contract/contracts",
			"/api/smart_contract/tasks",
			"/api/smart_contract/claims",
			"/api/smart_contract/submissions",
			"/api/smart_contract/events",
			"/api/open-contracts",
		},
		"tools": []string{
			"list_contracts", "get_contract", "get_contract_funding", "get_open_contracts",
			"list_tasks", "get_task", "claim_task", "submit_work", "get_task_proof", "get_task_status",
			"list_skills",
			"list_proposals", "get_proposal", "create_proposal", "approve_proposal", "publish_proposal",
			"list_submissions", "get_submission", "review_submission", "rework_submission",
			"list_events",
			"scan_image", "scan_block", "extract_message", "get_scanner_info",
		},
		"authentication": map[string]string{
			"type":        "api_key",
			"header_name": "X-API-Key",
			"required":    fmt.Sprintf("%t", s.apiKeys != nil),
		},
		"rate_limits": map[string]interface{}{
			"enabled":       false,
			"notes":         "rate limiting planned; not enforced by default",
			"recommended":   "10 rps claim, 5 rps submit (see roadmap)",
			"burst_example": 100,
		},
	}
	JSON(w, http.StatusOK, resp)
}

func splitCSV(v string) []string {
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func intFromQuery(r *http.Request, key string, def int) int {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return v
}

func int64FromQuery(r *http.Request, key string, def int64) int64 {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return def
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return def
	}
	return v
}

// recordEvent appends an event to the in-memory log with a small bounded buffer.
func (s *Server) recordEvent(evt smart_contract.Event) {
	const maxEvents = 200
	if evt.CreatedAt.IsZero() {
		evt.CreatedAt = time.Now()
	}
	s.eventsMu.Lock()
	defer s.eventsMu.Unlock()
	s.events = append([]smart_contract.Event{evt}, s.events...)
	if len(s.events) > maxEvents {
		s.events = s.events[:maxEvents]
	}
	s.broadcastEvent(evt)
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
				return s.publishProposalTasks(ctx, p.ID)
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

// publishProposalTasks publishes the tasks stored in a proposal into MCP tasks.
func (s *Server) publishProposalTasks(ctx context.Context, proposalID string) error {
	p, err := s.store.GetProposal(ctx, proposalID)
	if err != nil {
		return err
	}
	if len(p.Tasks) == 0 {
		// Try to derive tasks from metadata embedded_message.
		if em, ok := p.Metadata["embedded_message"].(string); ok && em != "" {
			p.Tasks = scstore.BuildTasksFromMarkdown(p.ID, em, p.VisiblePixelHash, p.BudgetSats, scstore.FundingAddressFromMeta(p.Metadata))
		}
		if len(p.Tasks) == 0 {
			return nil
		}
	}
	// Build a contract from the proposal, then upsert tasks.
	contract := smart_contract.Contract{
		ContractID:          p.ID,
		Title:               p.Title,
		TotalBudgetSats:     p.BudgetSats,
		GoalsCount:          1,
		AvailableTasksCount: len(p.Tasks),
		Status:              "active",
	}
	// Preserve hashes/funding if present.
	fundingAddr := scstore.FundingAddressFromMeta(p.Metadata)
	tasks := make([]smart_contract.Task, 0, len(p.Tasks))
	for _, t := range p.Tasks {
		task := t
		if task.ContractID == "" {
			task.ContractID = p.ID
		}
		if task.MerkleProof == nil && p.VisiblePixelHash != "" {
			task.MerkleProof = &smart_contract.MerkleProof{
				VisiblePixelHash:   p.VisiblePixelHash,
				FundedAmountSats:   p.BudgetSats / int64(len(p.Tasks)),
				FundingAddress:     fundingAddr,
				ConfirmationStatus: "provisional",
			}
		}
		if task.MerkleProof != nil && task.MerkleProof.FundingAddress == "" {
			task.MerkleProof.FundingAddress = fundingAddr
		}
		tasks = append(tasks, task)
	}
	if pg, ok := s.store.(interface {
		UpsertContractWithTasks(context.Context, smart_contract.Contract, []smart_contract.Task) error
	}); ok {
		if err := pg.UpsertContractWithTasks(ctx, contract, tasks); err != nil {
			return err
		}
		s.recordEvent(smart_contract.Event{
			Type:      "publish",
			EntityID:  proposalID,
			Actor:     "system",
			Message:   "proposal tasks published",
			CreatedAt: time.Now(),
		})
		return nil
	}
	return nil
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
			if err := s.store.ApproveProposal(r.Context(), id); err != nil {
				Error(w, http.StatusBadRequest, err.Error())
				return
			}
			// Publish tasks for this proposal if available.
			if err := s.publishProposalTasks(r.Context(), id); err != nil {
				log.Printf("failed to publish tasks for proposal %s: %v", id, err)
			}
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
				"message":     "Proposal approved; tasks published.",
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
			if err := s.store.CreateProposal(r.Context(), proposal); err != nil {
				Error(w, http.StatusBadRequest, err.Error())
				return
			}
			JSON(w, http.StatusCreated, map[string]interface{}{
				"proposal_id": proposal.ID,
				"status":      proposal.Status,
				"message":     "proposal created from pending ingestion",
			})
			return
		}

		// Manual creation path (not tied to ingestion).
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
		if body.ContractID != "" {
			body.Metadata["contract_id"] = body.ContractID
		}
		for i := range body.Tasks {
			if body.Tasks[i].TaskID == "" {
				body.Tasks[i].TaskID = body.ID + "-task-" + strconv.Itoa(i+1)
			}
			if body.Tasks[i].ContractID == "" && body.ContractID != "" {
				body.Tasks[i].ContractID = body.ContractID
			}
			if body.Tasks[i].Status == "" {
				body.Tasks[i].Status = "available"
			}
		}
		p := smart_contract.Proposal{
			ID:               body.ID,
			Title:            body.Title,
			DescriptionMD:    body.DescriptionMD,
			VisiblePixelHash: body.VisiblePixelHash,
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
		JSON(w, http.StatusCreated, map[string]interface{}{
			"proposal_id": p.ID,
			"status":      p.Status,
			"tasks":       len(p.Tasks),
			"budget_sats": p.BudgetSats,
		})
		return
	case http.MethodGet:
		if path == "" {
			minBudget := int64FromQuery(r, "min_budget_sats", 0)
			filter := smart_contract.ProposalFilter{
				Status:     r.URL.Query().Get("status"),
				Skills:     splitCSV(r.URL.Query().Get("skills")),
				MinBudget:  minBudget,
				ContractID: r.URL.Query().Get("contract_id"),
				MaxResults: intFromQuery(r, "limit", 0),
				Offset:     intFromQuery(r, "offset", 0),
			}
			proposals, err := s.store.ListProposals(r.Context(), filter)
			if err != nil {
				Error(w, http.StatusInternalServerError, err.Error())
				return
			}
			// hydrate submissions alongside tasks
			var taskIDs []string
			for _, p := range proposals {
				for _, t := range p.Tasks {
					taskIDs = append(taskIDs, t.TaskID)
				}
			}
			subs, _ := s.store.ListSubmissions(r.Context(), taskIDs)
			JSON(w, http.StatusOK, map[string]interface{}{
				"proposals":   proposals,
				"total":       len(proposals),
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
	if visible == "" && rec.ImageBase64 != "" {
		if h, err := hashBase64(rec.ImageBase64); err == nil {
			visible = h
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

// submissionReviewBody captures POST payload for reviewing submissions.
type submissionReviewBody struct {
	Action string `json:"action"` // review | approve | reject
	Notes  string `json:"notes"`
}

// submissionReworkBody captures POST payload for reworking submissions.
type submissionReworkBody struct {
	Deliverables map[string]interface{} `json:"deliverables"`
	Notes        string                 `json:"notes"`
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

			// We need to get all submissions to find the specific one
			// This is not optimal but works for the current store interface
			tasks, err := s.store.ListTasks(smart_contract.TaskFilter{})
			if err != nil {
				Error(w, http.StatusInternalServerError, err.Error())
				return
			}

			taskIDs := make([]string, len(tasks))
			for i, task := range tasks {
				taskIDs[i] = task.TaskID
			}

			submissions, err := s.store.ListSubmissions(r.Context(), taskIDs)
			if err != nil {
				Error(w, http.StatusInternalServerError, err.Error())
				return
			}

			log.Printf("Found %d submissions for contract", len(submissions))
			for _, sub := range submissions {
				log.Printf("Checking submission ID: %s == %s ?", sub.SubmissionID, submissionID)
				if sub.SubmissionID == submissionID {
					log.Printf("Found matching submission: %s", submissionID)
					JSON(w, http.StatusOK, sub)
					return
				}
			}

			log.Printf("No matching submission found for ID: %s", submissionID)
			Error(w, http.StatusNotFound, "submission not found")
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
			err := s.store.UpdateSubmissionStatus(ctx, submissionID, newStatus)
			if err != nil {
				if strings.Contains(err.Error(), "not found") {
					Error(w, http.StatusNotFound, "submission not found")
					return
				}
				Error(w, http.StatusInternalServerError, err.Error())
				return
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

			// Get the original submission to update it
			tasks, err := s.store.ListTasks(smart_contract.TaskFilter{})
			if err != nil {
				Error(w, http.StatusInternalServerError, err.Error())
				return
			}

			taskIDs := make([]string, len(tasks))
			for i, task := range tasks {
				taskIDs[i] = task.TaskID
			}

			submissions, err := s.store.ListSubmissions(r.Context(), taskIDs)
			if err != nil {
				Error(w, http.StatusInternalServerError, err.Error())
				return
			}

			var originalSubmission *smart_contract.Submission
			for _, sub := range submissions {
				if sub.SubmissionID == submissionID {
					originalSubmission = &sub
					break
				}
			}

			if originalSubmission == nil {
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

			// Reset status to pending_review
			ctx := r.Context()
			err = s.store.UpdateSubmissionStatus(ctx, submissionID, "pending_review")
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
