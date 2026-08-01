package smart_contract

import (
	"encoding/json"
	"strings"
	"time"

	"stargate-backend/core/smart_contract"
)

// GORM row models for simple primary-key reads.
// Skills are always selected via skillsExpr AS skills (comma-separated text).

type gormClaimRow struct {
	ClaimID      string     `gorm:"column:claim_id;primaryKey"`
	TaskID       string     `gorm:"column:task_id"`
	AiIdentifier string     `gorm:"column:ai_identifier"`
	Status       string     `gorm:"column:status"`
	ExpiresAt    *time.Time `gorm:"column:expires_at"`
	CreatedAt    *time.Time `gorm:"column:created_at"`
}

func (gormClaimRow) TableName() string { return TableClaims }

func (r gormClaimRow) toClaim() smart_contract.Claim {
	c := smart_contract.Claim{
		ClaimID:      r.ClaimID,
		TaskID:       r.TaskID,
		AiIdentifier: r.AiIdentifier,
		Status:       r.Status,
	}
	if r.ExpiresAt != nil {
		c.ExpiresAt = *r.ExpiresAt
	}
	if r.CreatedAt != nil {
		c.CreatedAt = *r.CreatedAt
	}
	return c
}

// gormTaskRow is used with explicit Select(taskSelectList()) so skills is text.
type gormTaskRow struct {
	TaskID         string     `gorm:"column:task_id;primaryKey"`
	ContractID     string     `gorm:"column:contract_id"`
	GoalID         string     `gorm:"column:goal_id"`
	Title          string     `gorm:"column:title"`
	Description    string     `gorm:"column:description"`
	BudgetSats     int64      `gorm:"column:budget_sats"`
	Skills         string     `gorm:"column:skills"` // CSV via skillsExpr
	Status         string     `gorm:"column:status"`
	ClaimedBy      *string    `gorm:"column:claimed_by"`
	ClaimedAt      *time.Time `gorm:"column:claimed_at"`
	ClaimExpiresAt *time.Time `gorm:"column:claim_expires_at"`
	Difficulty     string     `gorm:"column:difficulty"`
	EstimatedHours int        `gorm:"column:estimated_hours"`
	Requirements   []byte     `gorm:"column:requirements"`
	MerkleProof    []byte     `gorm:"column:merkle_proof"`
}

func (gormTaskRow) TableName() string { return TableTasks }

func (r gormTaskRow) toTask() smart_contract.Task {
	t := smart_contract.Task{
		TaskID:         r.TaskID,
		ContractID:     r.ContractID,
		GoalID:         r.GoalID,
		Title:          r.Title,
		Description:    r.Description,
		BudgetSats:     r.BudgetSats,
		Skills:         decodeSkillsCSV(r.Skills),
		Status:         r.Status,
		Difficulty:     r.Difficulty,
		EstimatedHours: r.EstimatedHours,
		ClaimedAt:      r.ClaimedAt,
		ClaimExpires:   r.ClaimExpiresAt,
	}
	if r.ClaimedBy != nil {
		t.ClaimedBy = *r.ClaimedBy
	}
	if len(r.Requirements) > 0 {
		_ = json.Unmarshal(r.Requirements, &t.Requirements)
	}
	if len(r.MerkleProof) > 0 {
		var proof smart_contract.MerkleProof
		if err := json.Unmarshal(r.MerkleProof, &proof); err == nil {
			t.MerkleProof = &proof
			if w := strings.TrimSpace(proof.ContractorWallet); w != "" {
				t.ContractorWallet = w
			}
		}
	}
	if strings.TrimSpace(t.ContractorWallet) == "" && strings.TrimSpace(t.ClaimedBy) != "" {
		t.ContractorWallet = strings.TrimSpace(t.ClaimedBy)
		if t.MerkleProof == nil {
			t.MerkleProof = &smart_contract.MerkleProof{}
		}
		if strings.TrimSpace(t.MerkleProof.ContractorWallet) == "" {
			t.MerkleProof.ContractorWallet = t.ContractorWallet
		}
	}
	return t
}
