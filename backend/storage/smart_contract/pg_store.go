package smart_contract

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"stargate-backend/core/smart_contract"
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
  rejection_reason TEXT,
  rejection_type TEXT,
  rejected_at TIMESTAMPTZ,
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
ALTER TABLE mcp_submissions ADD COLUMN IF NOT EXISTS rejection_reason TEXT;
ALTER TABLE mcp_submissions ADD COLUMN IF NOT EXISTS rejection_type TEXT;
ALTER TABLE mcp_submissions ADD COLUMN IF NOT EXISTS rejected_at TIMESTAMPTZ;
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

// ListContracts returns all contracts filtered by status, skill, and metadata fields.
func (s *PGStore) ListContracts(filter smart_contract.ContractFilter) ([]smart_contract.Contract, error) {
	ctx := context.Background()
	rows, err := s.pool.Query(ctx, `
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

// hydrateProposalTasks updates proposal tasks with live task statuses from the DB.
func (s *PGStore) hydrateProposalTasks(ctx context.Context, p *smart_contract.Proposal) {
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

	liveTasks := make(map[string]smart_contract.Task)
	taskIDs := make([]string, 0)
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			continue
		}
		liveTasks[t.TaskID] = t
		taskIDs = append(taskIDs, t.TaskID)
	}
	if len(liveTasks) == 0 {
		return
	}

	// attach active claims
	tasksSlice := make([]smart_contract.Task, 0, len(liveTasks))
	for _, t := range liveTasks {
		tasksSlice = append(tasksSlice, t)
	}
	tasksSlice = s.attachActiveClaims(ctx, tasksSlice, taskIDs)
	liveTasks = make(map[string]smart_contract.Task, len(tasksSlice))
	for _, t := range tasksSlice {
		liveTasks[t.TaskID] = t
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
			p.Tasks[i].ActiveClaimID = lt.ActiveClaimID
			if lt.MerkleProof != nil {
				p.Tasks[i].MerkleProof = lt.MerkleProof
				if strings.TrimSpace(p.Tasks[i].ContractorWallet) == "" && strings.TrimSpace(lt.MerkleProof.ContractorWallet) != "" {
					p.Tasks[i].ContractorWallet = strings.TrimSpace(lt.MerkleProof.ContractorWallet)
				}
			}
			if strings.TrimSpace(lt.ContractorWallet) != "" {
				p.Tasks[i].ContractorWallet = strings.TrimSpace(lt.ContractorWallet)
			}
			if len(lt.Skills) > 0 {
				p.Tasks[i].Skills = lt.Skills
			}
		}
	}
}

// ListTasks returns tasks filtered by a TaskFilter.
func (s *PGStore) ListTasks(filter smart_contract.TaskFilter) ([]smart_contract.Task, error) {
	ctx := context.Background()
	rows, err := s.pool.Query(ctx, `
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
	taskIDs := make([]string, 0)
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
		taskIDs = append(taskIDs, task.TaskID)
	}
	out = s.attachActiveClaims(ctx, out, taskIDs)
	if filter.Offset > 0 && filter.Offset < len(out) {
		out = out[filter.Offset:]
	}
	if filter.Limit > 0 && filter.Limit < len(out) {
		out = out[:filter.Limit]
	}
	return out, rows.Err()
}

// GetTask returns a task by ID.
func (s *PGStore) GetTask(id string) (smart_contract.Task, error) {
	ctx := context.Background()
	row := s.pool.QueryRow(ctx, `
SELECT task_id, contract_id, goal_id, title, description, budget_sats, skills, status, claimed_by, claimed_at, claim_expires_at, difficulty, estimated_hours, requirements, merkle_proof
FROM mcp_tasks WHERE task_id=$1
`, id)
	task, err := scanTask(row)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") {
			return smart_contract.Task{}, ErrTaskNotFound
		}
		return smart_contract.Task{}, err
	}
	out := s.attachActiveClaims(ctx, []smart_contract.Task{task}, []string{id})
	if len(out) > 0 {
		return out[0], nil
	}
	return task, nil
}

