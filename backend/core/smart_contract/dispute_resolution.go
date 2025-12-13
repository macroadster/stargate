package smart_contract

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"time"
)

// DisputeResolution handles disputes and arbitration for smart contracts
type DisputeResolution struct {
	scriptInterpreter *ScriptInterpreter
	verifier          *MerkleProofVerifier
	arbitrators       []Arbitrator
	disputeTimeout    time.Duration
}

// NewDisputeResolution creates a new dispute resolution system
func NewDisputeResolution(scriptInterpreter *ScriptInterpreter, verifier *MerkleProofVerifier) *DisputeResolution {
	return &DisputeResolution{
		scriptInterpreter: scriptInterpreter,
		verifier:          verifier,
		arbitrators:       []Arbitrator{},
		disputeTimeout:    7 * 24 * time.Hour, // 7 days
	}
}

// Arbitrator represents an arbitration participant
type Arbitrator struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	PublicKey   string   `json:"public_key"`
	Reputation  float64  `json:"reputation"`
	Specialties []string `json:"specialties"`
	IsActive    bool     `json:"is_active"`
	VoteWeight  float64  `json:"vote_weight"`
}

// Dispute represents a contract dispute
type Dispute struct {
	DisputeID   string                     `json:"dispute_id"`
	ContractID  string                     `json:"contract_id"`
	TaskID      string                     `json:"task_id"`
	Initiator   string                     `json:"initiator"`  // who initiated the dispute
	Respondent  string                     `json:"respondent"` // who is being disputed
	Type        DisputeType                `json:"type"`
	Status      DisputeStatus              `json:"status"`
	Description string                     `json:"description"`
	Evidence    []DisputeEvidence          `json:"evidence"`
	Arbitrators []string                   `json:"arbitrators"` // selected arbitrators
	Votes       map[string]ArbitrationVote `json:"votes"`
	Resolution  *DisputeResolutionResult   `json:"resolution,omitempty"`
	CreatedAt   time.Time                  `json:"created_at"`
	Deadline    time.Time                  `json:"deadline"`
	ResolvedAt  *time.Time                 `json:"resolved_at,omitempty"`
}

// DisputeType represents the type of dispute
type DisputeType string

const (
	DisputeTypeQuality   DisputeType = "quality"
	DisputeTypePayment   DisputeType = "payment"
	DisputeTypeDelivery  DisputeType = "delivery"
	DisputeTypeBehavior  DisputeType = "behavior"
	DisputeTypeTechnical DisputeType = "technical"
	DisputeTypeOther     DisputeType = "other"
)

// DisputeStatus represents the current status of a dispute
type DisputeStatus string

const (
	DisputeStatusInitiated   DisputeStatus = "initiated"
	DisputeStatusResponded   DisputeStatus = "responded"
	DisputeStatusArbitrating DisputeStatus = "arbitrating"
	DisputeStatusVoting      DisputeStatus = "voting"
	DisputeStatusCompleted   DisputeStatus = "completed"
	DisputeStatusExpired     DisputeStatus = "expired"
	DisputeStatusCanceled    DisputeStatus = "canceled"
)

// DisputeEvidence represents evidence submitted in a dispute
type DisputeEvidence struct {
	ID          string         `json:"id"`
	Submitter   string         `json:"submitter"`
	Type        EvidenceType   `json:"type"`
	Content     string         `json:"content"`
	Metadata    map[string]any `json:"metadata"`
	SubmittedAt time.Time      `json:"submitted_at"`
	IsValid     bool           `json:"is_valid"`
	Weight      float64        `json:"weight"` // weight in arbitration decision
}

// EvidenceType represents the type of evidence
type EvidenceType string

const (
	EvidenceTypeText       EvidenceType = "text"
	EvidenceTypeImage      EvidenceType = "image"
	EvidenceTypeDocument   EvidenceType = "document"
	EvidenceTypeCode       EvidenceType = "code"
	EvidenceTypeScreenshot EvidenceType = "screenshot"
	EvidenceTypeLogs       EvidenceType = "logs"
	EvidenceTypeOther      EvidenceType = "other"
)

// ArbitrationVote represents an arbitrator's vote
type ArbitrationVote struct {
	ArbitratorID string              `json:"arbitrator_id"`
	Decision     ArbitrationDecision `json:"decision"`
	Reason       string              `json:"reason"`
	EvidenceIDs  []string            `json:"evidence_ids"`
	Confidence   float64             `json:"confidence"` // 0.0 to 1.0
	VotedAt      time.Time           `json:"voted_at"`
	Signature    string              `json:"signature,omitempty"` // digital signature of vote
}

