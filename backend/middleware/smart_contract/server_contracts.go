package smart_contract

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"stargate-backend/core/smart_contract"
	auth "stargate-backend/storage/auth"
	"stargate-backend/storage/ipfs"
)

func applyCreatorWallet(meta map[string]interface{}, apiKey string, apiKeys auth.APIKeyValidator) {
	if meta == nil {
		return
	}
	if _, ok := meta["creator_wallet"].(string); ok {
		return
	}
	if apiKeys != nil {
		if rec, ok := apiKeys.Get(apiKey); ok {
			if wallet := strings.TrimSpace(rec.Wallet); wallet != "" {
				meta["creator_wallet"] = wallet
			}
		}
	}
}

func (s *Server) enforceCreatorApproval(r *http.Request, proposal smart_contract.Proposal) error {
	apiKey := r.Header.Get("X-API-Key")

	// Get approver's wallet from API key
	var approverWallet string
	if s.apiKeys != nil {
		if approverRec, ok := s.apiKeys.Get(apiKey); ok {
			approverWallet = strings.TrimSpace(approverRec.Wallet)
		}
	}
	if approverWallet == "" {
		return fmt.Errorf("api key with wallet binding required for approval")
	}

	// 0. GLOBAL AUDITOR: Check if the bound wallet is the donation address
	donationAddr := strings.TrimSpace(os.Getenv("STARLIGHT_DONATION_ADDRESS"))
	if donationAddr != "" && strings.EqualFold(approverWallet, donationAddr) {
		log.Printf("AUTHORIZATION: Allowing approval for proposal %s based on Global Auditor status (%s)", proposal.ID, approverWallet)
		return nil
	}

	// 1. Check if matches Wish Creator by wallet
	visibleHash := proposalVisibleHash(proposal)
	if visibleHash != "" && s.ingestionSvc != nil {
		// Try both hash and wish-hash
		rec, err := s.ingestionSvc.Get(visibleHash)
		if err != nil {
			rec, _ = s.ingestionSvc.Get("wish-" + visibleHash)
		}

		if rec != nil && rec.Metadata != nil {
			if wishCreatorWallet, ok := rec.Metadata["creator_wallet"].(string); ok {
				if strings.EqualFold(strings.TrimSpace(wishCreatorWallet), approverWallet) {
					return nil
				}
			}
		}
	}

	// 2. Fallback: if no wish creator info exists at all, allow for now to prevent deadlock on old data
	hasWishCreatorInfo := false
	if visibleHash != "" && s.ingestionSvc != nil {
		rec, _ := s.ingestionSvc.Get(visibleHash)
		if rec != nil && rec.Metadata != nil {
			if _, ok := rec.Metadata["creator_wallet"].(string); ok {
				hasWishCreatorInfo = true
			}
		}
	}

	if !hasWishCreatorInfo {
		log.Printf("WARNING: allowing approval for proposal %s with NO wish creator info via REST", proposal.ID)
		return nil
	}

	return fmt.Errorf("approver wallet %s does not match wish creator", approverWallet)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	JSON(w, http.StatusOK, map[string]string{
		"donation_address": strings.TrimSpace(os.Getenv("STARLIGHT_DONATION_ADDRESS")),
	})
}

func (s *Server) handleStegoPayload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	cid := strings.TrimPrefix(r.URL.Path, "/api/smart_contract/stego/payload/")
	cid = strings.TrimSpace(cid)
	if cid == "" {
		Error(w, http.StatusBadRequest, "payload cid required")
		return
	}
	client := ipfs.NewClientFromEnv()
	if client == nil {
		Error(w, http.StatusServiceUnavailable, "IPFS is disabled")
		return
	}
	data, err := client.Cat(r.Context(), cid)
	if err != nil {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(data, &payload); err != nil {
		Error(w, http.StatusBadRequest, "payload decode failed")
		return
	}
	JSON(w, http.StatusOK, payload)
}

