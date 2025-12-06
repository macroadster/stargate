package mcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PGStore persists MCP state in Postgres.
type PGStore struct {
	pool     *pgxpool.Pool
	claimTTL time.Duration
}

// NewPGStore connects, initializes schema, and optionally seeds fixtures.
func NewPGStore(ctx context.Context, dsn string, claimTTL time.Duration, seed bool) (*PGStore, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}

	s := &PGStore{pool: pool, claimTTL: claimTTL}
	if err := s.initSchema(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	if seed {
		if err := s.seedFixtures(ctx); err != nil {
			pool.Close()
			return nil, err
		}
	}
	return s, nil
}

func (s *PGStore) initSchema(ctx context.Context) error {
	schema := `
CREATE TABLE IF NOT EXISTS mcp_contracts (
  contract_id TEXT PRIMARY KEY,
  title TEXT,
  total_budget_sats BIGINT,
  goals_count INT,
  available_tasks_count INT,
  status TEXT,
  skills TEXT[]
);
CREATE TABLE IF NOT EXISTS mcp_tasks (
  task_id TEXT PRIMARY KEY,
  contract_id TEXT,
  goal_id TEXT,
  title TEXT,
  description TEXT,
  budget_sats BIGINT,
  skills TEXT[],
  status TEXT,
  claimed_by TEXT,
  claimed_at TIMESTAMPTZ,
  claim_expires_at TIMESTAMPTZ,
  difficulty TEXT,
  estimated_hours INT,
  requirements JSONB,
  merkle_proof JSONB
);
CREATE TABLE IF NOT EXISTS mcp_claims (
  claim_id TEXT PRIMARY KEY,
  task_id TEXT,
  ai_identifier TEXT,
  status TEXT,
  expires_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ
);
CREATE TABLE IF NOT EXISTS mcp_submissions (
  submission_id TEXT PRIMARY KEY,
  claim_id TEXT,
  status TEXT,
  deliverables JSONB,
  completion_proof JSONB,
  created_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS mcp_proposals (
  id TEXT PRIMARY KEY,
  title TEXT NOT NULL,
  description_md TEXT NOT NULL,
  visible_pixel_hash TEXT,
  budget_sats BIGINT DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'pending',
  metadata JSONB,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_mcp_proposals_status ON mcp_proposals(status);
CREATE INDEX IF NOT EXISTS idx_mcp_tasks_contract_status ON mcp_tasks(contract_id, status);
`
	_, err := s.pool.Exec(ctx, schema)
	return err
}