// ArbitrationDecision represents the possible arbitration decisions
type ArbitrationDecision string

const (
	DecisionFavorInitiator  ArbitrationDecision = "favor_initiator"
	DecisionFavorRespondent ArbitrationDecision = "favor_respondent"
	DecisionPartialRefund   ArbitrationDecision = "partial_refund"
	DecisionFullRefund      ArbitrationDecision = "full_refund"
	DecisionNoAction        ArbitrationDecision = "no_action"
	DecisionTechnicalIssue  ArbitrationDecision = "technical_issue"
)

// DisputeResolutionResult represents the final resolution of a dispute
type DisputeResolutionResult struct {
	Decision        ArbitrationDecision `json:"decision"`
	Reason          string              `json:"reason"`
	PayoutSplit     map[string]int64    `json:"payout_split"` // participant -> amount in sats
	EvidenceSummary EvidenceSummary     `json:"evidence_summary"`
	ArbitratorVotes []ArbitrationVote   `json:"arbitrator_votes"`
	ResolvedAt      time.Time           `json:"resolved_at"`
	AppealDeadline  *time.Time          `json:"appeal_deadline,omitempty"`
}

// EvidenceSummary summarizes evidence in a dispute
type EvidenceSummary struct {
	TotalEvidence      int            `json:"total_evidence"`
	ValidEvidence      int            `json:"valid_evidence"`
	InitiatorEvidence  int            `json:"initiator_evidence"`
	RespondentEvidence int            `json:"respondent_evidence"`
	EvidenceTypes      map[string]int `json:"evidence_types"`
}

// CreateDispute creates a new dispute
func (dr *DisputeResolution) CreateDispute(ctx context.Context, dispute *Dispute) error {
	log.Printf("Creating dispute %s for contract %s", dispute.DisputeID, dispute.ContractID)

	// Validate dispute
	if err := dr.validateDispute(dispute); err != nil {
		return fmt.Errorf("invalid dispute: %v", err)
	}

	// Set initial status and deadline
	dispute.Status = DisputeStatusInitiated
	dispute.CreatedAt = time.Now()
	dispute.Deadline = time.Now().Add(dr.disputeTimeout)

	// Select arbitrators
	arbitrators, err := dr.selectArbitrators(ctx, dispute)
	if err != nil {
		return fmt.Errorf("failed to select arbitrators: %v", err)
	}

	dispute.Arbitrators = arbitrators
	dispute.Votes = make(map[string]ArbitrationVote)

	log.Printf("Dispute %s created with %d arbitrators", dispute.DisputeID, len(arbitrators))
	return nil
}

// validateDispute validates a dispute before creation
func (dr *DisputeResolution) validateDispute(dispute *Dispute) error {
	if dispute.DisputeID == "" {
		return fmt.Errorf("dispute ID is required")
	}

	if dispute.ContractID == "" {
		return fmt.Errorf("contract ID is required")
	}

	if dispute.Initiator == "" || dispute.Respondent == "" {
		return fmt.Errorf("initiator and respondent are required")
	}

	if dispute.Initiator == dispute.Respondent {
		return fmt.Errorf("initiator and respondent cannot be the same")
	}

	if dispute.Description == "" {
		return fmt.Errorf("description is required")
	}

	// Validate dispute type
	validTypes := []DisputeType{
		DisputeTypeQuality, DisputeTypePayment, DisputeTypeDelivery,
		DisputeTypeBehavior, DisputeTypeTechnical, DisputeTypeOther,
	}

	isValidType := false
	for _, validType := range validTypes {
		if dispute.Type == validType {
			isValidType = true
			break
		}
	}

	if !isValidType {
		return fmt.Errorf("invalid dispute type: %s", dispute.Type)
	}

	return nil
}

// selectArbitrators selects appropriate arbitrators for a dispute
func (dr *DisputeResolution) selectArbitrators(_ context.Context, dispute *Dispute) ([]string, error) {
	// For now, return mock arbitrator IDs
	// In a real implementation, this would:
	// 1. Filter by availability and specialization
	// 2. Check for conflicts of interest
	// 3. Select odd number (3, 5, etc.)
	// 4. Consider reputation and vote weight

	arbitratorIDs := []string{"arb-001", "arb-002", "arb-003"}
	log.Printf("Selected arbitrators for dispute %s: %v", dispute.DisputeID, arbitratorIDs)

	return arbitratorIDs, nil
}

