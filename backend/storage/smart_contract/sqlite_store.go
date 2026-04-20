package smart_contract

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"stargate-backend/core/smart_contract"
)

type SQLiteStore struct {
	db       *sql.DB
	claimTTL time.Duration
}

func NewSQLiteStore(dbPath string, claimTTL time.Duration, seed bool) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("open sqlite3: %w", err)
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(1 * time.Hour)

	s := &SQLiteStore{db: db, claimTTL: claimTTL}
	if err := s.initSchema(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	if seed {
		if err := s.seedFixtures(context.Background()); err != nil {
			log.Printf("seed fixtures warning: %v", err)
		}
	}
	return s, nil
}

func (s *SQLiteStore) initSchema(ctx context.Context) error {
	schema := `
CREATE TABLE IF NOT EXISTS mcp_contracts (
  contract_id TEXT PRIMARY KEY,
  title TEXT,
  total_budget_sats INTEGER,
  goals_count INTEGER,
  available_tasks_count INTEGER,
  status TEXT,
  skills TEXT,
  stego_image_url TEXT,
  confirmed_block_height INTEGER,
  confirmed_at TEXT,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  metadata TEXT DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS idx_mcp_contracts_confirmed_height ON mcp_contracts(confirmed_block_height DESC);
CREATE INDEX IF NOT EXISTS idx_mcp_contracts_confirmed_at ON mcp_contracts(confirmed_at DESC);
CREATE INDEX IF NOT EXISTS idx_mcp_contracts_created_at ON mcp_contracts(created_at DESC);

CREATE TABLE IF NOT EXISTS mcp_tasks (
  task_id TEXT PRIMARY KEY,
  contract_id TEXT,
  goal_id TEXT,
  title TEXT,
  description TEXT,
  budget_sats INTEGER,
  skills TEXT,
  status TEXT,
  claimed_by TEXT,
  claimed_at TEXT,
  claim_expires_at TEXT,
  difficulty TEXT,
  estimated_hours INTEGER,
  requirements TEXT,
  merkle_proof TEXT,
  FOREIGN KEY (contract_id) REFERENCES mcp_contracts(contract_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS mcp_claims (
  claim_id TEXT PRIMARY KEY,
  task_id TEXT,
  ai_identifier TEXT,
  status TEXT,
  expires_at TEXT,
  created_at TEXT,
  FOREIGN KEY (task_id) REFERENCES mcp_tasks(task_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS mcp_submissions (
  submission_id TEXT PRIMARY KEY,
  claim_id TEXT,
  task_id TEXT,
  status TEXT NOT NULL,
  deliverables TEXT,
  completion_proof TEXT,
  rejection_reason TEXT,
  rejection_type TEXT,
  rejected_at TEXT,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  FOREIGN KEY (claim_id) REFERENCES mcp_claims(claim_id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_mcp_submissions_claim_id ON mcp_submissions(claim_id);
CREATE INDEX IF NOT EXISTS idx_mcp_submissions_task_id ON mcp_submissions(task_id);
CREATE INDEX IF NOT EXISTS idx_mcp_submissions_status ON mcp_submissions(status);
CREATE INDEX IF NOT EXISTS idx_mcp_submissions_created_at ON mcp_submissions(created_at DESC);

CREATE TABLE IF NOT EXISTS mcp_proposals (
  id TEXT PRIMARY KEY,
  title TEXT NOT NULL,
  description_md TEXT NOT NULL,
  visible_pixel_hash TEXT,
  budget_sats INTEGER DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'pending',
  metadata TEXT,
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_mcp_proposals_status ON mcp_proposals(status);
CREATE INDEX IF NOT EXISTS idx_mcp_tasks_contract_status ON mcp_tasks(contract_id, status);

CREATE TABLE IF NOT EXISTS mcp_escort_status (
  task_id TEXT PRIMARY KEY,
  proof_status TEXT,
  last_checked TEXT,
  payload TEXT
);
`
	_, err := s.db.ExecContext(ctx, schema)
	return err
}