func (s *PGStore) seedFixtures(ctx context.Context) error {
	var count int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM mcp_tasks`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	contracts, tasks := SeedData()
	for _, c := range contracts {
		_, err := s.pool.Exec(ctx, `
INSERT INTO mcp_contracts (contract_id, title, total_budget_sats, goals_count, available_tasks_count, status, skills)
VALUES ($1,$2,$3,$4,$5,$6,$7)
ON CONFLICT (contract_id) DO NOTHING
`, c.ContractID, c.Title, c.TotalBudgetSats, c.GoalsCount, c.AvailableTasksCount, c.Status, c.Skills)
		if err != nil {
			return err
		}
	}

	for _, t := range tasks {
		reqJSON, _ := json.Marshal(t.Requirements)
		var proofJSON []byte
		if t.MerkleProof != nil {
			proofJSON, _ = json.Marshal(t.MerkleProof)
		}
		_, err := s.pool.Exec(ctx, `
INSERT INTO mcp_tasks (task_id, contract_id, goal_id, title, description, budget_sats, skills, status, difficulty, estimated_hours, requirements, merkle_proof)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
ON CONFLICT (task_id) DO NOTHING
`, t.TaskID, t.ContractID, t.GoalID, t.Title, t.Description, t.BudgetSats, t.Skills, t.Status, t.Difficulty, t.EstimatedHours, string(reqJSON), string(proofJSON))
		if err != nil {
			return err
		}
	}
	return nil
}

// Close shuts down the pool.
func (s *PGStore) Close() {
	if s.pool != nil {
		s.pool.Close()
	}
}

// ListContracts returns all contracts filtered by status and skill.
func (s *PGStore) ListContracts(status string, skills []string) ([]Contract, error) {
	ctx := context.Background()
	rows, err := s.pool.Query(ctx, `
SELECT contract_id, title, total_budget_sats, goals_count, available_tasks_count, status, skills
FROM mcp_contracts
WHERE ($1 = '' OR status = $1)
`, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Contract
	for rows.Next() {
		var c Contract
		if err := rows.Scan(&c.ContractID, &c.Title, &c.TotalBudgetSats, &c.GoalsCount, &c.AvailableTasksCount, &c.Status, &c.Skills); err != nil {
			return nil, err
		}
		if len(skills) > 0 && !containsSkill(c.Skills, skills) {
			continue
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// hydrateProposalTasks updates proposal tasks with live task statuses from the DB.
func (s *PGStore) hydrateProposalTasks(ctx context.Context, p *Proposal) {
	if p == nil {
		return
	}
	contractIDs := []string{}
	if cid, ok := p.Metadata["contract_id"].(string); ok && strings.TrimSpace(cid) != "" {
		contractIDs = append(contractIDs, cid)
	}
	if strings.TrimSpace(p.ID) != "" {
		contractIDs = append(contractIDs, p.ID)
	}
	if len(contractIDs) == 0 {
		return
	}

	rows, err := s.pool.Query(ctx, `
SELECT task_id, contract_id, goal_id, title, description, budget_sats, skills, status, claimed_by, claimed_at, claim_expires_at, difficulty, estimated_hours, requirements, merkle_proof
FROM mcp_tasks WHERE contract_id = ANY($1)
`, contractIDs)
	if err != nil {
		return
	}
	defer rows.Close()

	liveTasks := make(map[string]Task)
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			continue
		}
		liveTasks[t.TaskID] = t
	}
	if len(liveTasks) == 0 {
		return
	}

	if len(p.Tasks) == 0 {
		for _, t := range liveTasks {
			p.Tasks = append(p.Tasks, t)
		}
		return
	}

	for i, t := range p.Tasks {
		if lt, ok := liveTasks[t.TaskID]; ok {
			p.Tasks[i].Status = lt.Status
			p.Tasks[i].ClaimedBy = lt.ClaimedBy
			p.Tasks[i].ClaimedAt = lt.ClaimedAt
			p.Tasks[i].ClaimExpires = lt.ClaimExpires
			if len(lt.Skills) > 0 {
				p.Tasks[i].Skills = lt.Skills
			}
		}
	}
}

// ListTasks returns tasks filtered by a TaskFilter.
func (s *PGStore) ListTasks(filter TaskFilter) ([]Task, error) {
	ctx := context.Background()
	rows, err := s.pool.Query(ctx, `
SELECT task_id, contract_id, goal_id, title, description, budget_sats, skills, status, claimed_by, claimed_at, claim_expires_at, difficulty, estimated_hours, requirements, merkle_proof
FROM mcp_tasks
WHERE ($1 = '' OR status = $1)
`, filter.Status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Task
	for rows.Next() {
		task, err := scanTask(rows)
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

// GetTask returns a task by ID.
func (s *PGStore) GetTask(id string) (Task, error) {
	ctx := context.Background()
	row := s.pool.QueryRow(ctx, `
SELECT task_id, contract_id, goal_id, title, description, budget_sats, skills, status, claimed_by, claimed_at, claim_expires_at, difficulty, estimated_hours, requirements, merkle_proof
FROM mcp_tasks WHERE task_id=$1
`, id)
	task, err := scanTask(row)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") {
			return Task{}, ErrTaskNotFound
		}
		return Task{}, err
	}
	return task, nil
}

// GetContract returns a contract by ID.
func (s *PGStore) GetContract(id string) (Contract, error) {
	ctx := context.Background()
	var c Contract
	err := s.pool.QueryRow(ctx, `
SELECT contract_id, title, total_budget_sats, goals_count, available_tasks_count, status, skills
FROM mcp_contracts WHERE contract_id=$1
`, id).Scan(&c.ContractID, &c.Title, &c.TotalBudgetSats, &c.GoalsCount, &c.AvailableTasksCount, &c.Status, &c.Skills)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") {
			return Contract{}, fmt.Errorf("contract %s not found", id)
		}
		return Contract{}, err
	}
	return c, nil
}

// ClaimTask reserves a task for an AI. It is idempotent if the same AI reclaims before expiry.
func (s *PGStore) ClaimTask(taskID, aiID string, estimatedCompletion *time.Time) (Claim, error) {
	ctx := context.Background()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Claim{}, err
	}
	defer tx.Rollback(ctx)

	_, err = scanTask(tx.QueryRow(ctx, `
SELECT task_id, contract_id, goal_id, title, description, budget_sats, skills, status, claimed_by, claimed_at, claim_expires_at, difficulty, estimated_hours, requirements, merkle_proof
FROM mcp_tasks WHERE task_id=$1 FOR UPDATE
`, taskID))
	if err != nil {
		return Claim{}, ErrTaskNotFound
	}

	rows, err := tx.Query(ctx, `SELECT claim_id, task_id, ai_identifier, status, expires_at, created_at FROM mcp_claims WHERE task_id=$1`, taskID)
	if err != nil {
		return Claim{}, err
	}
	defer rows.Close()
	now := time.Now()
	for rows.Next() {
		var c Claim
		if err := rows.Scan(&c.ClaimID, &c.TaskID, &c.AiIdentifier, &c.Status, &c.ExpiresAt, &c.CreatedAt); err != nil {
			return Claim{}, err
		}
		if c.Status == "active" && now.Before(c.ExpiresAt) {
			if c.AiIdentifier == aiID {
				return c, tx.Commit(ctx)
			}
			return Claim{}, ErrTaskTaken
		}
	}

	claimID := fmt.Sprintf("CLAIM-%d", time.Now().UnixNano())
	expires := now.Add(s.claimTTL)
	claim := Claim{
		ClaimID:      claimID,
		TaskID:       taskID,
		AiIdentifier: aiID,
		Status:       "active",
		ExpiresAt:    expires,
		CreatedAt:    now,
	}

	if _, err := tx.Exec(ctx, `
INSERT INTO mcp_claims (claim_id, task_id, ai_identifier, status, expires_at, created_at)
VALUES ($1,$2,$3,$4,$5,$6)
`, claim.ClaimID, claim.TaskID, claim.AiIdentifier, claim.Status, claim.ExpiresAt, claim.CreatedAt); err != nil {
		return Claim{}, err
	}

	_, err = tx.Exec(ctx, `
UPDATE mcp_tasks SET status='claimed', claimed_by=$2, claimed_at=$3, claim_expires_at=$4 WHERE task_id=$1
`, taskID, aiID, claim.CreatedAt, claim.ExpiresAt)
	if err != nil {
		return Claim{}, err
	}

	_ = estimatedCompletion // placeholder to persist ETA later
	if err := tx.Commit(ctx); err != nil {
		return Claim{}, err
	}
	return claim, nil
}

// SubmitWork records a submission for a claim.
func (s *PGStore) SubmitWork(claimID string, deliverables map[string]interface{}, proof map[string]interface{}) (Submission, error) {
	ctx := context.Background()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Submission{}, err
	}
	defer tx.Rollback(ctx)

	var claim Claim
	err = tx.QueryRow(ctx, `SELECT claim_id, task_id, ai_identifier, status, expires_at, created_at FROM mcp_claims WHERE claim_id=$1`, claimID).
		Scan(&claim.ClaimID, &claim.TaskID, &claim.AiIdentifier, &claim.Status, &claim.ExpiresAt, &claim.CreatedAt)
	if err != nil {
		return Submission{}, ErrClaimNotFound
	}
	if claim.Status != "active" {
		return Submission{}, fmt.Errorf("claim %s not active", claimID)
	}
	if time.Now().After(claim.ExpiresAt) {
		_, _ = tx.Exec(ctx, `UPDATE mcp_claims SET status='expired' WHERE claim_id=$1`, claimID)
		return Submission{}, fmt.Errorf("claim %s expired", claimID)
	}

	subID := fmt.Sprintf("SUB-%d", time.Now().UnixNano())
	delivJSON, _ := json.Marshal(deliverables)
	proofJSON, _ := json.Marshal(proof)
	sub := Submission{
		SubmissionID:    subID,
		ClaimID:         claimID,
		Status:          "pending_review",
		Deliverables:    deliverables,
		CompletionProof: proof,
		CreatedAt:       time.Now(),
	}

	if _, err := tx.Exec(ctx, `
INSERT INTO mcp_submissions (submission_id, claim_id, status, deliverables, completion_proof, created_at)
VALUES ($1,$2,$3,$4,$5,$6)
`, sub.SubmissionID, sub.ClaimID, sub.Status, delivJSON, proofJSON, sub.CreatedAt); err != nil {
		return Submission{}, err
	}

	if _, err := tx.Exec(ctx, `UPDATE mcp_claims SET status='submitted' WHERE claim_id=$1`, claimID); err != nil {
		return Submission{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE mcp_tasks SET status='submitted' WHERE task_id=$1`, claim.TaskID); err != nil {
		return Submission{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return Submission{}, err
	}
	return sub, nil
}

