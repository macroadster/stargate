package smart_contract

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"stargate-backend/core/smart_contract"
)

// TasksRepository handles task-related database operations
type TasksRepository struct {
	pool *pgxpool.Pool
}

// NewTasksRepository creates a new tasks repository
func NewTasksRepository(pool *pgxpool.Pool) *TasksRepository {
	return &TasksRepository{pool: pool}
}

// List returns tasks filtered by a TaskFilter
func (r *TasksRepository) List(ctx context.Context, filter smart_contract.TaskFilter) ([]smart_contract.Task, error) {
	rows, err := r.pool.Query(ctx, `
SELECT task_id, contract_id, goal_id, title, description, budget_sats, skills, status, claimed_by, claimed_at, claim_expires_at, difficulty, estimated_hours, requirements, merkle_proof
FROM mcp_tasks
WHERE ($1 = '' OR status = $1)
AND ($2 = '' OR contract_id = $2)
AND ($3 = '' OR claimed_by = $3)
`, filter.Status, filter.ContractID, filter.ClaimedBy)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []smart_contract.Task
	for rows.Next() {
		task, err := scanTaskRow(rows)
		if err != nil {
			return nil, err
		}
		if filter.MinBudgetSats > 0 && task.BudgetSats < filter.MinBudgetSats {
			continue
		}
		if len(filter.Skills) > 0 && !containsSkill(task.Skills, filter.Skills) {
			continue
		}
		out = append(out, task)
	}

	if filter.Offset > 0 && filter.Offset < len(out) {
		out = out[filter.Offset:]
	}
	if filter.Limit > 0 && filter.Limit < len(out) {
		out = out[:filter.Limit]
	}
	return out, rows.Err()
}

// Get returns a task by ID
func (r *TasksRepository) Get(ctx context.Context, id string) (smart_contract.Task, error) {
	row := r.pool.QueryRow(ctx, `
SELECT task_id, contract_id, goal_id, title, description, budget_sats, skills, status, claimed_by, claimed_at, claim_expires_at, difficulty, estimated_hours, requirements, merkle_proof
FROM mcp_tasks WHERE task_id=$1
`, id)
	task, err := scanTaskRow(row)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") {
			return smart_contract.Task{}, ErrTaskNotFound
		}
		return smart_contract.Task{}, err
	}
	return task, nil
}

// Upsert persists a single task idempotently
func (r *TasksRepository) Upsert(ctx context.Context, t smart_contract.Task) error {
	reqJSON, _ := json.Marshal(t.Requirements)
	var proofJSON []byte
	if t.MerkleProof != nil {
		proofJSON, _ = json.Marshal(t.MerkleProof)
	}
	_, err := r.pool.Exec(ctx, `
INSERT INTO mcp_tasks (task_id, contract_id, goal_id, title, description, budget_sats, skills, status, claimed_by, claimed_at, claim_expires_at, difficulty, estimated_hours, requirements, merkle_proof)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
ON CONFLICT (task_id) DO UPDATE SET
  contract_id = EXCLUDED.contract_id,
  goal_id = EXCLUDED.goal_id,
  title = EXCLUDED.title,
  description = EXCLUDED.description,
  budget_sats = EXCLUDED.budget_sats,
  skills = EXCLUDED.skills,
  status = EXCLUDED.status,
  claimed_by = COALESCE(EXCLUDED.claimed_by, mcp_tasks.claimed_by),
  claimed_at = COALESCE(EXCLUDED.claimed_at, mcp_tasks.claimed_at),
  claim_expires_at = COALESCE(EXCLUDED.claim_expires_at, mcp_tasks.claim_expires_at),
  difficulty = EXCLUDED.difficulty,
  estimated_hours = EXCLUDED.estimated_hours,
  requirements = EXCLUDED.requirements,
  merkle_proof = COALESCE(EXCLUDED.merkle_proof, mcp_tasks.merkle_proof)
`, t.TaskID, t.ContractID, t.GoalID, t.Title, t.Description, t.BudgetSats, t.Skills, t.Status, t.ClaimedBy, t.ClaimedAt, t.ClaimExpires, t.Difficulty, t.EstimatedHours, string(reqJSON), string(proofJSON))
	return err
}

// UpdateProof replaces the merkle_proof for a task
func (r *TasksRepository) UpdateProof(ctx context.Context, taskID string, proof *smart_contract.MerkleProof) error {
	if proof == nil {
		return nil
	}
	// Preserve existing contractor wallet if new proof doesn't have one
	if existing, err := r.GetProof(ctx, taskID); err == nil && existing != nil {
		if strings.TrimSpace(existing.ContractorWallet) != "" && strings.TrimSpace(proof.ContractorWallet) == "" {
			proof.ContractorWallet = strings.TrimSpace(existing.ContractorWallet)
		}
	}
	b, err := json.Marshal(proof)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `UPDATE mcp_tasks SET merkle_proof=$2 WHERE task_id=$1`, taskID, string(b))
	return err
}

// GetProof returns the Merkle proof for a task
func (r *TasksRepository) GetProof(ctx context.Context, taskID string) (*smart_contract.MerkleProof, error) {
	task, err := r.Get(ctx, taskID)
	if err != nil {
		return nil, err
	}
	return task.MerkleProof, nil
}

// Status returns task status including claim info
func (r *TasksRepository) Status(ctx context.Context, taskID string) (map[string]interface{}, error) {
	task, err := r.Get(ctx, taskID)
	if err != nil {
		return nil, err
	}

	// This would need the claims repository for full status info
	resp := map[string]interface{}{
		"task_id":           task.TaskID,
		"status":            task.Status,
		"claimed_by":        task.ClaimedBy,
		"claim_expires_at":  task.ClaimExpires,
		"claimed_at":        task.ClaimedAt,
		"time_remaining_hr": nil,
	}
	return resp, nil
}

// scanTaskRow scans a task from a database row
func scanTaskRow(scanner interface {
	Scan(dest ...interface{}) error
}) (smart_contract.Task, error) {
	var t smart_contract.Task
	var reqJSON, proofJSON []byte
	var claimedBy sql.NullString
	var claimedAt, claimExpires sql.NullTime
	if err := scanner.Scan(
		&t.TaskID, &t.ContractID, &t.GoalID, &t.Title, &t.Description, &t.BudgetSats, &t.Skills, &t.Status,
		&claimedBy, &claimedAt, &claimExpires, &t.Difficulty, &t.EstimatedHours, &reqJSON, &proofJSON,
	); err != nil {
		return smart_contract.Task{}, err
	}
	if claimedBy.Valid {
		t.ClaimedBy = claimedBy.String
	}
	if claimedAt.Valid {
		c := claimedAt.Time
		t.ClaimedAt = &c
	}
	if claimExpires.Valid {
		e := claimExpires.Time
		t.ClaimExpires = &e
	}
	if len(reqJSON) > 0 {
		_ = json.Unmarshal(reqJSON, &t.Requirements)
	}
	if len(proofJSON) > 0 {
		_ = json.Unmarshal(proofJSON, &t.MerkleProof)
	}
	return t, nil
}
