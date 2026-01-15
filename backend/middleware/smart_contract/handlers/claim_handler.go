package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"stargate-backend/core/smart_contract"
	smartstore "stargate-backend/middleware/smart_contract"
	"stargate-backend/middleware/smart_contract/middleware"
	"stargate-backend/storage/auth"
)

// ClaimHandler handles claim-related HTTP endpoints
type ClaimHandler struct {
	store   smartstore.Store
	apiKeys auth.APIKeyValidator
}

// NewClaimHandler creates a new claim handler
func NewClaimHandler(store smartstore.Store, apiKeys auth.APIKeyValidator) *ClaimHandler {
	return &ClaimHandler{
		store:   store,
		apiKeys: apiKeys,
	}
}

// ClaimTask handles POST /tasks/{id}/claim
func (h *ClaimHandler) ClaimTask(w http.ResponseWriter, r *http.Request, taskID string) {
	if ct := r.Header.Get("Content-Type"); ct != "" && !strings.Contains(ct, "application/json") {
		middleware.Error(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return
	}

	var body struct {
		AiIdentifier        string     `json:"ai_identifier"`
		Wallet              string     `json:"wallet_address,omitempty"`
		EstimatedCompletion *time.Time `json:"estimated_completion,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		middleware.Error(w, http.StatusBadRequest, "invalid json")
		return
	}

	if body.AiIdentifier == "" {
		middleware.Error(w, http.StatusBadRequest, "ai_identifier required")
		return
	}

	// Validate task and contract status
	if err := h.validateTaskStatus(r.Context(), taskID); err != nil {
		middleware.Error(w, http.StatusConflict, err.Error())
		return
	}

	contractorWallet := strings.TrimSpace(body.Wallet)
	if h.apiKeys != nil {
		apiKey := r.Header.Get("X-API-Key")
		if contractorWallet != "" {
			if getter, ok := h.apiKeys.(interface {
				Get(string) (auth.APIKey, bool)
			}); ok {
				if rec, ok := getter.Get(apiKey); ok {
					if strings.TrimSpace(rec.Wallet) != "" && rec.Wallet != contractorWallet {
						middleware.Error(w, http.StatusForbidden, "wallet already bound; rebind requires verification")
						return
					}
				}
			}
			if updater, ok := h.apiKeys.(auth.APIKeyWalletUpdater); ok {
				if _, err := updater.UpdateWallet(apiKey, contractorWallet); err != nil {
					middleware.Error(w, http.StatusBadRequest, "failed to bind wallet to api key")
					return
				}
			}
		}
		if rec, ok := h.apiKeys.Get(apiKey); ok && strings.TrimSpace(rec.Wallet) != "" {
			contractorWallet = strings.TrimSpace(rec.Wallet)
		}
	}

	claim, err := h.store.ClaimTask(taskID, body.AiIdentifier, contractorWallet, body.EstimatedCompletion)
	if err != nil {
		if err.Error() == "task not found" {
			// Attempt to publish tasks lazily from proposals that reference this task id, then retry.
			if h.tryPublishTasksForTaskID(r.Context(), taskID) == nil {
				if retry, retryErr := h.store.ClaimTask(taskID, body.AiIdentifier, contractorWallet, body.EstimatedCompletion); retryErr == nil {
					claim = retry
					err = nil
				} else {
					err = retryErr
				}
			}
			if err == nil {
				goto claim_success
			}
		}
		if err.Error() == "task not found" {
			middleware.Error(w, http.StatusNotFound, err.Error())
			return
		}
		if err.Error() == "task already claimed by another agent" || err.Error() == "task is not available for claiming" {
			middleware.Error(w, http.StatusConflict, err.Error())
			return
		}
		middleware.Error(w, http.StatusBadRequest, err.Error())
		return
	}

claim_success:
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":    true,
		"claim_id":   claim.ClaimID,
		"expires_at": claim.ExpiresAt,
		"message":    "Task reserved. Submit work before expiration.",
	})
}

// validateTaskStatus validates that task and associated contract/proposal are not confirmed
func (h *ClaimHandler) validateTaskStatus(ctx context.Context, taskID string) error {
	task, err := h.store.GetTask(taskID)
	if err != nil {
		return err
	}

	if strings.TrimSpace(task.ContractID) != "" {
		if contract, err := h.store.GetContract(task.ContractID); err == nil {
			status := strings.ToLower(strings.TrimSpace(contract.Status))
			if status == "confirmed" || status == "published" {
				return fmt.Errorf("task claims closed for confirmed contract")
			}
		}

		if proposals, err := h.store.ListProposals(ctx, smart_contract.ProposalFilter{ContractID: task.ContractID}); err == nil {
			for _, p := range proposals {
				if strings.EqualFold(strings.TrimSpace(p.Status), "confirmed") {
					return fmt.Errorf("task claims closed for confirmed proposal")
				}
			}
		}
	}

	return nil
}

// tryPublishTasksForTaskID attempts to publish tasks from proposals
func (h *ClaimHandler) tryPublishTasksForTaskID(ctx context.Context, taskID string) error {
	// TODO: Extract this logic from original server.go
	// This is a placeholder for the publishing logic
	return fmt.Errorf("publishing logic not yet extracted")
}

// Claims handles GET /claims
func (h *ClaimHandler) Claims(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		middleware.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// TODO: Implement claims listing logic
	middleware.Error(w, http.StatusNotImplemented, "claims listing not yet implemented")
}