// TaskStatus returns task status, including claim info if present.
func (s *PGStore) TaskStatus(taskID string) (map[string]interface{}, error) {
	task, err := s.GetTask(taskID)
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	var claim Claim
	err = s.pool.QueryRow(ctx, `
SELECT claim_id, task_id, ai_identifier, status, expires_at, created_at
FROM mcp_claims
WHERE task_id=$1
ORDER BY created_at DESC
LIMIT 1
`, taskID).Scan(&claim.ClaimID, &claim.TaskID, &claim.AiIdentifier, &claim.Status, &claim.ExpiresAt, &claim.CreatedAt)
	if err != nil {
		claim = Claim{}
	}

	resp := map[string]interface{}{
		"task_id":           task.TaskID,
		"status":            task.Status,
		"claimed_by":        task.ClaimedBy,
		"claim_expires_at":  task.ClaimExpires,
		"claimed_at":        task.ClaimedAt,
		"time_remaining_hr": nil,
	}
	if claim.ClaimID != "" {
		remaining := time.Until(claim.ExpiresAt).Hours()
		resp["time_remaining_hr"] = remaining
		resp["claim_id"] = claim.ClaimID
	}
	return resp, nil
}

// GetTaskProof returns the Merkle proof for a task.
func (s *PGStore) GetTaskProof(taskID string) (*MerkleProof, error) {
	task, err := s.GetTask(taskID)
	if err != nil {
		return nil, err
	}
	return task.MerkleProof, nil
}