// SubmitEvidence submits evidence for a dispute
func (dr *DisputeResolution) SubmitEvidence(ctx context.Context, disputeID, submitterID string, evidence *DisputeEvidence) error {
	log.Printf("Submitting evidence for dispute %s by %s", disputeID, submitterID)

	// Validate evidence
	if err := dr.validateEvidence(evidence); err != nil {
		return fmt.Errorf("invalid evidence: %v", err)
	}

	// Set evidence metadata
	evidence.SubmittedAt = time.Now()
	evidence.ID = fmt.Sprintf("ev-%d", time.Now().UnixNano())

	// In a real implementation, this would store the evidence
	// For now, just log it
	evidenceJSON, _ := json.MarshalIndent(evidence, "", "  ")
	log.Printf("Evidence submitted: %s", string(evidenceJSON))

	return nil
}

// validateEvidence validates evidence before submission
func (dr *DisputeResolution) validateEvidence(evidence *DisputeEvidence) error {
	if evidence.Content == "" {
		return fmt.Errorf("evidence content is required")
	}

	// Validate evidence type
	validTypes := []EvidenceType{
		EvidenceTypeText, EvidenceTypeImage, EvidenceTypeDocument,
		EvidenceTypeCode, EvidenceTypeScreenshot, EvidenceTypeLogs, EvidenceTypeOther,
	}

	isValidType := false
	for _, validType := range validTypes {
		if evidence.Type == validType {
			isValidType = true
			break
		}
	}

	if !isValidType {
		return fmt.Errorf("invalid evidence type: %s", evidence.Type)
	}

	// Set default weight if not specified
	if evidence.Weight == 0 {
		evidence.Weight = 1.0
	}

	return nil
}

// CastVote records an arbitrator's vote
func (dr *DisputeResolution) CastVote(ctx context.Context, disputeID, arbitratorID string, vote *ArbitrationVote) error {
	log.Printf("Casting vote for dispute %s by arbitrator %s", disputeID, arbitratorID)

	// Validate vote
	if err := dr.validateVote(vote); err != nil {
		return fmt.Errorf("invalid vote: %v", err)
	}

	// Set vote metadata
	vote.ArbitratorID = arbitratorID
	vote.VotedAt = time.Now()

	// In a real implementation, this would:
	// 1. Verify arbitrator is authorized for this dispute
	// 2. Validate vote signature
	// 3. Store the vote
	// 4. Update dispute status if all votes received

	voteJSON, _ := json.MarshalIndent(vote, "", "  ")
	log.Printf("Vote cast: %s", string(voteJSON))

	return nil
}

// validateVote validates a vote before casting
func (dr *DisputeResolution) validateVote(vote *ArbitrationVote) error {
	if vote.ArbitratorID == "" {
		return fmt.Errorf("arbitrator ID is required")
	}

	// Validate decision
	validDecisions := []ArbitrationDecision{
		DecisionFavorInitiator, DecisionFavorRespondent,
		DecisionPartialRefund, DecisionFullRefund,
		DecisionNoAction, DecisionTechnicalIssue,
	}

	isValidDecision := false
	for _, validDecision := range validDecisions {
		if vote.Decision == validDecision {
			isValidDecision = true
			break
		}
	}

	if !isValidDecision {
		return fmt.Errorf("invalid decision: %s", vote.Decision)
	}

	if vote.Reason == "" {
		return fmt.Errorf("reason is required")
	}

	// Validate confidence
	if vote.Confidence < 0.0 || vote.Confidence > 1.0 {
		return fmt.Errorf("confidence must be between 0.0 and 1.0")
	}

	return nil
}