// GetContract returns a contract by ID.
func (s *PGStore) GetContract(id string) (smart_contract.Contract, error) {
	ctx := context.Background()
	var c smart_contract.Contract
	err := s.pool.QueryRow(ctx, `
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

// ClaimTask reserves a task for an AI. It is idempotent if the same AI reclaims before expiry.
func (s *PGStore) ClaimTask(taskID, aiID, contractorWallet string, estimatedCompletion *time.Time) (smart_contract.Claim, error) {
	ctx := context.Background()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return smart_contract.Claim{}, err
	}
	defer tx.Rollback(ctx)

	task, err := scanTask(tx.QueryRow(ctx, `
SELECT task_id, contract_id, goal_id, title, description, budget_sats, skills, status, claimed_by, claimed_at, claim_expires_at, difficulty, estimated_hours, requirements, merkle_proof
FROM mcp_tasks WHERE task_id=$1 FOR UPDATE
`, taskID))
	if err != nil {
		return smart_contract.Claim{}, ErrTaskNotFound
	}
	if strings.EqualFold(task.Status, "approved") || strings.EqualFold(task.Status, "completed") || strings.EqualFold(task.Status, "published") || strings.EqualFold(task.Status, "claimed") || strings.EqualFold(task.Status, "submitted") {
		return smart_contract.Claim{}, ErrTaskUnavailable
	}

	normalizedWallet := strings.TrimSpace(contractorWallet)
	if normalizedWallet != "" {
		if err := ValidateBitcoinAddress(normalizedWallet); err != nil {
			return smart_contract.Claim{}, fmt.Errorf("contractor wallet validation failed: %v", err)
		}
	}
	persistWallet := func(wallet string) error {
		wallet = strings.TrimSpace(wallet)
		if wallet == "" {
			return nil
		}
		if strings.EqualFold(strings.TrimSpace(task.ContractorWallet), wallet) {
			return nil
		}
		task.ContractorWallet = wallet
		if task.MerkleProof == nil {
			task.MerkleProof = &smart_contract.MerkleProof{}
		}
		task.MerkleProof.ContractorWallet = wallet
		proofJSON, _ := json.Marshal(task.MerkleProof)
		_, err := tx.Exec(ctx, `UPDATE mcp_tasks SET merkle_proof=$2 WHERE task_id=$1`, taskID, string(proofJSON))
		return err
	}

	rows, err := tx.Query(ctx, `SELECT claim_id, task_id, ai_identifier, status, expires_at, created_at FROM mcp_claims WHERE task_id=$1`, taskID)
	if err != nil {
		return smart_contract.Claim{}, err
	}
	defer rows.Close()
	now := time.Now()
	for rows.Next() {
		var c smart_contract.Claim
		if err := rows.Scan(&c.ClaimID, &c.TaskID, &c.AiIdentifier, &c.Status, &c.ExpiresAt, &c.CreatedAt); err != nil {
			return smart_contract.Claim{}, err
		}
		if c.Status == "active" && now.Before(c.ExpiresAt) {
			if c.AiIdentifier == aiID {
				if err := persistWallet(normalizedWallet); err != nil {
					return smart_contract.Claim{}, err
				}
				return c, tx.Commit(ctx)
			}
			return smart_contract.Claim{}, ErrTaskTaken
		}
	}

	claimID := fmt.Sprintf("CLAIM-%d", time.Now().UnixNano())
	expires := now.Add(s.claimTTL)
	claim := smart_contract.Claim{
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
		return smart_contract.Claim{}, err
	}

	_, err = tx.Exec(ctx, `
UPDATE mcp_tasks SET status='claimed', claimed_by=$2, claimed_at=$3, claim_expires_at=$4 WHERE task_id=$1
`, taskID, aiID, claim.CreatedAt, claim.ExpiresAt)
	if err != nil {
		return smart_contract.Claim{}, err
	}
	if err := persistWallet(normalizedWallet); err != nil {
		return smart_contract.Claim{}, err
	}

	_ = estimatedCompletion // placeholder to persist ETA later
	if err := tx.Commit(ctx); err != nil {
		return smart_contract.Claim{}, err
	}
	return claim, nil
}

// SubmitWork records a submission for a claim.
func (s *PGStore) SubmitWork(claimID string, deliverables map[string]interface{}, proof map[string]interface{}) (smart_contract.Submission, error) {
	ctx := context.Background()

	// Log the submission attempt
	log.Printf("SubmitWork called for claimID: %s", claimID)

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		log.Printf("Failed to begin transaction: %v", err)
		return smart_contract.Submission{}, fmt.Errorf("database transaction failed: %v", err)
	}
	defer func() {
		if r := tx.Rollback(ctx); r != nil {
			log.Printf("Warning: transaction rollback failed: %v", r)
		}
	}()

	var claim smart_contract.Claim
	err = tx.QueryRow(ctx, `SELECT claim_id, task_id, ai_identifier, status, expires_at, created_at FROM mcp_claims WHERE claim_id=$1`, claimID).
		Scan(&claim.ClaimID, &claim.TaskID, &claim.AiIdentifier, &claim.Status, &claim.ExpiresAt, &claim.CreatedAt)
	if err != nil {
		return smart_contract.Submission{}, ErrClaimNotFound
	}
	// Allow submissions on active claims OR submitted claims with existing rejected/reviewed submissions
	if claim.Status != "active" && claim.Status != "submitted" {
		return smart_contract.Submission{}, fmt.Errorf("claim %s not active or submitted", claimID)
	}
	if time.Now().After(claim.ExpiresAt) {
		_, _ = tx.Exec(ctx, `UPDATE mcp_claims SET status='expired' WHERE claim_id=$1`, claimID)
		return smart_contract.Submission{}, fmt.Errorf("claim %s expired", claimID)
	}

	// For submitted claims, check if there's an existing submission that allows resubmission
	if claim.Status == "submitted" {
		// Query existing submissions for this claim
		log.Printf("Querying existing submissions for claimID: %s", claimID)
		rows, err := tx.Query(ctx, `SELECT status FROM mcp_submissions WHERE claim_id=$1`, claimID)
		if err != nil {
			log.Printf("Failed to query existing submissions: %v", err)
			return smart_contract.Submission{}, err
		}
		defer rows.Close()

		canResubmit := false
		existingStatuses := make([]string, 0)
		for rows.Next() {
			var existingStatus string
			if err := rows.Scan(&existingStatus); err != nil {
				log.Printf("Failed to scan submission status: %v", err)
				continue
			}
			existingStatuses = append(existingStatuses, existingStatus)
			if existingStatus == "rejected" || existingStatus == "reviewed" {
				canResubmit = true
				log.Printf("Found eligible resubmission status: %s", existingStatus)
				break
			}
		}

		// Close rows before any updates
		rows.Close()

		log.Printf("Existing submission statuses for claim %s: %v", claimID, existingStatuses)

		if !canResubmit {
			return smart_contract.Submission{}, fmt.Errorf("claim %s already submitted with no eligible resubmission", claimID)
		}

		// Reactivate the claim for resubmission
		log.Printf("Reactivating claim %s for resubmission", claimID)
		if _, err := tx.Exec(ctx, `UPDATE mcp_claims SET status='active' WHERE claim_id=$1`, claimID); err != nil {
			log.Printf("Failed to reactivate claim: %v", err)
			return smart_contract.Submission{}, err
		}
		log.Printf("Successfully reactivated claim %s", claimID)

		// Brief pause to allow connection pool to recover
		time.Sleep(10 * time.Millisecond)
	}

	subID := fmt.Sprintf("SUB-%d", time.Now().UnixNano())
	delivJSON, _ := json.Marshal(deliverables)
	proofJSON, _ := json.Marshal(proof)
	sub := smart_contract.Submission{
		SubmissionID:    subID,
		ClaimID:         claimID,
		Status:          "pending_review",
		Deliverables:    deliverables,
		CompletionProof: proof,
		CreatedAt:       time.Now(),
	}

	log.Printf("Creating new submission: ID=%s, ClaimID=%s", subID, claimID)

	if _, err := tx.Exec(ctx, `
	INSERT INTO mcp_submissions (submission_id, claim_id, status, deliverables, completion_proof, created_at)
	VALUES ($1,$2,$3,$4,$5,$6)
	`, sub.SubmissionID, sub.ClaimID, sub.Status, delivJSON, proofJSON, sub.CreatedAt); err != nil {
		log.Printf("Failed to insert submission: %v", err)
		return smart_contract.Submission{}, err
	}
	log.Printf("Inserted submission, updating claim and task status to submitted")

	// Update claim and task status to submitted for the new submission
	if _, err := tx.Exec(ctx, `UPDATE mcp_claims SET status='submitted' WHERE claim_id=$1`, claimID); err != nil {
		log.Printf("Failed to update claim status: %v", err)
		return smart_contract.Submission{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE mcp_tasks SET status='submitted' WHERE task_id=$1`, claim.TaskID); err != nil {
		log.Printf("Failed to update task status: %v", err)
		return smart_contract.Submission{}, err
	}

	log.Printf("Committing transaction for submission %s", subID)
	if err := tx.Commit(ctx); err != nil {
		log.Printf("Failed to commit transaction: %v", err)
		return smart_contract.Submission{}, err
	}

	log.Printf("Successfully created submission %s for claim %s", subID, claimID)
	return sub, nil
}