func (s *SQLiteStore) seedFixtures(ctx context.Context) error {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM mcp_tasks`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	contracts, tasks := SeedData()
	for _, c := range contracts {
		metadata, _ := json.Marshal(c.Metadata)
		skills := strings.Join(c.Skills, ",")
		_, err := s.db.ExecContext(ctx, `
INSERT OR IGNORE INTO mcp_contracts (contract_id, title, total_budget_sats, goals_count, available_tasks_count, status, skills, stego_image_url, metadata)
VALUES (?,?,?,?,?,?,?,?,?)
`, c.ContractID, c.Title, c.TotalBudgetSats, c.GoalsCount, c.AvailableTasksCount, c.Status, skills, c.StegoImageURL, string(metadata))
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
		taskSkills := strings.Join(t.Skills, ",")
		_, err := s.db.ExecContext(ctx, `
INSERT OR IGNORE INTO mcp_tasks (task_id, contract_id, goal_id, title, description, budget_sats, skills, status, difficulty, estimated_hours, requirements, merkle_proof)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?)
`, t.TaskID, t.ContractID, t.GoalID, t.Title, t.Description, t.BudgetSats, taskSkills, t.Status, t.Difficulty, t.EstimatedHours, string(reqJSON), string(proofJSON))
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteStore) Close() {
	if s.db != nil {
		s.db.Close()
	}
}

func (s *SQLiteStore) containsSkill(all []string, skills []string) bool {
	for _, want := range skills {
		for _, have := range all {
			if strings.EqualFold(have, want) {
				return true
			}
		}
	}
	return len(skills) == 0
}

func (s *SQLiteStore) ListContracts(filter smart_contract.ContractFilter) ([]smart_contract.Contract, error) {
	baseSelect := `
SELECT c.contract_id, c.title, c.total_budget_sats, c.goals_count,
	(SELECT COUNT(*) FROM mcp_tasks t WHERE t.contract_id = c.contract_id AND t.status = 'available') AS available_tasks_count,
	c.status, c.skills, c.stego_image_url, c.metadata, c.confirmed_block_height, c.confirmed_at, c.created_at
FROM mcp_contracts c
`

	whereConditions := []string{}
	args := []interface{}{}

	if filter.Status != "" {
		whereConditions = append(whereConditions, "c.status = ?")
		args = append(args, filter.Status)
	}

	if filter.CursorHeight != nil && *filter.CursorHeight > 0 {
		whereConditions = append(whereConditions, "c.confirmed_block_height < ?")
		args = append(args, *filter.CursorHeight)
	}

	whereClause := ""
	if len(whereConditions) > 0 {
		whereClause = "WHERE " + strings.Join(whereConditions, " AND ")
	}

	orderBy := "ORDER BY c.confirmed_block_height DESC NULLS LAST, c.created_at DESC, c.contract_id DESC"
	if filter.Limit > 0 {
		orderBy += fmt.Sprintf(" LIMIT %d", filter.Limit)
	}

	query := baseSelect + " " + whereClause + " " + orderBy

	rows, err := s.db.QueryContext(context.Background(), query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var allContracts []smart_contract.Contract
	for rows.Next() {
		var c smart_contract.Contract
		var metadata, skillsStr []byte
		if err := rows.Scan(&c.ContractID, &c.Title, &c.TotalBudgetSats, &c.GoalsCount, &c.AvailableTasksCount,
			&c.Status, &skillsStr, &c.StegoImageURL, &metadata, &c.ConfirmedBlockHeight, &c.ConfirmedAt, &c.CreatedAt); err != nil {
			return nil, err
		}
		if len(metadata) > 0 {
			_ = json.Unmarshal(metadata, &c.Metadata)
		}
		if len(skillsStr) > 0 {
			c.Skills = strings.Split(string(skillsStr), ",")
		}
		if len(filter.Skills) > 0 && !s.containsSkill(c.Skills, filter.Skills) {
			continue
		}
		allContracts = append(allContracts, c)
	}

	if filter.Offset > 0 && filter.Offset < len(allContracts) {
		allContracts = allContracts[filter.Offset:]
	}

	return allContracts, nil
}

func (s *SQLiteStore) ListTasks(filter smart_contract.TaskFilter) ([]smart_contract.Task, error) {
	query := `
SELECT task_id, contract_id, goal_id, title, description, budget_sats, skills, status, claimed_by, claimed_at, claim_expires_at, difficulty, estimated_hours, requirements, merkle_proof
FROM mcp_tasks
WHERE (? = '' OR status = ?)
AND (? = '' OR contract_id = ?)
AND (? = '' OR claimed_by = ?)
`
	args := []interface{}{filter.Status, filter.Status, filter.ContractID, filter.ContractID, filter.ClaimedBy, filter.ClaimedBy}

	rows, err := s.db.QueryContext(context.Background(), query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []smart_contract.Task
	for rows.Next() {
		task, err := scanTaskSQLite(rows)
		if err != nil {
			return nil, err
		}
		if filter.MinBudgetSats > 0 && task.BudgetSats < filter.MinBudgetSats {
			continue
		}
		if len(filter.Skills) > 0 && !s.containsSkill(task.Skills, filter.Skills) {
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

func scanTaskSQLite(rows *sql.Rows) (smart_contract.Task, error) {
	var t smart_contract.Task
	var skillsStr, requirementsStr, merkleProofStr []byte
	err := rows.Scan(&t.TaskID, &t.ContractID, &t.GoalID, &t.Title, &t.Description, &t.BudgetSats,
		&skillsStr, &t.Status, &t.ClaimedBy, &t.ClaimedAt, &t.ClaimExpires, &t.Difficulty,
		&t.EstimatedHours, &requirementsStr, &merkleProofStr)
	if err != nil {
		return t, err
	}
	if len(skillsStr) > 0 {
		t.Skills = strings.Split(string(skillsStr), ",")
	}
	if len(requirementsStr) > 0 {
		_ = json.Unmarshal(requirementsStr, &t.Requirements)
	}
	if len(merkleProofStr) > 0 {
		_ = json.Unmarshal(merkleProofStr, &t.MerkleProof)
	}
	return t, nil
}

func (s *SQLiteStore) GetTask(id string) (smart_contract.Task, error) {
	row := s.db.QueryRowContext(context.Background(), `