func (s *Server) handleContracts(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/smart_contract/contracts")
	parts := strings.Split(strings.Trim(path, "/"), "/")

	switch r.Method {
	case http.MethodGet:
		if path == "" || path == "/" {
			status := r.URL.Query().Get("status")
			skills := splitCSV(r.URL.Query().Get("skills"))
			filter := smart_contract.ContractFilter{
				Status:  status,
				Skills:  skills,
				Creator: r.URL.Query().Get("creator"),
			}
			contracts, err := s.store.ListContracts(filter)
			if err != nil {
				Error(w, http.StatusInternalServerError, err.Error())
				return
			}
			if !includeConfirmed(r) {
				filtered := make([]smart_contract.Contract, 0, len(contracts))
				for _, c := range contracts {
					_, proofs, err := s.store.ContractFunding(c.ContractID)
					if err != nil {
						log.Printf("contract funding lookup failed for %s: %v", c.ContractID, err)
						filtered = append(filtered, c)
						continue
					}
					if proofsConfirmed(proofs) {
						continue
					}
					filtered = append(filtered, c)
				}
				contracts = filtered
			}
			// Standardize on MCP-style full pagination shape (Cat 4.4)
			hasMore := false
			// Note: this handler's filter may not have Limit/Offset set in all paths; has_more is best-effort
			if len(contracts) > 0 {
				// simplistic; real has_more would require extra query
			}
			JSON(w, http.StatusOK, map[string]interface{}{
				"contracts": contracts,
				"total":     len(contracts),
				"limit":     0, // unknown in this path
				"offset":    0,
				"has_more":  hasMore,
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

		if len(parts) > 1 && parts[1] == "payment-details" {
			s.handlePaymentDetails(w, r, contractID)
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
		if len(parts) > 1 && parts[1] == "commitment-psbt" {
			contractID := parts[0]
			s.handleCommitmentPSBT(w, r, contractID)
			return
		}
		if len(parts) > 1 && parts[1] == "payment-details" {
			contractID := parts[0]
			s.handlePaymentDetails(w, r, contractID)
			return
		}
		if len(parts) > 1 && parts[1] == "rework" {
			contractID := parts[0]
			s.handleContractRework(w, r, contractID)
			return
		}
		Error(w, http.StatusNotFound, "unknown contract action")
	case http.MethodPatch:
		if len(parts) > 1 && parts[1] == "rework" && len(parts) > 2 && parts[2] != "" {
			contractID := parts[0]
			requestID := parts[2]
			s.handleResolveContractRework(w, r, contractID, requestID)
			return
		}
		Error(w, http.StatusNotFound, "unknown contract action")
	default:
		Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleGetContractReworkRequests returns all rework requests for a contract.
func (s *Server) handleGetContractReworkRequests(w http.ResponseWriter, r *http.Request, contractID string) {
	reworkReqs, err := s.store.GetContractReworkRequests(r.Context(), contractID)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]interface{}{
		"rework_requests": reworkReqs,
	})
}

// handleContractRework creates a new rework request for a contract.
func (s *Server) handleContractRework(w http.ResponseWriter, r *http.Request, contractID string) {
	if r.Header.Get("Content-Type") != "" && !strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		Error(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return
	}

	var body struct {
		Notes string `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if strings.TrimSpace(body.Notes) == "" {
		Error(w, http.StatusBadRequest, "notes are required")
		return
	}

	apiKey := r.Header.Get("X-API-Key")
	var requester string
	if apiKey != "" && s.apiKeys != nil {
		if rec, ok := s.apiKeys.Get(apiKey); ok {
			requester = strings.TrimSpace(rec.Wallet)
		}
	}

	if requester == "" {
		Error(w, http.StatusForbidden, "authenticated user required")
		return
	}

	reworkReq, err := s.store.CreateContractReworkRequest(r.Context(), contractID, requester, body.Notes)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	JSON(w, http.StatusCreated, reworkReq)
}

// handleResolveContractRework resolves/closes a rework request.
func (s *Server) handleResolveContractRework(w http.ResponseWriter, r *http.Request, contractID, requestID string) {
	apiKey := r.Header.Get("X-API-Key")
	var requester string
	if apiKey != "" && s.apiKeys != nil {
		if rec, ok := s.apiKeys.Get(apiKey); ok {
			requester = strings.TrimSpace(rec.Wallet)
		}
	}

	if requester == "" {
		Error(w, http.StatusForbidden, "authenticated user required")
		return
	}

	// Get rework requests to verify the requester is the original requester
	reworkReqs, err := s.store.GetContractReworkRequests(r.Context(), contractID)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Verify the requester is the original creator of this rework request
	authorized := false
	for _, req := range reworkReqs {
		if req.RequestID == requestID {
			// Allow the original requester to resolve their own rework request
			if strings.EqualFold(req.Requester, requester) {
				authorized = true
			}
			break
		}
	}

	if !authorized {
		Error(w, http.StatusForbidden, "only the original requester can resolve this rework request")
		return
	}

	err = s.store.ResolveContractReworkRequest(r.Context(), contractID, requestID)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"message": "rework request resolved",
	})
}