// ContractFunding returns the contract and any proofs of funding.
func (s *PGStore) ContractFunding(contractID string) (Contract, []MerkleProof, error) {
	contract, err := s.GetContract(contractID)
	if err != nil {
		return Contract{}, nil, err
	}
	ctx := context.Background()
	rows, err := s.pool.Query(ctx, `SELECT merkle_proof FROM mcp_tasks WHERE contract_id=$1`, contractID)
	if err != nil {
		return Contract{}, nil, err
	}
	defer rows.Close()

	var proofs []MerkleProof
	for rows.Next() {
		var proofJSON []byte
		if err := rows.Scan(&proofJSON); err != nil {
			return Contract{}, nil, err
		}
		if len(proofJSON) == 0 {
			continue
		}
		var proof MerkleProof
		if err := json.Unmarshal(proofJSON, &proof); err != nil {
			return Contract{}, nil, err
		}
		proofs = append(proofs, proof)
	}
	return contract, proofs, rows.Err()
}

// UpsertContractWithTasks persists a contract and its tasks idempotently.
func (s *PGStore) UpsertContractWithTasks(ctx context.Context, contract Contract, tasks []Task) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
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
	if err != nil {
		return err
	}

	for _, t := range tasks {
		reqJSON, _ := json.Marshal(t.Requirements)
		var proofJSON []byte
		if t.MerkleProof != nil {
			proofJSON, _ = json.Marshal(t.MerkleProof)
		}
		_, err := tx.Exec(ctx, `
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
  merkle_proof = EXCLUDED.merkle_proof
`, t.TaskID, t.ContractID, t.GoalID, t.Title, t.Description, t.BudgetSats, t.Skills, t.Status, t.ClaimedBy, t.ClaimedAt, t.ClaimExpires, t.Difficulty, t.EstimatedHours, string(reqJSON), string(proofJSON))
		if err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

// UpdateTaskProof replaces the merkle_proof for a task.
func (s *PGStore) UpdateTaskProof(ctx context.Context, taskID string, proof *MerkleProof) error {
	if proof == nil {
		return nil
	}
	b, err := json.Marshal(proof)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `UPDATE mcp_tasks SET merkle_proof=$2 WHERE task_id=$1`, taskID, string(b))
	return err
}