SELECT task_id, contract_id, goal_id, title, description, budget_sats, skills, status, claimed_by, claimed_at, claim_expires_at, difficulty, estimated_hours, requirements, merkle_proof
FROM mcp_tasks WHERE task_id=?
`, id)
	var t smart_contract.Task
	var skillsStr, requirementsStr, merkleProofStr []byte
	err := row.Scan(&t.TaskID, &t.ContractID, &t.GoalID, &t.Title, &t.Description, &t.BudgetSats,
		&skillsStr, &t.Status, &t.ClaimedBy, &t.ClaimedAt, &t.ClaimExpires, &t.Difficulty,
		&t.EstimatedHours, &requirementsStr, &merkleProofStr)
	if err != nil {
		return t, ErrTaskNotFound
	}
	if len(skillsStr) > 0 {
		t.Skills = strings.Split(string(skillsStr), ",")
	}
	if len(requirementsStr) > 0 {
		_ = json.Unmarshal(requirementsStr, &t.Requirements)
	}
	if len(merkleProofStr) > 0 {
		_ = json.Unmarshal(merkleProofStr, &t.MerkleProof)
	}
	return t, nil
}

func (s *SQLiteStore) GetContract(id string) (smart_contract.Contract, error) {
	var c smart_contract.Contract
	var metadata, skillsStr []byte
	err := s.db.QueryRowContext(context.Background(), `
SELECT contract_id, title, total_budget_sats, goals_count,
       (SELECT COUNT(*) FROM mcp_tasks t WHERE t.contract_id = mcp_contracts.contract_id AND t.status = 'available') AS available_tasks_count,
       status, skills, stego_image_url, confirmed_block_height, confirmed_at, metadata
FROM mcp_contracts WHERE contract_id=?
`, id).Scan(&c.ContractID, &c.Title, &c.TotalBudgetSats, &c.GoalsCount, &c.AvailableTasksCount,
		&c.Status, &skillsStr, &c.StegoImageURL, &c.ConfirmedBlockHeight, &c.ConfirmedAt, &metadata)
	if err != nil {
		return c, fmt.Errorf("contract %s not found", id)
	}
	if len(metadata) > 0 {
		_ = json.Unmarshal(metadata, &c.Metadata)
	}
	if len(skillsStr) > 0 {
		c.Skills = strings.Split(string(skillsStr), ",")
	}
	return c, nil
}

func (s *SQLiteStore) GetClaim(id string) (smart_contract.Claim, error) {
	var c smart_contract.Claim
	var createdAt, expiresAt sql.NullString
	err := s.db.QueryRowContext(context.Background(), `
SELECT claim_id, task_id, ai_identifier, status, expires_at, created_at
FROM mcp_claims WHERE claim_id=?
`, id).Scan(&c.ClaimID, &c.TaskID, &c.AiIdentifier, &c.Status, &expiresAt, &createdAt)
	if err != nil {
		return c, ErrClaimNotFound
	}
	if expiresAt.Valid {
		if t, err := time.Parse(time.RFC3339, expiresAt.String); err == nil {
			c.ExpiresAt = t
		}
	}
	if createdAt.Valid {
		if t, err := time.Parse(time.RFC3339, createdAt.String); err == nil {
			c.CreatedAt = t
		}
	}
	return c, nil
}

func (s *SQLiteStore) ClaimTask(taskID, walletAddress string, estimatedCompletion *time.Time) (smart_contract.Claim, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return smart_contract.Claim{}, err
	}
	defer tx.Rollback()

	var taskStatus string
	err = tx.QueryRow(`SELECT status FROM mcp_tasks WHERE task_id=?`, taskID).Scan(&taskStatus)
	if err != nil {
		return smart_contract.Claim{}, ErrTaskNotFound
	}
	if taskStatus != "available" && taskStatus != "approved" {
		return smart_contract.Claim{}, ErrTaskUnavailable
	}

	normalizedWallet := strings.TrimSpace(walletAddress)
	if normalizedWallet == "" {
		return smart_contract.Claim{}, fmt.Errorf("wallet address required")
	}

	now := time.Now()
	expires := now.Add(s.claimTTL)

	claimID := fmt.Sprintf("CLAIM-%d", time.Now().UnixNano())
	claim := smart_contract.Claim{
		ClaimID:      claimID,
		TaskID:       taskID,
		AiIdentifier: walletAddress,
		Status:       "active",
		ExpiresAt:    expires,
		CreatedAt:    now,
	}

	_, err = tx.Exec(`