// ListSubmissions returns submissions for the given task IDs by joining claims.
func (s *PGStore) ListSubmissions(ctx context.Context, taskIDs []string) ([]smart_contract.Submission, error) {
	if len(taskIDs) == 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `
SELECT s.submission_id, s.claim_id, c.task_id, s.status, s.deliverables, s.completion_proof, s.rejection_reason, s.rejection_type, s.rejected_at, s.created_at
FROM mcp_submissions s
JOIN mcp_claims c ON c.claim_id = s.claim_id
WHERE c.task_id = ANY($1::text[])
ORDER BY s.created_at DESC
`, taskIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []smart_contract.Submission
	for rows.Next() {
		var sub smart_contract.Submission
		var delivJSON, proofJSON []byte
		var rejectionReason sql.NullString
		var rejectionType sql.NullString
		var rejectedAt sql.NullTime
		if err := rows.Scan(&sub.SubmissionID, &sub.ClaimID, &sub.TaskID, &sub.Status, &delivJSON, &proofJSON, &rejectionReason, &rejectionType, &rejectedAt, &sub.CreatedAt); err != nil {
			return nil, err
		}
		if rejectionReason.Valid {
			sub.RejectionReason = rejectionReason.String
		}
		if rejectionType.Valid {
			sub.RejectionType = rejectionType.String
		}
		if rejectedAt.Valid {
			t := rejectedAt.Time
			sub.RejectedAt = &t
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

// TaskStatus returns task status, including claim info if present.
func (s *PGStore) TaskStatus(taskID string) (map[string]interface{}, error) {
	task, err := s.GetTask(taskID)
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	var claim smart_contract.Claim
	err = s.pool.QueryRow(ctx, `
SELECT claim_id, task_id, ai_identifier, status, expires_at, created_at
FROM mcp_claims
WHERE task_id=$1 AND status IN ('active','submitted','pending_review')
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
	var submissionAttempts int
	if err := s.pool.QueryRow(ctx, `
SELECT COUNT(*)
FROM mcp_submissions s
JOIN mcp_claims c ON c.claim_id = s.claim_id
WHERE c.task_id=$1
`, taskID).Scan(&submissionAttempts); err == nil {
		resp["submission_attempts"] = submissionAttempts
	} else {
		resp["submission_attempts"] = 0
	}
	if claim.ClaimID != "" {
		final := strings.EqualFold(task.Status, "published") || strings.EqualFold(task.Status, "approved") || strings.EqualFold(task.Status, "completed")
		switch strings.ToLower(claim.Status) {
		case "submitted", "pending_review":
			if !final {
				resp["status"] = "submitted"
			}
		case "active":
			if !final && (task.Status == "" || strings.EqualFold(task.Status, "available") || strings.EqualFold(task.Status, "approved")) {
				resp["status"] = "claimed"
			}
		case "complete":
			resp["status"] = "approved"
		}
		remaining := time.Until(claim.ExpiresAt).Hours()
		resp["time_remaining_hr"] = remaining
		resp["claim_id"] = claim.ClaimID
		if resp["claimed_by"] == "" {
			resp["claimed_by"] = claim.AiIdentifier
		}
		if resp["claim_expires_at"] == nil && !claim.ExpiresAt.IsZero() {
			resp["claim_expires_at"] = claim.ExpiresAt
		}
		if resp["claimed_at"] == nil && !claim.CreatedAt.IsZero() {
			resp["claimed_at"] = claim.CreatedAt
		}
	}
	return resp, nil
}

// GetTaskProof returns the Merkle proof for a task.
func (s *PGStore) GetTaskProof(taskID string) (*smart_contract.MerkleProof, error) {
	task, err := s.GetTask(taskID)
	if err != nil {
		return nil, err
	}
	return task.MerkleProof, nil
}

// ContractFunding returns the contract and any proofs of funding.
func (s *PGStore) ContractFunding(contractID string) (smart_contract.Contract, []smart_contract.MerkleProof, error) {
	contract, err := s.GetContract(contractID)
	if err != nil {
		return smart_contract.Contract{}, nil, err
	}
	ctx := context.Background()
	rows, err := s.pool.Query(ctx, `SELECT merkle_proof FROM mcp_tasks WHERE contract_id=$1`, contractID)
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

// UpsertContractWithTasks persists a contract and its tasks idempotently.
func (s *PGStore) UpsertContractWithTasks(ctx context.Context, contract smart_contract.Contract, tasks []smart_contract.Task) error {
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
		} else {
			proofJSON = []byte("null")
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
func (s *PGStore) UpdateTaskProof(ctx context.Context, taskID string, proof *smart_contract.MerkleProof) error {
	if proof == nil {
		return nil
	}
	if existing, err := s.GetTaskProof(taskID); err == nil && existing != nil {
		if strings.TrimSpace(existing.ContractorWallet) != "" && strings.TrimSpace(proof.ContractorWallet) == "" {
			proof.ContractorWallet = strings.TrimSpace(existing.ContractorWallet)
		}
	}
	b, err := json.Marshal(proof)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `UPDATE mcp_tasks SET merkle_proof=$2 WHERE task_id=$1`, taskID, string(b))
	return err
}

// Proposal operations
func (s *PGStore) CreateProposal(ctx context.Context, p smart_contract.Proposal) error {
	if p.Metadata == nil {
		p.Metadata = map[string]interface{}{}
	}
	if strings.TrimSpace(p.VisiblePixelHash) != "" {
		if vph, ok := p.Metadata["visible_pixel_hash"].(string); !ok || strings.TrimSpace(vph) == "" {
			p.Metadata["visible_pixel_hash"] = p.VisiblePixelHash
		}
	}
	if metaContract, ok := p.Metadata["contract_id"].(string); ok {
		metaContract = strings.TrimSpace(metaContract)
		if metaContract != "" {
			if metaHash, ok2 := p.Metadata["visible_pixel_hash"].(string); ok2 {
				metaHash = strings.TrimSpace(metaHash)
				if metaHash != "" && metaHash != metaContract {
					return fmt.Errorf("visible_pixel_hash must match contract_id when both are set")
				}
			}
		}
	}

	// Comprehensive security validation - this sanitizes inputs in-place
	if err := ValidateProposalInput(p); err != nil {
		return fmt.Errorf("proposal validation failed: %v", err)
	}

	// Validate status field
	if p.Status == "" {
		p.Status = "pending" // Default to pending
	} else if !isValidProposalStatus(p.Status) {
		return fmt.Errorf("invalid proposal status: %s (must be one of: pending, approved, rejected, published)", p.Status)
	}

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
	if err != nil {
		return err
	}

	if strings.EqualFold(p.Status, "approved") || strings.EqualFold(p.Status, "published") {
		visible := strings.TrimSpace(p.VisiblePixelHash)
		if visible == "" {
			if v, ok := p.Metadata["visible_pixel_hash"].(string); ok {
				visible = strings.TrimSpace(v)
			}
		}
		if visible == "" {
			if v, ok := p.Metadata["contract_id"].(string); ok {
				visible = strings.TrimSpace(v)
			}
		}
		if visible != "" {
			wishID := "wish-" + visible
			_, _ = s.pool.Exec(ctx, `UPDATE mcp_contracts SET status='superseded' WHERE contract_id=$1`, wishID)
		}
	}
	return nil
}

func (s *PGStore) ListProposals(ctx context.Context, filter smart_contract.ProposalFilter) ([]smart_contract.Proposal, error) {
	query := `SELECT id, title, description_md, visible_pixel_hash, budget_sats, status, metadata, created_at FROM mcp_proposals`
	var args []interface{}
	if filter.Status != "" {
		query += " WHERE status=$1"
		args = append(args, filter.Status)
	}
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []smart_contract.Proposal
	for rows.Next() {
		var p smart_contract.Proposal
		var meta []byte
		if err := rows.Scan(&p.ID, &p.Title, &p.DescriptionMD, &p.VisiblePixelHash, &p.BudgetSats, &p.Status, &meta, &p.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(meta, &p.Metadata)
		populateProposalTasks(&p)
		s.hydrateProposalTasks(ctx, &p)
		if filter.ContractID != "" {
			contractID := p.Metadata["contract_id"]
			if contractID == nil {
				contractID = p.Metadata["ingestion_id"]
			}
			cid, _ := contractID.(string)
			if cid == "" {
				cid = p.ID
			}
			if cid != filter.ContractID {
				continue
			}
		}
		if filter.MinBudget > 0 && p.BudgetSats < filter.MinBudget {
			continue
		}
		if len(filter.Skills) > 0 && !proposalHasSkills(p, filter.Skills) {
			continue
		}
		out = append(out, p)
	}
	if filter.Offset > 0 && filter.Offset < len(out) {
		out = out[filter.Offset:]
	}
	if filter.MaxResults > 0 && filter.MaxResults < len(out) {
		out = out[:filter.MaxResults]
	}
	return out, rows.Err()
}

func (s *PGStore) GetProposal(ctx context.Context, id string) (smart_contract.Proposal, error) {
	var p smart_contract.Proposal
	var meta []byte
	err := s.pool.QueryRow(ctx, `
SELECT id, title, description_md, visible_pixel_hash, budget_sats, status, metadata, created_at
FROM mcp_proposals WHERE id=$1
`, id).Scan(&p.ID, &p.Title, &p.DescriptionMD, &p.VisiblePixelHash, &p.BudgetSats, &p.Status, &meta, &p.CreatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") {
			return smart_contract.Proposal{}, fmt.Errorf("proposal %s not found", id)
		}
		return smart_contract.Proposal{}, err
	}
	_ = json.Unmarshal(meta, &p.Metadata)
	populateProposalTasks(&p)
	s.hydrateProposalTasks(ctx, &p)
	return p, nil
}

// UpdateProposal updates a pending proposal.
func (s *PGStore) UpdateProposal(ctx context.Context, p smart_contract.Proposal) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var status string
	var metaJSON []byte
	var current smart_contract.Proposal
	if err := tx.QueryRow(ctx, `
SELECT title, description_md, visible_pixel_hash, budget_sats, status, metadata, created_at
FROM mcp_proposals WHERE id=$1 FOR UPDATE
`, p.ID).Scan(&current.Title, &current.DescriptionMD, &current.VisiblePixelHash, &current.BudgetSats, &status, &metaJSON, &current.CreatedAt); err != nil {
		if strings.Contains(err.Error(), "no rows") {
			return fmt.Errorf("proposal %s not found", p.ID)
		}
		return err
	}
	current.ID = p.ID
	current.Status = status
	_ = json.Unmarshal(metaJSON, &current.Metadata)
	populateProposalTasks(&current)

	if !strings.EqualFold(current.Status, "pending") {
		return fmt.Errorf("proposal %s must be pending to update, current status: %s", p.ID, current.Status)
	}

	if p.Title == "" {
		p.Title = current.Title
	}
	if p.DescriptionMD == "" {
		p.DescriptionMD = current.DescriptionMD
	}
	if p.VisiblePixelHash == "" {
		p.VisiblePixelHash = current.VisiblePixelHash
	}
	if p.BudgetSats == 0 {
		p.BudgetSats = current.BudgetSats
	}
	if p.Metadata == nil {
		p.Metadata = current.Metadata
	}
	if p.Tasks == nil {
		p.Tasks = current.Tasks
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = current.CreatedAt
	}

	p.Status = current.Status
	if p.Metadata == nil {
		p.Metadata = map[string]interface{}{}
	}
	if strings.TrimSpace(p.VisiblePixelHash) != "" {
		if vph, ok := p.Metadata["visible_pixel_hash"].(string); !ok || strings.TrimSpace(vph) == "" {
			p.Metadata["visible_pixel_hash"] = p.VisiblePixelHash
		}
	}

	if err := ValidateProposalInput(p); err != nil {
		return fmt.Errorf("proposal validation failed: %v", err)
	}

	metaMap := p.Metadata
	if metaMap == nil {
		metaMap = map[string]interface{}{}
	}
	if len(p.Tasks) > 0 {
		metaMap["suggested_tasks"] = p.Tasks
	} else {
		delete(metaMap, "suggested_tasks")
	}
	metaOut, _ := json.Marshal(metaMap)

	if _, err := tx.Exec(ctx, `
UPDATE mcp_proposals
SET title=$2, description_md=$3, visible_pixel_hash=$4, budget_sats=$5, metadata=$6
WHERE id=$1
`, p.ID, p.Title, p.DescriptionMD, p.VisiblePixelHash, p.BudgetSats, string(metaOut)); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// UpdateProposalMetadata updates proposal metadata without status restrictions.
func (s *PGStore) UpdateProposalMetadata(ctx context.Context, id string, updates map[string]interface{}) error {
	if strings.TrimSpace(id) == "" || len(updates) == 0 {
		return nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var metaJSON []byte
	var visiblePixelHash string
	if err := tx.QueryRow(ctx, `
SELECT metadata, visible_pixel_hash
FROM mcp_proposals WHERE id=$1 FOR UPDATE
`, id).Scan(&metaJSON, &visiblePixelHash); err != nil {
		if strings.Contains(err.Error(), "no rows") {
			return fmt.Errorf("proposal %s not found", id)
		}
		return err
	}

	var meta map[string]interface{}
	_ = json.Unmarshal(metaJSON, &meta)
	if meta == nil {
		meta = map[string]interface{}{}
	}
	for k, v := range updates {
		meta[k] = v
	}
	if strings.TrimSpace(visiblePixelHash) != "" {
		if vph, ok := meta["visible_pixel_hash"].(string); !ok || strings.TrimSpace(vph) == "" {
			meta["visible_pixel_hash"] = visiblePixelHash
		}
	}
	metaOut, _ := json.Marshal(meta)

	if _, err := tx.Exec(ctx, `
UPDATE mcp_proposals
SET metadata=$2
WHERE id=$1
`, id, string(metaOut)); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (s *PGStore) ApproveProposal(ctx context.Context, id string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Load and lock the proposal row
	var metaJSON []byte
	var currentStatus string
	var visiblePixelHash string
	if err := tx.QueryRow(ctx, `SELECT metadata, status, visible_pixel_hash FROM mcp_proposals WHERE id=$1 FOR UPDATE`, id).Scan(&metaJSON, &currentStatus, &visiblePixelHash); err != nil {
		return err
	}

	// Check if proposal is already in final state
	if strings.EqualFold(currentStatus, "approved") || strings.EqualFold(currentStatus, "published") {
		return fmt.Errorf("proposal %s is already %s", id, currentStatus)
	}

	if !strings.EqualFold(currentStatus, "pending") {
		return fmt.Errorf("proposal %s must be pending to approve, current status: %s", id, currentStatus)
	}

	var meta map[string]interface{}
	if err := json.Unmarshal(metaJSON, &meta); err != nil {
		return err
	}
	if strings.TrimSpace(visiblePixelHash) != "" {
		if meta == nil {
			meta = map[string]interface{}{}
		}
		if vph, ok := meta["visible_pixel_hash"].(string); !ok || strings.TrimSpace(vph) == "" {
			meta["visible_pixel_hash"] = visiblePixelHash
		}
	}
	contractID := contractIDFromMeta(meta, id)

	// Block double-approval/publish for the same contract.
	var conflict int
	if err := tx.QueryRow(ctx, `
SELECT count(*) FROM mcp_proposals
WHERE id<>$1 AND status IN ('approved','published')
AND (
  metadata->>'contract_id' = $2 OR
  metadata->>'ingestion_id' = $2 OR
  metadata->>'visible_pixel_hash' = $2 OR
  id = $2
)`, id, contractID).Scan(&conflict); err != nil {
		return err
	}
	if conflict > 0 {
		return fmt.Errorf("another proposal is already approved/published for contract %s", contractID)
	}
	// Auto-reject any other pending proposals for this contract.
	_, _ = tx.Exec(ctx, `
UPDATE mcp_proposals SET status='rejected'
WHERE id<>$1 AND status='pending' AND (
  metadata->>'contract_id' = $2 OR
  metadata->>'ingestion_id' = $2 OR
  metadata->>'visible_pixel_hash' = $2 OR
  id = $2
)`, id, contractID)

	// Load complete proposal for validation
	proposal, err := s.GetProposal(ctx, id)
	if err != nil {
		return err
	}

	// HACK: temporarily set status to approved to trigger validation
	originalStatus := proposal.Status
	proposal.Status = "approved"
	err = ValidateProposalInput(proposal)
	proposal.Status = originalStatus // revert status
	if err != nil {
		return fmt.Errorf("proposal validation failed: %v", err)
	}
	if len(proposal.Tasks) == 0 {
		var taskCount int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM mcp_tasks WHERE contract_id=$1`, contractID).Scan(&taskCount); err != nil {
			return err
		}
		if taskCount == 0 {
			return fmt.Errorf("approved proposals must contain at least one task")
		}
	}

	if _, err := tx.Exec(ctx, `UPDATE mcp_proposals SET status='approved' WHERE id=$1`, id); err != nil {
		return err
	}
	visible := strings.TrimSpace(visiblePixelHash)
	if visible == "" {
		if v, ok := meta["visible_pixel_hash"].(string); ok {
			visible = strings.TrimSpace(v)
		}
	}
	if visible != "" {
		wishID := "wish-" + visible
		_, _ = tx.Exec(ctx, `UPDATE mcp_contracts SET status='superseded' WHERE contract_id=$1`, wishID)
	}

	return tx.Commit(ctx)
}

// PublishProposal marks tasks for a proposal's contract as published.
func (s *PGStore) PublishProposal(ctx context.Context, id string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var status string
	var metaJSON []byte
	if err := tx.QueryRow(ctx, `SELECT status, metadata FROM mcp_proposals WHERE id=$1`, id).Scan(&status, &metaJSON); err != nil {
		return err
	}
	if !strings.EqualFold(status, "approved") && !strings.EqualFold(status, "published") {
		return fmt.Errorf("proposal %s must be approved before publish", id)
	}

	var meta map[string]interface{}
	_ = json.Unmarshal(metaJSON, &meta)
	contractID := contractIDFromMeta(meta, id)

	if _, err := tx.Exec(ctx, `UPDATE mcp_tasks SET status='published' WHERE contract_id=$1 AND status IN ('submitted','pending_review','claimed','approved')`, contractID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE mcp_claims SET status='complete' WHERE task_id IN (SELECT task_id FROM mcp_tasks WHERE contract_id=$1) AND status IN ('submitted','pending_review','active','approved')`, contractID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE mcp_proposals SET status='published' WHERE id=$1`, id); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// populateProposalTasks hydrates Tasks from metadata suggested_tasks or embedded_message.
func populateProposalTasks(p *smart_contract.Proposal) {
	if p == nil {
		return
	}
	if p.BudgetSats == 0 {
		p.BudgetSats = DefaultBudgetSats()
		if p.Metadata == nil {
			p.Metadata = map[string]interface{}{}
		}
		p.Metadata["budget_sats"] = p.BudgetSats
	}
	if p.Metadata == nil {
		p.Metadata = map[string]interface{}{}
	}
	if _, ok := p.Metadata["funding_address"]; !ok {
		p.Metadata["funding_address"] = FundingAddressFromMeta(p.Metadata)
	}
	if tasksRaw, ok := p.Metadata["suggested_tasks"]; ok {
		var tasks []smart_contract.Task
		if b, err := json.Marshal(tasksRaw); err == nil {
			_ = json.Unmarshal(b, &tasks)
		}
		if len(tasks) > 0 {
			p.Tasks = tasks
			return
		}
	}
	if em, ok := p.Metadata["embedded_message"].(string); ok && em != "" && len(p.Tasks) == 0 {
		p.Tasks = BuildTasksFromMarkdown(p.ID, em, p.VisiblePixelHash, p.BudgetSats, FundingAddressFromMeta(p.Metadata))
	}
	if len(p.Tasks) == 0 {
		desc := strings.TrimSpace(p.DescriptionMD)
		if desc != "" {
			p.Tasks = BuildTasksFromMarkdown(p.ID, desc, p.VisiblePixelHash, p.BudgetSats, FundingAddressFromMeta(p.Metadata))
		}
	}
}

func scanTask(scanner interface {
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
		var proof smart_contract.MerkleProof
		if err := json.Unmarshal(proofJSON, &proof); err == nil {
			t.MerkleProof = &proof
			if strings.TrimSpace(proof.ContractorWallet) != "" {
				t.ContractorWallet = strings.TrimSpace(proof.ContractorWallet)
			}
		}
	}
	return t, nil
}

// attachActiveClaims enriches tasks with active claim ids from the claims table.
func (s *PGStore) attachActiveClaims(ctx context.Context, tasks []smart_contract.Task, taskIDs []string) []smart_contract.Task {
	if len(tasks) == 0 || len(taskIDs) == 0 {
		return tasks
	}
	rows, err := s.pool.Query(ctx, `
SELECT task_id, claim_id, status, ai_identifier, expires_at, created_at FROM mcp_claims
WHERE task_id = ANY($1::text[]) AND status IN ('active','submitted','pending_review') 
ORDER BY created_at DESC
`, taskIDs)
	if err != nil {
		return tasks
	}
	defer rows.Close()
	type claimInfo struct {
		claimID   string
		status    string
		ai        string
		expiresAt time.Time
		createdAt time.Time
	}
	claimMap := make(map[string]claimInfo)
	for rows.Next() {
		var taskID, claimID, status, ai string
		var expiresAt, createdAt time.Time
		if err := rows.Scan(&taskID, &claimID, &status, &ai, &expiresAt, &createdAt); err == nil {
			if _, ok := claimMap[taskID]; !ok {
				claimMap[taskID] = claimInfo{
					claimID:   claimID,
					status:    status,
					ai:        ai,
					expiresAt: expiresAt,
					createdAt: createdAt,
				}
			}
		}
	}
	for i, t := range tasks {
		if c, ok := claimMap[t.TaskID]; ok {
			t.ActiveClaimID = c.claimID
			if t.ClaimedBy == "" {
				t.ClaimedBy = c.ai
			}
			if t.ClaimedAt == nil && !c.createdAt.IsZero() {
				t.ClaimedAt = &c.createdAt
			}
			if t.ClaimExpires == nil && !c.expiresAt.IsZero() {
				t.ClaimExpires = &c.expiresAt
			}
			final := strings.EqualFold(t.Status, "published") || strings.EqualFold(t.Status, "approved") || strings.EqualFold(t.Status, "completed")
			switch strings.ToLower(c.status) {
			case "submitted", "pending_review":
				if !final {
					t.Status = "submitted"
				}
			case "active":
				if !final && (t.Status == "" || strings.EqualFold(t.Status, "available") || strings.EqualFold(t.Status, "approved")) {
					t.Status = "claimed"
				}
			case "complete":
				t.Status = "approved"
			}
			tasks[i] = t
		}
	}
	return tasks
}

// UpdateSubmissionStatus updates the status of a submission and related entities.
func (s *PGStore) UpdateSubmissionStatus(ctx context.Context, submissionID, status, reviewerNotes, rejectionType string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Get claim_id from submission
	var claimID string
	err = tx.QueryRow(ctx, `SELECT claim_id FROM mcp_submissions WHERE submission_id=$1`, submissionID).Scan(&claimID)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") {
			return ErrClaimNotFound
		}
		return err
	}

	// Update submission status
	rejectionReason := strings.TrimSpace(reviewerNotes)
	rejectionType = strings.TrimSpace(rejectionType)
	var rejectedAt *time.Time
	if status == "rejected" {
		now := time.Now()
		rejectedAt = &now
	} else {
		rejectionReason = ""
		rejectionType = ""
	}
	if _, err := tx.Exec(ctx, `
UPDATE mcp_submissions
SET status=$2, rejection_reason=$3, rejection_type=$4, rejected_at=$5
WHERE submission_id=$1
`, submissionID, status, rejectionReason, rejectionType, rejectedAt); err != nil {
		return err
	}

	// On approval, update task and claim
	if status == "accepted" || status == "approved" {
		var taskID string
		err = tx.QueryRow(ctx, `SELECT task_id FROM mcp_claims WHERE claim_id=$1`, claimID).Scan(&taskID)
		if err != nil {
			return err // should not happen if submission existed
		}

		if _, err := tx.Exec(ctx, `UPDATE mcp_tasks SET status='approved' WHERE task_id=$1`, taskID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE mcp_claims SET status='complete' WHERE claim_id=$1`, claimID); err != nil {
			return err
		}
	}

	// On rejection, release claim and reset task status
	if status == "rejected" {
		var taskID string
		err = tx.QueryRow(ctx, `SELECT task_id FROM mcp_claims WHERE claim_id=$1`, claimID).Scan(&taskID)
		if err != nil {
			return err
		}

		if _, err := tx.Exec(ctx, `
UPDATE mcp_tasks
SET status='available', claimed_by=NULL, claimed_at=NULL, claim_expires_at=NULL
WHERE task_id=$1
`, taskID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE mcp_claims SET status='rejected' WHERE claim_id=$1`, claimID); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}