// Proposal operations
func (s *PGStore) CreateProposal(ctx context.Context, p Proposal) error {
	metaMap := p.Metadata
	if metaMap == nil {
		metaMap = map[string]interface{}{}
	}
	if len(p.Tasks) > 0 {
		metaMap["suggested_tasks"] = p.Tasks
	}
	meta, _ := json.Marshal(metaMap)
	_, err := s.pool.Exec(ctx, `
INSERT INTO mcp_proposals (id, title, description_md, visible_pixel_hash, budget_sats, status, metadata, created_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,COALESCE($8, now()))
ON CONFLICT (id) DO NOTHING
`, p.ID, p.Title, p.DescriptionMD, p.VisiblePixelHash, p.BudgetSats, p.Status, string(meta), p.CreatedAt)
	return err
}

func (s *PGStore) ListProposals(ctx context.Context, status string) ([]Proposal, error) {
	query := `SELECT id, title, description_md, visible_pixel_hash, budget_sats, status, metadata, created_at FROM mcp_proposals`
	var args []interface{}
	if status != "" {
		query += " WHERE status=$1"
		args = append(args, status)
	}
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Proposal
	for rows.Next() {
		var p Proposal
		var meta []byte
		if err := rows.Scan(&p.ID, &p.Title, &p.DescriptionMD, &p.VisiblePixelHash, &p.BudgetSats, &p.Status, &meta, &p.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(meta, &p.Metadata)
		populateProposalTasks(&p)
		s.hydrateProposalTasks(ctx, &p)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *PGStore) GetProposal(ctx context.Context, id string) (Proposal, error) {
	var p Proposal
	var meta []byte
	err := s.pool.QueryRow(ctx, `
SELECT id, title, description_md, visible_pixel_hash, budget_sats, status, metadata, created_at
FROM mcp_proposals WHERE id=$1
`, id).Scan(&p.ID, &p.Title, &p.DescriptionMD, &p.VisiblePixelHash, &p.BudgetSats, &p.Status, &meta, &p.CreatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") {
			return Proposal{}, fmt.Errorf("proposal %s not found", id)
		}
		return Proposal{}, err
	}
	_ = json.Unmarshal(meta, &p.Metadata)
	populateProposalTasks(&p)
	s.hydrateProposalTasks(ctx, &p)
	return p, nil
}

func (s *PGStore) ApproveProposal(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `UPDATE mcp_proposals SET status='approved' WHERE id=$1`, id)
	return err
}

// populateProposalTasks hydrates Tasks from metadata suggested_tasks or embedded_message.
func populateProposalTasks(p *Proposal) {
	if p == nil {
		return
	}
	if p.BudgetSats == 0 {
		p.BudgetSats = defaultBudgetSats()
		if p.Metadata == nil {
			p.Metadata = map[string]interface{}{}
		}
		p.Metadata["budget_sats"] = p.BudgetSats
	}
	if p.Metadata == nil {
		p.Metadata = map[string]interface{}{}
	}
	if _, ok := p.Metadata["funding_address"]; !ok {
		p.Metadata["funding_address"] = fundingAddressFromMeta(p.Metadata)
	}
	if tasksRaw, ok := p.Metadata["suggested_tasks"]; ok {
		var tasks []Task
		if b, err := json.Marshal(tasksRaw); err == nil {
			_ = json.Unmarshal(b, &tasks)
		}
		if len(tasks) > 0 {
			p.Tasks = tasks
			return
		}
	}
	if em, ok := p.Metadata["embedded_message"].(string); ok && em != "" && len(p.Tasks) == 0 {
		p.Tasks = BuildTasksFromMarkdown(p.ID, em, p.VisiblePixelHash, p.BudgetSats, fundingAddressFromMeta(p.Metadata))
	}
}

func scanTask(scanner interface {
	Scan(dest ...interface{}) error
}) (Task, error) {
	var t Task
	var reqJSON, proofJSON []byte
	var claimedBy sql.NullString
	var claimedAt, claimExpires sql.NullTime
	if err := scanner.Scan(
		&t.TaskID, &t.ContractID, &t.GoalID, &t.Title, &t.Description, &t.BudgetSats, &t.Skills, &t.Status,
		&claimedBy, &claimedAt, &claimExpires, &t.Difficulty, &t.EstimatedHours, &reqJSON, &proofJSON,
	); err != nil {
		return Task{}, err
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
		var proof MerkleProof
		if err := json.Unmarshal(proofJSON, &proof); err == nil {
			t.MerkleProof = &proof
		}
	}
	return t, nil
}