INSERT INTO mcp_claims (claim_id, task_id, ai_identifier, status, expires_at, created_at)
VALUES (?,?,?,?,?,?)
`, claim.ClaimID, claim.TaskID, claim.AiIdentifier, claim.Status, claim.ExpiresAt.Format(time.RFC3339), claim.CreatedAt.Format(time.RFC3339))
	if err != nil {
		return smart_contract.Claim{}, err
	}

	_, err = tx.Exec(`
UPDATE mcp_tasks SET status='claimed', claimed_by=?, claimed_at=?, claim_expires_at=? WHERE task_id=?
`, claim.AiIdentifier, claim.CreatedAt.Format(time.RFC3339), claim.ExpiresAt.Format(time.RFC3339), taskID)
	if err != nil {
		return smart_contract.Claim{}, err
	}

	if err := tx.Commit(); err != nil {
		return smart_contract.Claim{}, err
	}
	return claim, nil
}

func (s *SQLiteStore) SubmitWork(claimID string, deliverables map[string]interface{}, proof map[string]interface{}) (smart_contract.Submission, error) {
	var claim smart_contract.Claim
	var expiresAt sql.NullString
	err := s.db.QueryRowContext(context.Background(), `SELECT claim_id, task_id, ai_identifier, status, expires_at, created_at FROM mcp_claims WHERE claim_id=?`, claimID).
		Scan(&claim.ClaimID, &claim.TaskID, &claim.AiIdentifier, &claim.Status, &expiresAt, &claim.CreatedAt)
	if err != nil {
		return smart_contract.Submission{}, ErrClaimNotFound
	}
	if claim.Status != "active" && claim.Status != "submitted" {
		return smart_contract.Submission{}, fmt.Errorf("claim %s not active or submitted", claimID)
	}
	if expiresAt.Valid {
		if t, err := time.Parse(time.RFC3339, expiresAt.String); err == nil {
			if time.Now().After(t) {
				return smart_contract.Submission{}, fmt.Errorf("claim %s expired", claimID)
			}
		}
	}

	delete(deliverables, "status")
	if proof != nil {
		delete(proof, "status")
	}

	subID := fmt.Sprintf("SUB-%d", time.Now().UnixNano())

	delivJSON, _ := json.Marshal(deliverables)
	proofJSON, _ := json.Marshal(proof)
	sub := smart_contract.Submission{
		SubmissionID:    subID,
		ClaimID:         claimID,
		TaskID:          claim.TaskID,
		Status:          "pending_review",
		Deliverables:    deliverables,
		CompletionProof: proof,
		CreatedAt:       time.Now(),
	}

	_, err = s.db.Exec(`
INSERT INTO mcp_submissions (submission_id, claim_id, task_id, status, deliverables, completion_proof, created_at)
VALUES (?,?,?,?,?,?,?)
`, sub.SubmissionID, sub.ClaimID, sub.TaskID, sub.Status, string(delivJSON), string(proofJSON), sub.CreatedAt.Format(time.RFC3339))
	if err != nil {
		return smart_contract.Submission{}, err
	}

	_, _ = s.db.Exec(`UPDATE mcp_claims SET status='submitted' WHERE claim_id=?`, claimID)
	_, _ = s.db.Exec(`UPDATE mcp_tasks SET status='submitted' WHERE task_id=?`, claim.TaskID)

	return sub, nil
}

func (s *SQLiteStore) ListSubmissions(ctx context.Context, taskIDs []string) ([]smart_contract.Submission, error) {
	if len(taskIDs) == 0 {
		return nil, nil
	}
	placeholders := strings.Repeat("?,", len(taskIDs))
	placeholders = placeholders[:len(placeholders)-1]
	query := fmt.Sprintf(`
