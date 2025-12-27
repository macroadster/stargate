package smart_contract

import "time"

// Contract captures a goal contract summary.
type Contract struct {
	ContractID          string   `json:"contract_id"`
	Title               string   `json:"title"`
	TotalBudgetSats     int64    `json:"total_budget_sats"`
	GoalsCount          int      `json:"goals_count"`
	AvailableTasksCount int      `json:"available_tasks_count"`
	Status              string   `json:"status"` // created | active | funded | expired
	Skills              []string `json:"skills,omitempty"`
}

// Task describes a specific unit of work an AI can claim.
type Task struct {
	TaskID           string            `json:"task_id"`
	ContractID       string            `json:"contract_id"`
	GoalID           string            `json:"goal_id"`
	Title            string            `json:"title"`
	Description      string            `json:"description"`
	BudgetSats       int64             `json:"budget_sats"`
	Skills           []string          `json:"skills_required"`
	Status           string            `json:"status"` // available | claimed | submitted | approved | published
	ClaimedBy        string            `json:"claimed_by,omitempty"`
	ContractorWallet string            `json:"contractor_wallet,omitempty"`
	ClaimedAt        *time.Time        `json:"claimed_at,omitempty"`
	ClaimExpires     *time.Time        `json:"claim_expires_at,omitempty"`
	ActiveClaimID    string            `json:"active_claim_id,omitempty"`
	Difficulty       string            `json:"difficulty,omitempty"`
	EstimatedHours   int               `json:"estimated_hours,omitempty"`
	Requirements     map[string]string `json:"requirements,omitempty"`
	MerkleProof      *MerkleProof      `json:"merkle_proof,omitempty"`
}

// MerkleProof represents the payment proof for a funded task.
type MerkleProof struct {
	TxID                   string      `json:"tx_id"`
	BlockHeight            int64       `json:"block_height"`
	BlockHeaderMerkleRoot  string      `json:"block_header_merkle_root"`
	ProofPath              []ProofNode `json:"proof_path"`
	VisiblePixelHash       string      `json:"visible_pixel_hash,omitempty"`
	ContractorWallet       string      `json:"contractor_wallet,omitempty"`
	FundedAmountSats       int64       `json:"funded_amount_sats"`
	FundingAddress         string      `json:"funding_address,omitempty"`
	CommitmentRedeemScript string      `json:"commitment_redeem_script,omitempty"`
	CommitmentRedeemHash   string      `json:"commitment_redeem_hash,omitempty"`
	CommitmentAddress      string      `json:"commitment_address,omitempty"`
	CommitmentVout         uint32      `json:"commitment_vout,omitempty"`
	CommitmentSats         int64       `json:"commitment_sats,omitempty"`
	SweepTxID              string      `json:"sweep_tx_id,omitempty"`
	SweepStatus            string      `json:"sweep_status,omitempty"`
	SweepError             string      `json:"sweep_error,omitempty"`
	SweepAttemptedAt       *time.Time  `json:"sweep_attempted_at,omitempty"`
	ConfirmationStatus     string      `json:"confirmation_status"` // provisional | confirmed
	SeenAt                 time.Time   `json:"seen_at"`
	ConfirmedAt            *time.Time  `json:"confirmed_at,omitempty"`
}

// ProofNode represents a single step in a Merkle proof path.
type ProofNode struct {
	Hash      string `json:"hash"`
	Direction string `json:"direction"` // left | right
}

// Claim captures task reservation info.
type Claim struct {
	ClaimID      string    `json:"claim_id"`
	TaskID       string    `json:"task_id"`
	AiIdentifier string    `json:"ai_identifier"`
	Status       string    `json:"status"` // active | submitted | complete | expired | rejected
	ExpiresAt    time.Time `json:"expires_at"`
	CreatedAt    time.Time `json:"created_at"`
}

// Submission contains a work submission reference.
type Submission struct {
	SubmissionID    string         `json:"submission_id"`
	ClaimID         string         `json:"claim_id"`
	TaskID          string         `json:"task_id,omitempty"`
	Status          string         `json:"status"` // pending_review | reviewed | approved | rejected
	Deliverables    map[string]any `json:"deliverables,omitempty"`
	CompletionProof map[string]any `json:"completion_proof,omitempty"`
	RejectionReason string         `json:"rejection_reason,omitempty"`
	RejectionType   string         `json:"rejection_type,omitempty"`
	RejectedAt      *time.Time     `json:"rejected_at,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
}

// ContractFilter captures list filters for contracts.
type ContractFilter struct {
	Status       string
	Skills       []string
	Creator      string
	AiIdentifier string
}

// TaskFilter captures simple query params for listing tasks.
type TaskFilter struct {
	Skills        []string
	MaxDifficulty string
	MinBudgetSats int64
	Limit         int
	Offset        int
	Status        string
	ContractID    string
	ClaimedBy     string
}

// Proposal represents a human/markdown wish that must be approved before tasks are published.
type Proposal struct {
	ID               string         `json:"id"`
	Title            string         `json:"title"`
	DescriptionMD    string         `json:"description_md"`
	VisiblePixelHash string         `json:"visible_pixel_hash,omitempty"`
	BudgetSats       int64          `json:"budget_sats"`
	Status           string         `json:"status"` // pending | approved | rejected | published
	CreatedAt        time.Time      `json:"created_at"`
	Tasks            []Task         `json:"tasks,omitempty"` // suggested tasks (for display; published on approval)
	Metadata         map[string]any `json:"metadata,omitempty"`
}

// ProposalFilter captures list filters for proposals.
type ProposalFilter struct {
	Status     string
	Skills     []string
	MinBudget  int64
	ContractID string
	MaxResults int
	Offset     int
}

// Event is a lightweight activity entry for MCP actions.
type Event struct {
	Type      string    `json:"type"`       // claim | approve | submit | publish
	EntityID  string    `json:"entity_id"`  // task_id, proposal_id, claim_id
	Actor     string    `json:"actor"`      // ai id or system
	Message   string    `json:"message"`    // human-readable summary
	CreatedAt time.Time `json:"created_at"` // timestamp of the event
}