// ResolveDispute resolves a dispute based on arbitrator votes
func (dr *DisputeResolution) ResolveDispute(ctx context.Context, dispute *Dispute) (*DisputeResolutionResult, error) {
	log.Printf("Resolving dispute %s", dispute.DisputeID)

	// Validate that voting is complete
	if len(dispute.Votes) < 3 {
		return nil, fmt.Errorf("insufficient votes for resolution (need at least 3)")
	}

	// Tally votes
	decision, reason := dr.tallyVotes(dispute.Votes)

	// Create resolution result
	resolution := &DisputeResolutionResult{
		Decision:        decision,
		Reason:          reason,
		ArbitratorVotes: dr.convertVotesToArray(dispute.Votes),
		EvidenceSummary: dr.summarizeEvidence(dispute.Evidence),
		ResolvedAt:      time.Now(),
	}

	// Calculate payout split based on decision
	payoutSplit := dr.calculatePayoutSplit(dispute, decision)
	resolution.PayoutSplit = payoutSplit

	// Set appeal deadline (14 days)
	appealDeadline := time.Now().Add(14 * 24 * time.Hour)
	resolution.AppealDeadline = &appealDeadline

	// Update dispute status
	dispute.Status = DisputeStatusCompleted
	dispute.Resolution = resolution
	dispute.ResolvedAt = &resolution.ResolvedAt

	resolutionJSON, _ := json.MarshalIndent(resolution, "", "  ")
	log.Printf("Dispute resolved: %s", string(resolutionJSON))

	return resolution, nil
}

// tallyVotes tallies arbitrator votes to determine outcome
func (dr *DisputeResolution) tallyVotes(votes map[string]ArbitrationVote) (ArbitrationDecision, string) {
	voteCounts := make(map[ArbitrationDecision]int)
	totalWeight := 0.0
	weightedVotes := make(map[ArbitrationDecision]float64)

	// Count votes and calculate weighted totals
	for _, vote := range votes {
		voteCounts[vote.Decision]++
		totalWeight += vote.Confidence
		weightedVotes[vote.Decision] += vote.Confidence
	}

	// Find decision with highest weighted vote
	var winningDecision ArbitrationDecision
	maxWeight := 0.0

	for decision, weight := range weightedVotes {
		if weight > maxWeight {
			maxWeight = weight
			winningDecision = decision
		}
	}

	// Handle ties
	tiedDecisions := []ArbitrationDecision{}
	for decision, weight := range weightedVotes {
		if math.Abs(weight-maxWeight) < 0.001 { // Allow for floating point precision
			tiedDecisions = append(tiedDecisions, decision)
		}
	}

	reason := fmt.Sprintf("Decision: %s with %d votes, weight: %.2f",
		winningDecision, voteCounts[winningDecision], maxWeight)

	if len(tiedDecisions) > 1 {
		reason += fmt.Sprintf(" (Tie with: %v)", tiedDecisions)
	}

	return winningDecision, reason
}

// calculatePayoutSplit calculates how to distribute funds based on decision
func (dr *DisputeResolution) calculatePayoutSplit(dispute *Dispute, decision ArbitrationDecision) map[string]int64 {
	// This is a simplified payout calculation
	// In a real implementation, this would consider:
	// 1. Contract terms
	// 2. Evidence validity
	// 3. Partial fault allocation
	// 4. Fees and costs

	payoutSplit := make(map[string]int64)

	switch decision {
	case DecisionFavorInitiator:
		// Initiator gets 100% (minus arbitration fees)
		payoutSplit[dispute.Initiator] = 1000000 // 0.01 BTC example
		payoutSplit[dispute.Respondent] = 0

	case DecisionFavorRespondent:
		// Respondent gets 100% (minus arbitration fees)
		payoutSplit[dispute.Initiator] = 0
		payoutSplit[dispute.Respondent] = 1000000

	case DecisionPartialRefund:
		// Split 50/50
		payoutSplit[dispute.Initiator] = 500000
		payoutSplit[dispute.Respondent] = 500000

	case DecisionFullRefund:
		// Return to original funder (simplified)
		payoutSplit[dispute.Initiator] = 1000000
		payoutSplit[dispute.Respondent] = 0

	case DecisionNoAction:
		// Return funds to both parties proportionally
		payoutSplit[dispute.Initiator] = 500000
		payoutSplit[dispute.Respondent] = 500000

	default:
		// Default to no action
		payoutSplit[dispute.Initiator] = 0
		payoutSplit[dispute.Respondent] = 0
	}

	return payoutSplit
}