SELECT s.submission_id, s.claim_id, c.task_id, s.status, s.deliverables, s.completion_proof, s.rejection_reason, s.rejection_type, s.rejected_at, s.created_at
FROM mcp_submissions s
JOIN mcp_claims c ON c.claim_id = s.claim_id
WHERE c.task_id IN (%s)
ORDER BY s.created_at DESC
`, placeholders)

	args := make([]interface{}, len(taskIDs))
	for i, id := range taskIDs {
		args[i] = id
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []smart_contract.Submission
	for rows.Next() {
		var sub smart_contract.Submission
		var delivJSON, proofJSON, rejectionReason, rejectionType, rejectedAt []byte
		if err := rows.Scan(&sub.SubmissionID, &sub.ClaimID, &sub.TaskID, &sub.Status, &delivJSON, &proofJSON, &rejectionReason, &rejectionType, &rejectedAt, &sub.CreatedAt); err != nil {
			return nil, err
		}
		if len(delivJSON) > 0 {
			_ = json.Unmarshal(delivJSON, &sub.Deliverables)
		}
		if len(proofJSON) > 0 {
			_ = json.Unmarshal(proofJSON, &sub.CompletionProof)
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) GetSubmission(ctx context.Context, id string) (smart_contract.Submission, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT s.submission_id, s.claim_id, c.task_id, s.status, s.deliverables, s.completion_proof, s.rejection_reason, s.rejection_type, s.rejected_at, s.created_at
FROM mcp_submissions s
JOIN mcp_claims c ON c.claim_id = s.claim_id
WHERE s.submission_id = ?
`, id)
	if err != nil {
		return smart_contract.Submission{}, err
	}
	defer rows.Close()
	if rows.Next() {
		var sub smart_contract.Submission
		var delivJSON, proofJSON []byte
		if err := rows.Scan(&sub.SubmissionID, &sub.ClaimID, &sub.TaskID, &sub.Status, &delivJSON, &proofJSON, &sub.RejectionReason, &sub.RejectionType, &sub.RejectedAt, &sub.CreatedAt); err != nil {
			return smart_contract.Submission{}, err
		}
		if len(delivJSON) > 0 {
			_ = json.Unmarshal(delivJSON, &sub.Deliverables)
		}
		if len(proofJSON) > 0 {
			_ = json.Unmarshal(proofJSON, &sub.CompletionProof)
		}
		return sub, nil
	}
	return smart_contract.Submission{}, fmt.Errorf("submission %s not found", id)
}

