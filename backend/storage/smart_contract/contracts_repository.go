package smart_contract

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"stargate-backend/core/smart_contract"
)

// ContractsRepository handles contract-related database operations
type ContractsRepository struct {
	pool *pgxpool.Pool
}

// NewContractsRepository creates a new contracts repository
func NewContractsRepository(pool *pgxpool.Pool) *ContractsRepository {
	return &ContractsRepository{pool: pool}
}

// List returns all contracts filtered by status, skill, and metadata fields
func (r *ContractsRepository) List(ctx context.Context, filter smart_contract.ContractFilter) ([]smart_contract.Contract, error) {
	rows, err := r.pool.Query(ctx, `
SELECT c.contract_id, c.title, c.total_budget_sats, c.goals_count,
       COALESCE((SELECT COUNT(*) FROM mcp_tasks t WHERE t.contract_id = c.contract_id AND t.status = 'available'), 0) AS available_tasks_count,
       c.status, c.skills
FROM mcp_contracts c
LEFT JOIN mcp_proposals p ON p.id = c.contract_id
WHERE ($1 = '' OR c.status = $1)
  AND ($2 = '' OR LOWER(COALESCE(p.metadata->>'creator','')) = LOWER($2))
  AND ($3 = '' OR LOWER(COALESCE(p.metadata->>'ai_identifier','')) = LOWER($3))
`, filter.Status, filter.Creator, filter.AiIdentifier)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []smart_contract.Contract
	for rows.Next() {
		var c smart_contract.Contract
		if err := rows.Scan(&c.ContractID, &c.Title, &c.TotalBudgetSats, &c.GoalsCount, &c.AvailableTasksCount, &c.Status, &c.Skills); err != nil {
			return nil, err
		}
		if len(filter.Skills) > 0 && !containsSkill(c.Skills, filter.Skills) {
			continue
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Get returns a contract by ID
func (r *ContractsRepository) Get(ctx context.Context, id string) (smart_contract.Contract, error) {
	var c smart_contract.Contract
	err := r.pool.QueryRow(ctx, `
SELECT contract_id, title, total_budget_sats, goals_count,
       COALESCE((SELECT COUNT(*) FROM mcp_tasks t WHERE t.contract_id = mcp_contracts.contract_id AND t.status = 'available'), 0) AS available_tasks_count,
       status, skills
FROM mcp_contracts WHERE contract_id=$1
`, id).Scan(&c.ContractID, &c.Title, &c.TotalBudgetSats, &c.GoalsCount, &c.AvailableTasksCount, &c.Status, &c.Skills)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") {
			return smart_contract.Contract{}, fmt.Errorf("contract %s not found", id)
		}
		return smart_contract.Contract{}, err
	}
	return c, nil
}

// Upsert persists a contract idempotently
func (r *ContractsRepository) Upsert(ctx context.Context, contract smart_contract.Contract) error {
	_, err := r.pool.Exec(ctx, `
INSERT INTO mcp_contracts (contract_id, title, total_budget_sats, goals_count, available_tasks_count, status, skills)
VALUES ($1,$2,$3,$4,$5,$6,$7)
ON CONFLICT (contract_id) DO UPDATE SET
  title = EXCLUDED.title,
  total_budget_sats = EXCLUDED.total_budget_sats,
  goals_count = EXCLUDED.goals_count,
  available_tasks_count = EXCLUDED.available_tasks_count,
  status = EXCLUDED.status,
  skills = EXCLUDED.skills
`, contract.ContractID, contract.Title, contract.TotalBudgetSats, contract.GoalsCount, contract.AvailableTasksCount, contract.Status, contract.Skills)
	return err
}

// UpdateStatus updates the status for a contract
func (r *ContractsRepository) UpdateStatus(ctx context.Context, contractID, status string) error {
	contractID = strings.TrimSpace(contractID)
	status = strings.TrimSpace(status)
	if contractID == "" || status == "" {
		return nil
	}
	_, err := r.pool.Exec(ctx, `UPDATE mcp_contracts SET status=$2 WHERE contract_id=$1`, contractID, status)
	if err != nil {
		return err
	}
	if strings.EqualFold(status, "confirmed") {
		normalized := NormalizeContractID(contractID)
		wishID := "wish-" + normalized
		_, err = r.pool.Exec(ctx, `
UPDATE mcp_proposals SET status='confirmed'
WHERE status='approved' AND (
  metadata->>'contract_id' IN ($1, $2) OR
  metadata->>'ingestion_id' IN ($1, $2) OR
  metadata->>'visible_pixel_hash' IN ($1, $2) OR
  id IN ($1, $2)
)`, normalized, wishID)
	}
	return err
}

// Funding returns the contract and proofs of funding
func (r *ContractsRepository) Funding(ctx context.Context, contractID string) (smart_contract.Contract, []smart_contract.MerkleProof, error) {
	contract, err := r.Get(ctx, contractID)
	if err != nil {
		return smart_contract.Contract{}, nil, err
	}
	rows, err := r.pool.Query(ctx, `SELECT merkle_proof FROM mcp_tasks WHERE contract_id=$1`, contractID)
	if err != nil {
		return smart_contract.Contract{}, nil, err
	}
	defer rows.Close()

	var proofs []smart_contract.MerkleProof
	for rows.Next() {
		var proofJSON []byte
		if err := rows.Scan(&proofJSON); err != nil {
			return smart_contract.Contract{}, nil, err
		}
		if len(proofJSON) == 0 {
			continue
		}
		var proof smart_contract.MerkleProof
		if err := json.Unmarshal(proofJSON, &proof); err != nil {
			return smart_contract.Contract{}, nil, err
		}
		proofs = append(proofs, proof)
	}
	return contract, proofs, rows.Err()
}
