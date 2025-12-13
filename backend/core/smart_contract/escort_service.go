package smart_contract

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

// EscortService manages lifecycle of Merkle proofs for smart contracts
type EscortService struct {
	verifier          *MerkleProofVerifier
	scriptInterpreter *ScriptInterpreter
	checkInterval     time.Duration
	httpClient        *http.Client
}

// NewEscortService creates a new escort service
func NewEscortService(verifier *MerkleProofVerifier, scriptInterpreter *ScriptInterpreter) *EscortService {
	return &EscortService{
		verifier:          verifier,
		scriptInterpreter: scriptInterpreter,
		checkInterval:     5 * time.Minute, // Check every 5 minutes
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// EscortStatus represents the status of an escorted proof
type EscortStatus struct {
	TaskID             string                   `json:"task_id"`
	ProofStatus        string                   `json:"proof_status"` // provisional | confirming | confirmed | failed
	LastChecked        time.Time                `json:"last_checked"`
	VerificationResult *ProofVerificationResult `json:"verification_result,omitempty"`
	ScriptValidation   *ValidationResult        `json:"script_validation,omitempty"`
	NextAction         string                   `json:"next_action"`
	RetryCount         int                      `json:"retry_count"`
	MaxRetries         int                      `json:"max_retries"`
	Error              string                   `json:"error,omitempty"`
}

// Start begins the escort service
func (es *EscortService) Start(ctx context.Context) error {
	log.Printf("Starting smart contract escort service with %s check interval", es.checkInterval)

	// Start periodic checking
	ticker := time.NewTicker(es.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("Escort service stopped")
			return ctx.Err()
		case <-ticker.C:
			log.Printf("Escort service checking proofs...")
			// In a real implementation, this would scan and update proofs
		}
	}
}

// ValidateProof validates a single proof
func (es *EscortService) ValidateProof(proof *MerkleProof) (*EscortStatus, error) {
	status := &EscortStatus{
		TaskID:      proof.TxID, // Using txID as task identifier for now
		LastChecked: time.Now(),
		MaxRetries:  3,
	}

	log.Printf("Validating proof for tx %s", proof.TxID)

	// Step 1: Verify proof structure
	verificationResult, err := es.verifier.VerifyProof(proof)
	if err != nil {
		status.Error = fmt.Sprintf("Proof verification failed: %v", err)
		status.ProofStatus = "failed"
		status.NextAction = "manual_review_required"
		return status, nil
	}

	status.VerificationResult = verificationResult

	if !verificationResult.Valid {
		status.Error = verificationResult.Error
		status.ProofStatus = "failed"
		status.NextAction = "manual_review_required"
		return status, nil
	}

	// Step 2: Check confirmation status
	switch proof.ConfirmationStatus {
	case "provisional":
		status.ProofStatus = "confirming"
		status.NextAction = "awaiting_confirmation"
		log.Printf("Proof %s is provisional, awaiting confirmation", proof.TxID)
	case "confirmed":
		status.ProofStatus = "confirmed"
		status.NextAction = "monitor_contract_execution"
		log.Printf("Proof %s is confirmed", proof.TxID)
	default:
		status.ProofStatus = "unknown"
		status.NextAction = "verify_proof_status"
		log.Printf("Proof %s has unknown status: %s", proof.TxID, proof.ConfirmationStatus)
	}

	return status, nil
}

// RefreshProof refreshes a proof with current blockchain data
func (es *EscortService) RefreshProof(proof *MerkleProof) (*MerkleProof, error) {
	log.Printf("Refreshing proof for tx %s", proof.TxID)

	updatedProof, err := es.verifier.RefreshProof(proof)
	if err != nil {
		return nil, fmt.Errorf("failed to refresh proof: %v", err)
	}

	log.Printf("Proof refreshed for tx %s, new status: %s", proof.TxID, updatedProof.ConfirmationStatus)
	return updatedProof, nil
}

// MonitorProof monitors a proof over time
func (es *EscortService) MonitorProof(ctx context.Context, proof *MerkleProof, interval time.Duration) error {
	log.Printf("Starting proof monitoring for tx %s with %s interval", proof.TxID, interval)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("Proof monitoring stopped for tx %s", proof.TxID)
			return ctx.Err()
		case <-ticker.C:
			status, err := es.ValidateProof(proof)
			if err != nil {
				log.Printf("Proof validation error for tx %s: %v", proof.TxID, err)
				continue
			}

			// Log status
			statusJSON, _ := json.MarshalIndent(status, "", "  ")
			log.Printf("Proof status for tx %s:\n%s", proof.TxID, string(statusJSON))

			// If proof is confirmed and valid, we can stop monitoring
			if status.ProofStatus == "confirmed" && status.VerificationResult != nil && status.VerificationResult.Valid {
				log.Printf("Proof %s is confirmed and valid - monitoring complete", proof.TxID)
				return nil
			}
		}
	}
}

// GetServiceStatus returns the overall service status
func (es *EscortService) GetServiceStatus() map[string]any {
	return map[string]any{
		"service_name":   "smart_contract_escort",
		"status":         "running",
		"check_interval": es.checkInterval.String(),
		"started_at":     time.Now().Format(time.RFC3339),
		"version":        "1.0.0",
		"capabilities": []string{
			"proof_verification",
			"script_validation",
			"lifecycle_management",
			"dispute_detection",
			"payout_monitoring",
		},
	}
}

// ValidateBatchProofs validates multiple proofs
func (es *EscortService) ValidateBatchProofs(proofs []*MerkleProof) ([]*EscortStatus, error) {
	statuses := make([]*EscortStatus, len(proofs))

	for i, proof := range proofs {
		status, err := es.ValidateProof(proof)
		if err != nil {
			statuses[i] = &EscortStatus{
				TaskID: proof.TxID,
				Error:  fmt.Sprintf("Validation error: %v", err),
			}
		} else {
			statuses[i] = status
		}
	}

	return statuses, nil
}

// GetProofHealth returns health status of a proof
func (es *EscortService) GetProofHealth(proof *MerkleProof) map[string]any {
	health := make(map[string]any)

	// Basic proof info
	health["tx_id"] = proof.TxID
	health["block_height"] = proof.BlockHeight
	health["confirmation_status"] = proof.ConfirmationStatus
	health["funded_amount_sats"] = proof.FundedAmountSats

	// Verification result
	verificationResult, err := es.verifier.VerifyProof(proof)
	if err != nil {
		health["verification_error"] = err.Error()
		health["health_status"] = "error"
	} else {
		health["verification_valid"] = verificationResult.Valid
		if verificationResult.Valid {
			health["health_status"] = "healthy"
		} else {
			health["health_status"] = "unhealthy"
			health["verification_error"] = verificationResult.Error
		}
	}

	health["checked_at"] = time.Now().Format(time.RFC3339)

	return health
}

// SetCheckInterval updates the check interval
func (es *EscortService) SetCheckInterval(interval time.Duration) {
	es.checkInterval = interval
	log.Printf("Escort service check interval updated to %s", interval)
}

// Stop gracefully stops the escort service
func (es *EscortService) Stop() {
	log.Printf("Stopping smart contract escort service")
	// In a real implementation, this would clean up resources
}