func (s *SQLiteStore) TaskStatus(taskID string) (map[string]interface{}, error) {
	task, err := s.GetTask(taskID)
	if err != nil {
		return nil, err
	}

	var claim smart_contract.Claim
	err = s.db.QueryRowContext(context.Background(), `
SELECT claim_id, task_id, ai_identifier, status, expires_at, created_at
FROM mcp_claims
WHERE task_id=? AND status IN ('active','submitted','pending_review')
ORDER BY created_at DESC
LIMIT 1
`, taskID).Scan(&claim.ClaimID, &claim.TaskID, &claim.AiIdentifier, &claim.Status, &claim.ExpiresAt, &claim.CreatedAt)
	if err != nil {
		claim = smart_contract.Claim{}
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

func (s *SQLiteStore) GetTaskProof(taskID string) (*smart_contract.MerkleProof, error) {
	task, err := s.GetTask(taskID)
	if err != nil {
		return nil, err
	}
	return task.MerkleProof, nil
}

func (s *SQLiteStore) ContractFunding(contractID string) (smart_contract.Contract, []smart_contract.MerkleProof, error) {
	contract, err := s.GetContract(contractID)
	if err != nil {
		return smart_contract.Contract{}, nil, err
	}
	rows, err := s.db.QueryContext(context.Background(), `SELECT merkle_proof FROM mcp_tasks WHERE contract_id=?`, contractID)
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

func (s *SQLiteStore) UpsertContractWithTasks(ctx context.Context, contract smart_contract.Contract, tasks []smart_contract.Task) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	metadata, _ := json.Marshal(contract.Metadata)
	skills := strings.Join(contract.Skills, ",")
	createdAt := time.Now().Format(time.RFC3339)
	if !contract.CreatedAt.IsZero() {
		createdAt = contract.CreatedAt.Format(time.RFC3339)
	}

	_, err = tx.ExecContext(ctx, `
INSERT INTO mcp_contracts (contract_id, title, total_budget_sats, goals_count, available_tasks_count, status, skills, stego_image_url, created_at, metadata)
VALUES (?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(contract_id) DO UPDATE SET
  title = excluded.title,
  total_budget_sats = excluded.total_budget_sats,
  goals_count = excluded.goals_count,
  available_tasks_count = excluded.available_tasks_count,
  status = excluded.status,
  skills = excluded.skills,
  stego_image_url = excluded.stego_image_url
`, contract.ContractID, contract.Title, contract.TotalBudgetSats, contract.GoalsCount, contract.AvailableTasksCount, contract.Status, skills, contract.StegoImageURL, createdAt, string(metadata))
	if err != nil {
		return err
	}

	for _, t := range tasks {
		reqJSON, _ := json.Marshal(t.Requirements)
		var proofJSON []byte
		if t.MerkleProof != nil {
			proofJSON, _ = json.Marshal(t.MerkleProof)
		}
		taskSkills := strings.Join(t.Skills, ",")
		_, err := tx.ExecContext(ctx, `
INSERT INTO mcp_tasks (task_id, contract_id, goal_id, title, description, budget_sats, skills, status, claimed_by, claimed_at, claim_expires_at, difficulty, estimated_hours, requirements, merkle_proof)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(task_id) DO UPDATE SET
  title = excluded.title,
  description = excluded.description,
  budget_sats = excluded.budget_sats,
  skills = excluded.skills,
  status = excluded.status,
  difficulty = excluded.difficulty,
  estimated_hours = excluded.estimated_hours,
  requirements = excluded.requirements
`, t.TaskID, t.ContractID, t.GoalID, t.Title, t.Description, t.BudgetSats, taskSkills, t.Status, t.ClaimedBy, t.ClaimedAt, t.ClaimExpires, t.Difficulty, t.EstimatedHours, string(reqJSON), string(proofJSON))
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *SQLiteStore) UpdateTaskProof(ctx context.Context, taskID string, proof *smart_contract.MerkleProof) error {
	if proof == nil {
		return nil
	}
	b, err := json.Marshal(proof)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE mcp_tasks SET merkle_proof=? WHERE task_id=?`, string(b), taskID)
	return err
}

func (s *SQLiteStore) UpdateContractStatus(ctx context.Context, contractID, status string) error {
	contractID = strings.TrimSpace(contractID)
	status = strings.TrimSpace(status)
	if contractID == "" || status == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `UPDATE mcp_contracts SET status=? WHERE contract_id=?`, status, contractID)
	return err
}

func (s *SQLiteStore) ConfirmContract(ctx context.Context, contractID string, blockHeight int, txid string) error {
	contractID = strings.TrimSpace(contractID)
	if contractID == "" {
		return nil
	}

	stegoImageURL := fmt.Sprintf("/api/block-image/%d/%s", blockHeight, contractID)

	_, err := s.db.ExecContext(ctx, `
UPDATE mcp_contracts 
SET status='confirmed', confirmed_block_height=?, confirmed_at=datetime('now'), 
    stego_image_url=COALESCE(?, stego_image_url)
WHERE contract_id=?
`, blockHeight, stegoImageURL, contractID)
	return err
}

func (s *SQLiteStore) SyncClaim(ctx context.Context, claim smart_contract.Claim) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO mcp_claims (claim_id, task_id, ai_identifier, status, expires_at, created_at)
VALUES (?,?,?,?,?,?)
ON CONFLICT(claim_id) DO UPDATE SET
  status = excluded.status,
  expires_at = excluded.expires_at
`, claim.ClaimID, claim.TaskID, claim.AiIdentifier, claim.Status, claim.ExpiresAt.Format(time.RFC3339), claim.CreatedAt.Format(time.RFC3339))
	return err
}

func (s *SQLiteStore) SyncSubmission(ctx context.Context, sub smart_contract.Submission) error {
	if sub.Deliverables != nil {
		delete(sub.Deliverables, "status")
	}
	if sub.CompletionProof != nil {
		delete(sub.CompletionProof, "status")
	}

	delivJSON, _ := json.Marshal(sub.Deliverables)
	proofJSON, _ := json.Marshal(sub.CompletionProof)
	_, err := s.db.ExecContext(ctx, `
INSERT INTO mcp_submissions (submission_id, claim_id, task_id, status, deliverables, completion_proof, rejection_reason, rejection_type, rejected_at, created_at)
VALUES (?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(submission_id) DO UPDATE SET
  status = excluded.status,
  deliverables = excluded.deliverables,
  completion_proof = excluded.completion_proof,
  task_id = excluded.task_id
`, sub.SubmissionID, sub.ClaimID, sub.TaskID, sub.Status, string(delivJSON), string(proofJSON), sub.RejectionReason, sub.RejectionType, sub.RejectedAt, sub.CreatedAt.Format(time.RFC3339))
	return err
}

func (s *SQLiteStore) UpsertTask(ctx context.Context, t smart_contract.Task) error {
	reqJSON, _ := json.Marshal(t.Requirements)
	var proofJSON []byte
	if t.MerkleProof != nil {
		proofJSON, _ = json.Marshal(t.MerkleProof)
	}
	taskSkills := strings.Join(t.Skills, ",")
	_, err := s.db.ExecContext(ctx, `
INSERT INTO mcp_tasks (task_id, contract_id, goal_id, title, description, budget_sats, skills, status, claimed_by, claimed_at, claim_expires_at, difficulty, estimated_hours, requirements, merkle_proof)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(task_id) DO UPDATE SET
  status = excluded.status,
  merkle_proof = COALESCE(excluded.merkle_proof, mcp_tasks.merkle_proof)
`, t.TaskID, t.ContractID, t.GoalID, t.Title, t.Description, t.BudgetSats, taskSkills, t.Status, t.ClaimedBy, t.ClaimedAt, t.ClaimExpires, t.Difficulty, t.EstimatedHours, string(reqJSON), string(proofJSON))
	return err
}

func (s *SQLiteStore) SyncEscortStatus(ctx context.Context, status smart_contract.EscortStatus) error {
	payload, _ := json.Marshal(status)
	_, err := s.db.ExecContext(ctx, `
INSERT INTO mcp_escort_status (task_id, proof_status, last_checked, payload)
VALUES (?,?,?,?)
ON CONFLICT(task_id) DO UPDATE SET
  proof_status = excluded.proof_status,
  last_checked = excluded.last_checked,
  payload = excluded.payload
`, status.TaskID, status.ProofStatus, status.LastChecked.Format(time.RFC3339), string(payload))
	return err
}

func (s *SQLiteStore) CreateProposal(ctx context.Context, p smart_contract.Proposal) error {
	if p.Metadata == nil {
		p.Metadata = map[string]interface{}{}
	}
	if strings.TrimSpace(p.VisiblePixelHash) != "" {
		if vph, ok := p.Metadata["visible_pixel_hash"].(string); !ok || strings.TrimSpace(vph) == "" {
			p.Metadata["visible_pixel_hash"] = p.VisiblePixelHash
		}
	}

	if err := ValidateProposalInput(&p); err != nil {
		return fmt.Errorf("proposal validation failed: %v", err)
	}

	if p.Status == "" {
		p.Status = "pending"
	}

	metadata, _ := json.Marshal(p.Metadata)
	_, err := s.db.ExecContext(ctx, `
INSERT INTO mcp_proposals (id, title, description_md, visible_pixel_hash, budget_sats, status, metadata, created_at)
VALUES (?,?,?,?,?,?,?,?)
`, p.ID, p.Title, p.DescriptionMD, p.VisiblePixelHash, p.BudgetSats, p.Status, string(metadata), p.CreatedAt.Format(time.RFC3339))
	return err
}

func (s *SQLiteStore) ListProposals(ctx context.Context, filter smart_contract.ProposalFilter) ([]smart_contract.Proposal, error) {
	query := `SELECT id, title, description_md, visible_pixel_hash, budget_sats, status, metadata, created_at FROM mcp_proposals WHERE 1=1`
	args := []interface{}{}

	if filter.ProposalID != "" {
		query += " AND id = ?"
		args = append(args, filter.ProposalID)
	}
	if filter.Status != "" {
		query += " AND status = ?"
		args = append(args, filter.Status)
	}

	query += " ORDER BY created_at DESC"

	if filter.MaxResults > 0 {
		query += fmt.Sprintf(" LIMIT %d", filter.MaxResults)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []smart_contract.Proposal
	for rows.Next() {
		var p smart_contract.Proposal
		var metadata []byte
		if err := rows.Scan(&p.ID, &p.Title, &p.DescriptionMD, &p.VisiblePixelHash, &p.BudgetSats, &p.Status, &metadata, &p.CreatedAt); err != nil {
			return nil, err
		}
		if len(metadata) > 0 {
			_ = json.Unmarshal(metadata, &p.Metadata)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) GetProposal(ctx context.Context, id string) (smart_contract.Proposal, error) {
	var p smart_contract.Proposal
	var metadata []byte
	err := s.db.QueryRowContext(ctx, `
SELECT id, title, description_md, visible_pixel_hash, budget_sats, status, metadata, created_at
FROM mcp_proposals WHERE id=?
`, id).Scan(&p.ID, &p.Title, &p.DescriptionMD, &p.VisiblePixelHash, &p.BudgetSats, &p.Status, &metadata, &p.CreatedAt)
	if err != nil {
		return smart_contract.Proposal{}, fmt.Errorf("proposal %s not found", id)
	}
	if len(metadata) > 0 {
		_ = json.Unmarshal(metadata, &p.Metadata)
	}
	return p, nil
}

func (s *SQLiteStore) UpdateProposal(ctx context.Context, p smart_contract.Proposal) error {
	existing, err := s.GetProposal(ctx, p.ID)
	if err != nil {
		return fmt.Errorf("proposal %s not found", p.ID)
	}
	if !strings.EqualFold(existing.Status, "pending") {
		return fmt.Errorf("proposal %s must be pending to update", p.ID)
	}

	if p.Title == "" {
		p.Title = existing.Title
	}
	if p.DescriptionMD == "" {
		p.DescriptionMD = existing.DescriptionMD
	}
	if p.BudgetSats == 0 {
		p.BudgetSats = existing.BudgetSats
	}

	metadata, _ := json.Marshal(p.Metadata)
	_, err = s.db.ExecContext(ctx, `
UPDATE mcp_proposals SET title=?, description_md=?, budget_sats=?, metadata=? WHERE id=?
`, p.Title, p.DescriptionMD, p.BudgetSats, string(metadata), p.ID)
	return err
}

func (s *SQLiteStore) UpdateProposalMetadata(ctx context.Context, id string, updates map[string]interface{}) error {
	existing, err := s.GetProposal(ctx, id)
	if err != nil {
		return err
	}
	meta := existing.Metadata
	if meta == nil {
		meta = map[string]interface{}{}
	}
	for k, v := range updates {
		meta[k] = v
	}
	metadata, _ := json.Marshal(meta)
	_, err = s.db.ExecContext(ctx, `UPDATE mcp_proposals SET metadata=? WHERE id=?`, string(metadata), id)
	return err
}

func (s *SQLiteStore) ApproveProposal(ctx context.Context, id string) error {
	_, err := s.GetProposal(ctx, id)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE mcp_proposals SET status='approved' WHERE id=?`, id)
	return err
}

func (s *SQLiteStore) PublishProposal(ctx context.Context, id string) error {
	_, err := s.GetProposal(ctx, id)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE mcp_proposals SET status='published' WHERE id=?`, id)
	return err
}

func (s *SQLiteStore) UpdateSubmissionStatus(ctx context.Context, submissionID, status, reviewerNotes, rejectionType string) error {
	var rejectedAt interface{}
	if status == "rejected" {
		rejectedAt = time.Now().Format(time.RFC3339)
	}
	_, err := s.db.ExecContext(ctx, `
UPDATE mcp_submissions SET status=?, rejection_reason=?, rejection_type=?, rejected_at=? WHERE submission_id=?
`, status, reviewerNotes, rejectionType, rejectedAt, submissionID)
	return err
}

func (s *SQLiteStore) UpdateSubmission(ctx context.Context, sub smart_contract.Submission) error {
	if sub.Deliverables != nil {
		delete(sub.Deliverables, "status")
	}
	if sub.CompletionProof != nil {
		delete(sub.CompletionProof, "status")
	}

	delivJSON, _ := json.Marshal(sub.Deliverables)
	proofJSON, _ := json.Marshal(sub.CompletionProof)
	_, err := s.db.ExecContext(ctx, `
UPDATE mcp_submissions SET status=?, deliverables=?, completion_proof=? WHERE submission_id=?
`, sub.Status, string(delivJSON), string(proofJSON), sub.SubmissionID)
	return err
}

func (s *SQLiteStore) DeleteWish(ctx context.Context, visiblePixelHash string) error {
	wishID := "wish-" + visiblePixelHash

	_, err := s.db.ExecContext(ctx, `DELETE FROM mcp_proposals WHERE id=? OR visible_pixel_hash=?`, wishID, visiblePixelHash)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, `DELETE FROM mcp_tasks WHERE contract_id=?`, wishID)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, `DELETE FROM mcp_contracts WHERE contract_id=?`, wishID)
	return err
}

func (s *SQLiteStore) CreateContractReworkRequest(ctx context.Context, contractID, requester, notes string) (smart_contract.ContractReworkRequest, error) {
	requestID := fmt.Sprintf("rework-%s-%d", contractID, time.Now().UnixNano())
	now := time.Now()

	reworkReq := smart_contract.ContractReworkRequest{
		RequestID:  requestID,
		ContractID: contractID,
		Requester:  requester,
		Notes:      notes,
		Status:     "open",
		CreatedAt:  now,
	}

	_, err := s.db.ExecContext(ctx, `
INSERT INTO mcp_proposals (id, title, description_md, status, metadata, created_at)
VALUES (?, ?, ?, 'rework', ?, datetime('now'))
ON CONFLICT(id) DO UPDATE SET metadata = excluded.metadata
`, requestID, "Contract Rework Request", notes, `{"requester":"`+requester+`","contract_id":"`+contractID+`"}`)
	if err != nil {
		return smart_contract.ContractReworkRequest{}, err
	}

	return reworkReq, nil
}

func (s *SQLiteStore) GetContractReworkRequests(ctx context.Context, contractID string) ([]smart_contract.ContractReworkRequest, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, title, description_md, metadata, created_at
FROM mcp_proposals WHERE id LIKE 'rework-%-%' AND metadata->>'contract_id' = ?
`, contractID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reqs []smart_contract.ContractReworkRequest
	for rows.Next() {
		var id, title, desc string
		var metadata []byte
		var createdAt time.Time
		if err := rows.Scan(&id, &title, &desc, &metadata, &createdAt); err != nil {
			continue
		}
		reqs = append(reqs, smart_contract.ContractReworkRequest{
			RequestID:  id,
			ContractID: contractID,
			Notes:     desc,
			CreatedAt:  createdAt,
		})
	}
	return reqs, nil
}

func (s *SQLiteStore) ResolveContractReworkRequest(ctx context.Context, contractID, requestID string) error {
	return nil
}