// summarizeEvidence creates a summary of evidence in a dispute
func (dr *DisputeResolution) summarizeEvidence(evidence []DisputeEvidence) EvidenceSummary {
	summary := EvidenceSummary{
		TotalEvidence: len(evidence),
		EvidenceTypes: make(map[string]int),
	}

	initiatorCount := 0
	respondentCount := 0

	for _, ev := range evidence {
		if ev.IsValid {
			summary.ValidEvidence++
		}

		summary.EvidenceTypes[string(ev.Type)]++

		// Count by submitter (simplified - would need submitter info)
		if len(evidence) > 0 {
			if evidence[0].Submitter == ev.Submitter {
				initiatorCount++
			} else {
				respondentCount++
			}
		}
	}

	summary.InitiatorEvidence = initiatorCount
	summary.RespondentEvidence = respondentCount

	return summary
}

// convertVotesToArray converts vote map to array for JSON serialization
func (dr *DisputeResolution) convertVotesToArray(votes map[string]ArbitrationVote) []ArbitrationVote {
	voteArray := make([]ArbitrationVote, 0, len(votes))
	i := 0
	for _, vote := range votes {
		voteArray[i] = vote
		i++
	}
	return voteArray
}

// AppealDispute handles an appeal of a dispute resolution
func (dr *DisputeResolution) AppealDispute(ctx context.Context, disputeID string, appealReason string, appealEvidence []DisputeEvidence) error {
	log.Printf("Appealing dispute %s: %s", disputeID, appealReason)

	// Validate appeal deadline
	// In a real implementation, this would check the dispute resolution
	// For now, just log the appeal

	appealJSON, _ := json.Marshal(map[string]any{
		"dispute_id":      disputeID,
		"appeal_reason":   appealReason,
		"appeal_evidence": appealEvidence,
		"appealed_at":     time.Now(),
	})

	log.Printf("Appeal submitted: %s", string(appealJSON))

	return nil
}

// GetDisputeStatus returns the current status of a dispute
func (dr *DisputeResolution) GetDisputeStatus(_ context.Context, disputeID string) (map[string]any, error) {
	status := map[string]any{
		"dispute_id": disputeID,
		"service":    "dispute_resolution",
		"timestamp":  time.Now().Format(time.RFC3339),
		"version":    "1.0.0",
	}

	// In a real implementation, this would fetch the actual dispute status
	// For now, return a mock status
	status["status"] = "arbitrating"
	status["arbitrators_count"] = 3
	status["evidence_count"] = 5
	status["deadline"] = time.Now().Add(48 * time.Hour).Format(time.RFC3339)

	return status, nil
}

// AddArbitrator adds a new arbitrator to the system
func (dr *DisputeResolution) AddArbitrator(arbitrator Arbitrator) error {
	// Validate arbitrator
	if arbitrator.ID == "" || arbitrator.Name == "" || arbitrator.PublicKey == "" {
		return fmt.Errorf("arbitrator ID, name, and public key are required")
	}

	if arbitrator.Reputation < 0.0 || arbitrator.Reputation > 10.0 {
		return fmt.Errorf("reputation must be between 0.0 and 10.0")
	}

	if arbitrator.VoteWeight <= 0.0 || arbitrator.VoteWeight > 5.0 {
		return fmt.Errorf("vote weight must be between 0.0 and 5.0")
	}

	// In a real implementation, this would store the arbitrator
	// For now, just log it
	arbitratorJSON, _ := json.MarshalIndent(arbitrator, "", "  ")
	log.Printf("Arbitrator added: %s", string(arbitratorJSON))

	return nil
}

// GetArbitrators returns all available arbitrators
func (dr *DisputeResolution) GetArbitrators() []Arbitrator {
	// In a real implementation, this would fetch from storage
	// For now, return mock data
	return []Arbitrator{
		{
			ID:          "arb-001",
			Name:        "Alice Arbitrator",
			PublicKey:   "02abc123...",
			Reputation:  4.5,
			Specialties: []string{"quality", "delivery"},
			IsActive:    true,
			VoteWeight:  1.0,
		},
		{
			ID:          "arb-002",
			Name:        "Bob Arbitrator",
			PublicKey:   "02def456...",
			Reputation:  3.8,
			Specialties: []string{"payment", "technical"},
			IsActive:    true,
			VoteWeight:  1.0,
		},
		{
			ID:          "arb-003",
			Name:        "Charlie Arbitrator",
			PublicKey:   "02ghi789...",
			Reputation:  4.2,
			Specialties: []string{"behavior", "other"},
			IsActive:    false,
			VoteWeight:  0.5,
		},
	}
}
