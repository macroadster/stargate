package smart_contract

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"gorm.io/gorm"
	"stargate-backend/core/smart_contract"
	"stargate-backend/storage/gormdb"
)

// SQLStore is the unified durable MCP store (SQLite + Postgres) opened via gormdb.
// Business rules live in policy_*.go; this type owns persistence only.
type SQLStore struct {
	gdb      *gorm.DB
	db       *sql.DB // exposed for tests and transactional helpers
	claimTTL time.Duration
	dialect  gormdb.Dialect
}

// Legacy names — same concrete type.
type (
	SQLiteStore = SQLStore
	PGStore     = SQLStore
)

func parseSQLiteTime(raw string) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, raw); err == nil {
			return &t, nil
		}
	}
	return nil, fmt.Errorf("parse sqlite time %q", raw)
}

// rebind converts "?" placeholders to "$1,$2,..." for Postgres (database/sql + pgx).
func (s *SQLStore) rebind(q string) string {
	if s == nil || s.dialect != gormdb.DialectPostgres {
		return q
	}
	var b strings.Builder
	n := 0
	for i := 0; i < len(q); i++ {
		if q[i] == '?' {
			n++
			b.WriteByte('$')
			b.WriteString(fmt.Sprintf("%d", n))
			continue
		}
		b.WriteByte(q[i])
	}
	return b.String()
}

func (s *SQLStore) execContext(ctx context.Context, q string, args ...interface{}) (sql.Result, error) {
	q = s.normalizeInsert(q)
	return s.db.ExecContext(ctx, s.rebind(q), args...)
}

func (s *SQLStore) exec(q string, args ...interface{}) (sql.Result, error) {
	q = s.normalizeInsert(q)
	return s.db.Exec(s.rebind(q), args...)
}

func (s *SQLStore) queryContext(ctx context.Context, q string, args ...interface{}) (*sql.Rows, error) {
	return s.db.QueryContext(ctx, s.rebind(q), args...)
}

func (s *SQLStore) query(q string, args ...interface{}) (*sql.Rows, error) {
	return s.db.Query(s.rebind(q), args...)
}

func (s *SQLStore) queryRowContext(ctx context.Context, q string, args ...interface{}) *sql.Row {
	return s.db.QueryRowContext(ctx, s.rebind(q), args...)
}

func (s *SQLStore) queryRow(q string, args ...interface{}) *sql.Row {
	return s.db.QueryRow(s.rebind(q), args...)
}

func (s *SQLStore) isPostgres() bool { return s.dialect == gormdb.DialectPostgres }

// skillsExpr returns a SQL expression that yields comma-separated skills text.
func (s *SQLStore) skillsExpr(col string) string {
	if s.isPostgres() {
		return "COALESCE(array_to_string(" + col + ", ','), '')"
	}
	return "COALESCE(" + col + ", '')"
}

// NewSQLStore wraps a GORM DB, ensures MCP schema, optional seed.
func NewSQLStore(gdb *gorm.DB, claimTTL time.Duration, seed bool) (*SQLStore, error) {
	if gdb == nil {
		return nil, fmt.Errorf("nil gorm DB")
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		return nil, err
	}
	s := &SQLStore{gdb: gdb, db: sqlDB, claimTTL: claimTTL, dialect: gormdb.DialectOf(gdb)}
	if s.claimTTL <= 0 {
		s.claimTTL = time.Hour
	}
	if err := s.initSchema(context.Background()); err != nil {
		return nil, err
	}
	if seed {
		if err := s.seedFixtures(context.Background()); err != nil {
			log.Printf("seed fixtures warning: %v", err)
		}
	}
	return s, nil
}

// NewSQLiteStore opens SQLite via gormdb (pure-Go) and returns SQLStore.
func NewSQLiteStore(dbPath string, claimTTL time.Duration, seed bool) (*SQLStore, error) {
	gdb, err := gormdb.OpenSQLite(dbPath, gormdb.Config{
		MaxOpenConns: 20, MaxIdleConns: 10, ConnMaxLifetime: time.Hour,
	})
	if err != nil {
		return nil, err
	}
	s, err := NewSQLStore(gdb, claimTTL, seed)
	if err != nil {
		_ = gormdb.Close(gdb)
		return nil, err
	}
	return s, nil
}

// NewPGStore opens Postgres via gormdb and returns SQLStore.
func NewPGStore(ctx context.Context, dsn string, claimTTL time.Duration, seed bool) (*SQLStore, error) {
	_ = ctx
	gdb, err := gormdb.OpenPostgres(dsn, gormdb.Config{
		MaxOpenConns: 20, MaxIdleConns: 10, ConnMaxLifetime: time.Hour, ConnMaxIdleTime: 30 * time.Minute,
	})
	if err != nil {
		return nil, err
	}
	s, err := NewSQLStore(gdb, claimTTL, seed)
	if err != nil {
		_ = gormdb.Close(gdb)
		return nil, err
	}
	return s, nil
}

func (s *SQLStore) initSchema(ctx context.Context) error {
	name := "sqlite"
	if s.isPostgres() {
		name = "postgres"
	}
	return s.gdb.WithContext(ctx).Exec(GetMCPSchema(name)).Error
}

func (s *SQLStore) seedFixtures(ctx context.Context) error {
	var count int
	if err := s.queryRowContext(ctx, `SELECT COUNT(*) FROM mcp_tasks`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	contracts, tasks := SeedData()
	for _, c := range contracts {
		metadata, _ := json.Marshal(c.Metadata)
		skills := strings.Join(c.Skills, ",")
		if s.isPostgres() {
			// Insert skills as PG text array via string cast workaround: use '{}' empty + update — use simple text array literal
			skillsArr := pgTextArray(c.Skills)
			_, err := s.execContext(ctx, `
INSERT INTO mcp_contracts (contract_id, title, total_budget_sats, goals_count, available_tasks_count, status, skills, stego_image_url, metadata)
VALUES (?,?,?,?,?,?,?::text[],?,?,?::jsonb)
ON CONFLICT DO NOTHING
`, c.ContractID, c.Title, c.TotalBudgetSats, c.GoalsCount, c.AvailableTasksCount, c.Status, skillsArr, c.StegoImageURL, string(metadata))
			if err != nil {
				return err
			}
			continue
		}
		_, err := s.execContext(ctx, `
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
		if s.isPostgres() {
			_, err := s.execContext(ctx, `
INSERT INTO mcp_tasks (task_id, contract_id, goal_id, title, description, budget_sats, skills, status, difficulty, estimated_hours, requirements, merkle_proof)
VALUES (?,?,?,?,?,?,?::text[],?,?,?,?::jsonb,?::jsonb)
ON CONFLICT DO NOTHING
`, t.TaskID, t.ContractID, t.GoalID, t.Title, t.Description, t.BudgetSats, pgTextArray(t.Skills), t.Status, t.Difficulty, t.EstimatedHours, string(reqJSON), string(proofJSON))
			if err != nil {
				return err
			}
			continue
		}
		_, err := s.execContext(ctx, `
INSERT OR IGNORE INTO mcp_tasks (task_id, contract_id, goal_id, title, description, budget_sats, skills, status, difficulty, estimated_hours, requirements, merkle_proof)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?)
`, t.TaskID, t.ContractID, t.GoalID, t.Title, t.Description, t.BudgetSats, taskSkills, t.Status, t.Difficulty, t.EstimatedHours, string(reqJSON), string(proofJSON))
		if err != nil {
			return err
		}
	}
	return nil
}

func pgTextArray(skills []string) string {
	if len(skills) == 0 {
		return "{}"
	}
	parts := make([]string, len(skills))
	for i, sk := range skills {
		parts[i] = `"` + strings.ReplaceAll(sk, `"`, `\"`) + `"`
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func (s *SQLStore) Close() {
	if s.gdb != nil {
		_ = gormdb.Close(s.gdb)
		s.gdb = nil
		s.db = nil
	}
}


// Transaction helpers rebind placeholders for the store dialect.
func (s *SQLStore) txExec(tx *sql.Tx, ctx context.Context, q string, args ...interface{}) (sql.Result, error) {
	q = s.normalizeInsert(q)
	return tx.ExecContext(ctx, s.rebind(q), args...)
}
func (s *SQLStore) txQuery(tx *sql.Tx, ctx context.Context, q string, args ...interface{}) (*sql.Rows, error) {
	return tx.QueryContext(ctx, s.rebind(q), args...)
}
func (s *SQLStore) txQueryRow(tx *sql.Tx, ctx context.Context, q string, args ...interface{}) *sql.Row {
	return tx.QueryRowContext(ctx, s.rebind(q), args...)
}

func (s *SQLStore) containsSkill(all []string, skills []string) bool {
	for _, want := range skills {
		for _, have := range all {
			if strings.EqualFold(have, want) {
				return true
			}
		}
	}
	return len(skills) == 0
}

func (s *SQLStore) ListContracts(filter smart_contract.ContractFilter) ([]smart_contract.Contract, error) {
	baseSelect := `
SELECT c.contract_id, COALESCE(c.title, ''), COALESCE(c.total_budget_sats, 0), COALESCE(c.goals_count, 0),
	(SELECT COUNT(*) FROM mcp_tasks t WHERE t.contract_id = c.contract_id AND t.status = 'available') AS available_tasks_count,
	COALESCE(c.status, 'pending'), ` + s.skillsExpr("c.skills") + `, COALESCE(c.stego_image_url, ''), c.metadata, c.confirmed_block_height, c.confirmed_at, c.created_at
FROM mcp_contracts c
`

	whereConditions := []string{}
	args := []interface{}{}

	if len(filter.Statuses) > 0 {
		placeholders := make([]string, len(filter.Statuses))
		for i, st := range filter.Statuses {
			placeholders[i] = "?"
			args = append(args, st)
		}
		whereConditions = append(whereConditions, "c.status IN ("+strings.Join(placeholders, ",")+")")
	} else if filter.Status != "" {
		whereConditions = append(whereConditions, "c.status = ?")
		args = append(args, filter.Status)
	}

	// Exclude bare-hash confirmed rows when a wish-<hash> twin is also confirmed.
	// Page-local dedupe alone is not enough: collapsing one twin shrinks the page
	// below LIMIT and incorrectly ends infinite scroll on /contracts.
	whereConditions = append(whereConditions, `
NOT (
  lower(COALESCE(c.status, '')) = 'confirmed'
  AND c.contract_id NOT LIKE 'wish-%'
  AND EXISTS (
    SELECT 1 FROM mcp_contracts w
    WHERE w.contract_id = 'wish-' || c.contract_id
      AND lower(COALESCE(w.status, '')) = 'confirmed'
  )
)`)

	if filter.CursorHeight != nil && *filter.CursorHeight > 0 {
		op := "<"
		if strings.EqualFold(filter.CursorType, "after") {
			op = ">"
		}
		whereConditions = append(whereConditions, "c.confirmed_block_height "+op+" ?")
		args = append(args, *filter.CursorHeight)
	}

	// Cursor-based pagination by date (confirmed_at for confirmed lists, created_at for open).
	// Normalize mixed SQLite timestamp formats ("2026-07-19 05:31:53" vs RFC3339).
	if filter.CursorDate != nil {
		op := "<"
		if strings.EqualFold(filter.CursorType, "after") {
			op = ">"
		}
		dateCol := "c.confirmed_at"
		if filter.OrderByCreatedAt {
			dateCol = "c.created_at"
		}
		whereConditions = append(whereConditions,
			"datetime(replace(replace("+dateCol+",'T',' '),'Z','')) "+op+" datetime(?)")
		args = append(args, filter.CursorDate.UTC().Format("2006-01-02 15:04:05"))
	}

	whereClause := ""
	if len(whereConditions) > 0 {
		whereClause = "WHERE " + strings.Join(whereConditions, " AND ")
	}

	// Normalize text timestamps so mixed SQLite formats still sort chronologically.
	const createdAtOrder = "datetime(replace(replace(c.created_at,'T',' '),'Z',''))"
	const confirmedAtOrder = "datetime(replace(replace(c.confirmed_at,'T',' '),'Z',''))"
	orderBy := "ORDER BY c.confirmed_block_height DESC NULLS LAST, " + createdAtOrder + " DESC, c.contract_id DESC"
	if filter.OrderByCreatedAt {
		// Open/unconfirmed: newest created first, oldest at the bottom (scroll loads older).
		orderBy = "ORDER BY " + createdAtOrder + " DESC, c.contract_id DESC"
	} else if filter.OrderByConfirmedAt {
		orderBy = "ORDER BY " + confirmedAtOrder + " DESC NULLS FIRST, " + createdAtOrder + " DESC, c.contract_id DESC"
	}
	if filter.Limit > 0 {
		orderBy += fmt.Sprintf(" LIMIT %d", filter.Limit)
		if filter.Offset > 0 {
			orderBy += fmt.Sprintf(" OFFSET %d", filter.Offset)
		}
	} else if filter.Offset > 0 {
		orderBy += fmt.Sprintf(" OFFSET %d", filter.Offset)
	}

	query := baseSelect + " " + whereClause + " " + orderBy

	rows, err := s.queryContext(context.Background(), query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var allContracts []smart_contract.Contract
	for rows.Next() {
		var c smart_contract.Contract
		var metadata, skillsStr []byte
		var confirmedAtStr, createdAtStr sql.NullString
		if err := rows.Scan(&c.ContractID, &c.Title, &c.TotalBudgetSats, &c.GoalsCount, &c.AvailableTasksCount,
			&c.Status, &skillsStr, &c.StegoImageURL, &metadata, &c.ConfirmedBlockHeight, &confirmedAtStr, &createdAtStr); err != nil {
			return nil, err
		}
		if confirmedAtStr.Valid {
			if t, err := parseSQLiteTime(confirmedAtStr.String); err == nil {
				c.ConfirmedAt = t
			}
		}
		if createdAtStr.Valid {
			if t, err := parseSQLiteTime(createdAtStr.String); err == nil && t != nil {
				c.CreatedAt = *t
			}
		}
		if len(metadata) > 0 {
			_ = json.Unmarshal(metadata, &c.Metadata)
		}
		if len(skillsStr) > 0 {
			c.Skills = decodeSkillsCSV(string(skillsStr))
		}
		if len(filter.Skills) > 0 && !s.containsSkill(c.Skills, filter.Skills) {
			continue
		}
		allContracts = append(allContracts, c)
	}

	// Offset now handled in SQL (Cat 4.3). Removed post-processing slice that caused zero results when Offset >= Limit.

	return allContracts, nil
}

func (s *SQLStore) ListTasks(filter smart_contract.TaskFilter) ([]smart_contract.Task, error) {
	query := `
SELECT ` + s.taskSelectList() + `
FROM mcp_tasks
WHERE (? = '' OR status = ?)
AND (? = '' OR contract_id = ?)
AND (? = '' OR claimed_by = ?)
`
	args := []interface{}{filter.Status, filter.Status, filter.ContractID, filter.ContractID, filter.ClaimedBy, filter.ClaimedBy}

	rows, err := s.queryContext(context.Background(), query, args...)
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
	var claimedBy, claimedAtStr, claimExpiresAtStr sql.NullString
	err := rows.Scan(&t.TaskID, &t.ContractID, &t.GoalID, &t.Title, &t.Description, &t.BudgetSats,
		&skillsStr, &t.Status, &claimedBy, &claimedAtStr, &claimExpiresAtStr, &t.Difficulty,
		&t.EstimatedHours, &requirementsStr, &merkleProofStr)
	if err != nil {
		return t, err
	}
	if claimedBy.Valid {
		t.ClaimedBy = claimedBy.String
	}
	if claimedAtStr.Valid {
		if tm, err := parseSQLiteTime(claimedAtStr.String); err == nil {
			t.ClaimedAt = tm
		}
	}
	if claimExpiresAtStr.Valid {
		if tm, err := parseSQLiteTime(claimExpiresAtStr.String); err == nil {
			t.ClaimExpires = tm
		}
	}
	if len(skillsStr) > 0 {
		t.Skills = decodeSkillsCSV(string(skillsStr))
	}
	if len(requirementsStr) > 0 {
		_ = json.Unmarshal(requirementsStr, &t.Requirements)
	}
	if len(merkleProofStr) > 0 {
		var proof smart_contract.MerkleProof
		if err := json.Unmarshal(merkleProofStr, &proof); err == nil {
			t.MerkleProof = &proof
			if w := strings.TrimSpace(proof.ContractorWallet); w != "" {
				t.ContractorWallet = w
			}
		}
	}
	// Legacy sqlite claims wrote claimed_by but not contractor_wallet; fall back so payouts still resolve.
	if strings.TrimSpace(t.ContractorWallet) == "" && strings.TrimSpace(t.ClaimedBy) != "" {
		t.ContractorWallet = strings.TrimSpace(t.ClaimedBy)
		if t.MerkleProof == nil {
			t.MerkleProof = &smart_contract.MerkleProof{}
		}
		if strings.TrimSpace(t.MerkleProof.ContractorWallet) == "" {
			t.MerkleProof.ContractorWallet = t.ContractorWallet
		}
	}
	return t, nil
}

func (s *SQLStore) GetTask(id string) (smart_contract.Task, error) {
	// Prefer GORM when timestamps scan cleanly (Postgres TIMESTAMPTZ); fall back to
	// raw scan for SQLite TEXT timestamps.
	if s.isPostgres() {
		var row gormTaskRow
		err := s.gdb.Table(TableTasks).
			Select(s.taskSelectList()).
			Where("task_id = ?", id).
			Take(&row).Error
		if err == nil {
			return row.toTask(), nil
		}
	}
	row := s.queryRowContext(context.Background(), `
SELECT `+s.taskSelectList()+`
FROM mcp_tasks WHERE task_id=?
`, id)
	var t smart_contract.Task
	var skillsStr, requirementsStr, merkleProofStr []byte
	var claimedBy, claimedAtStr, claimExpiresAtStr sql.NullString
	err := row.Scan(&t.TaskID, &t.ContractID, &t.GoalID, &t.Title, &t.Description, &t.BudgetSats,
		&skillsStr, &t.Status, &claimedBy, &claimedAtStr, &claimExpiresAtStr, &t.Difficulty,
		&t.EstimatedHours, &requirementsStr, &merkleProofStr)
	if err != nil {
		return t, ErrTaskNotFound
	}
	if claimedBy.Valid {
		t.ClaimedBy = claimedBy.String
	}
	if claimedAtStr.Valid {
		if tm, err := parseSQLiteTime(claimedAtStr.String); err == nil {
			t.ClaimedAt = tm
		}
	}
	if claimExpiresAtStr.Valid {
		if tm, err := parseSQLiteTime(claimExpiresAtStr.String); err == nil {
			t.ClaimExpires = tm
		}
	}
	t.Skills = decodeSkillsCSV(string(skillsStr))
	if len(requirementsStr) > 0 {
		_ = json.Unmarshal(requirementsStr, &t.Requirements)
	}
	if len(merkleProofStr) > 0 {
		var proof smart_contract.MerkleProof
		if err := json.Unmarshal(merkleProofStr, &proof); err == nil {
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
	return t, nil
}

func (s *SQLStore) GetContract(id string) (smart_contract.Contract, error) {
	var c smart_contract.Contract
	var metadata, skillsStr []byte
	var confirmedAtStr sql.NullString
	err := s.queryRowContext(context.Background(), `
SELECT contract_id, COALESCE(title, ''), COALESCE(total_budget_sats, 0), COALESCE(goals_count, 0),
       (SELECT COUNT(*) FROM mcp_tasks t WHERE t.contract_id = mcp_contracts.contract_id AND t.status = 'available') AS available_tasks_count,
       COALESCE(status, 'pending'), `+s.skillsExpr("skills")+`, COALESCE(stego_image_url, ''), confirmed_block_height, confirmed_at, metadata
FROM mcp_contracts WHERE contract_id=?
`, id).Scan(&c.ContractID, &c.Title, &c.TotalBudgetSats, &c.GoalsCount, &c.AvailableTasksCount,
		&c.Status, &skillsStr, &c.StegoImageURL, &c.ConfirmedBlockHeight, &confirmedAtStr, &metadata)
	if err != nil {
		return c, fmt.Errorf("contract %s not found", id)
	}
	if confirmedAtStr.Valid {
		if t, err := parseSQLiteTime(confirmedAtStr.String); err == nil {
			c.ConfirmedAt = t
		}
	}
	if len(metadata) > 0 {
		_ = json.Unmarshal(metadata, &c.Metadata)
		// Hydrate rework requests from metadata (mirrors PG GetContract)
		if c.Metadata != nil {
			if reworkReqs, ok := c.Metadata["rework_requests"].([]interface{}); ok {
				for _, r := range reworkReqs {
					if rMap, ok := r.(map[string]interface{}); ok {
						req := smart_contract.ContractReworkRequest{}
						if id, ok := rMap["request_id"].(string); ok {
							req.RequestID = id
						}
						if contractID, ok := rMap["contract_id"].(string); ok {
							req.ContractID = contractID
						}
						if requester, ok := rMap["requester"].(string); ok {
							req.Requester = requester
						}
						if notes, ok := rMap["notes"].(string); ok {
							req.Notes = notes
						}
						if status, ok := rMap["status"].(string); ok {
							req.Status = status
						}
						if createdAt, ok := rMap["created_at"].(string); ok {
							if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
								req.CreatedAt = t
							}
						}
						if resolvedAt, ok := rMap["resolved_at"].(string); ok {
							if t, err := time.Parse(time.RFC3339, resolvedAt); err == nil {
								req.ResolvedAt = &t
							}
						}
						c.ReworkRequests = append(c.ReworkRequests, req)
					}
				}
			}
		}
	}
	if len(skillsStr) > 0 {
		c.Skills = decodeSkillsCSV(string(skillsStr))
	} else {
		// GORM/driver may return skills as plain string via Scan into []byte
		c.Skills = decodeSkillsCSV(string(skillsStr))
	}
	// Prefer shared rework parser when metadata present
	if c.Metadata != nil && len(c.ReworkRequests) == 0 {
		c.ReworkRequests = ParseReworkRequestsFromMetadata(c.Metadata)
	}
	return c, nil
}

func (s *SQLStore) GetClaim(id string) (smart_contract.Claim, error) {
	if s.isPostgres() {
		var row gormClaimRow
		if err := s.gdb.Table(TableClaims).Where("claim_id = ?", id).Take(&row).Error; err == nil {
			return row.toClaim(), nil
		}
	}
	var c smart_contract.Claim
	var createdAt, expiresAt sql.NullString
	err := s.queryRowContext(context.Background(), `
SELECT claim_id, task_id, ai_identifier, status, expires_at, created_at
FROM mcp_claims WHERE claim_id=?
`, id).Scan(&c.ClaimID, &c.TaskID, &c.AiIdentifier, &c.Status, &expiresAt, &createdAt)
	if err != nil {
		return c, ErrClaimNotFound
	}
	if expiresAt.Valid {
		if t, err := parseSQLiteTime(expiresAt.String); err == nil && t != nil {
			c.ExpiresAt = *t
		}
	}
	if createdAt.Valid {
		if t, err := parseSQLiteTime(createdAt.String); err == nil && t != nil {
			c.CreatedAt = *t
		}
	}
	return c, nil
}

func (s *SQLStore) ClaimTask(taskID, walletAddress string, estimatedCompletion *time.Time) (smart_contract.Claim, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return smart_contract.Claim{}, err
	}
	defer tx.Rollback()

	var taskStatus string
	var merkleProofStr []byte
	err = s.txQueryRow(tx, context.Background(), `SELECT status, COALESCE(merkle_proof, '') FROM mcp_tasks WHERE task_id=?`, taskID).Scan(&taskStatus, &merkleProofStr)
	if err != nil {
		return smart_contract.Claim{}, ErrTaskNotFound
	}

	var proof *smart_contract.MerkleProof
	if len(merkleProofStr) > 0 && string(merkleProofStr) != "" {
		var p smart_contract.MerkleProof
		if err := json.Unmarshal(merkleProofStr, &p); err == nil {
			proof = &p
		}
	}

	rows, err := s.txQuery(tx, context.Background(), `SELECT claim_id, task_id, ai_identifier, status, expires_at, created_at FROM mcp_claims WHERE task_id=?`, taskID)
	if err != nil {
		return smart_contract.Claim{}, err
	}
	var existing []smart_contract.Claim
	for rows.Next() {
		var c smart_contract.Claim
		var expiresStr, createdStr string
		if err := rows.Scan(&c.ClaimID, &c.TaskID, &c.AiIdentifier, &c.Status, &expiresStr, &createdStr); err != nil {
			rows.Close()
			return smart_contract.Claim{}, err
		}
		if t, perr := parseSQLiteTime(expiresStr); perr == nil && t != nil {
			c.ExpiresAt = *t
		}
		if t, perr := parseSQLiteTime(createdStr); perr == nil && t != nil {
			c.CreatedAt = *t
		}
		existing = append(existing, c)
	}
	rows.Close()

	now := time.Now()
	plan, err := DecideClaim(ClaimInput{
		TaskID:         taskID,
		TaskStatus:     taskStatus,
		Wallet:         walletAddress,
		ExistingClaims: existing,
		CurrentProof:   proof,
		ClaimTTL:       s.claimTTL,
		Now:            now,
		NewClaimID:     fmt.Sprintf("CLAIM-%d", now.UnixNano()),
	})
	if err != nil {
		return smart_contract.Claim{}, err
	}

	if plan.Action == ClaimActionCreate {
		_, err = s.txExec(tx, context.Background(), `
INSERT OR IGNORE INTO mcp_claims (claim_id, task_id, ai_identifier, status, expires_at, created_at)
VALUES (?,?,?,?,?,?)
`, plan.Claim.ClaimID, plan.Claim.TaskID, plan.Claim.AiIdentifier, plan.Claim.Status,
			plan.Claim.ExpiresAt.Format(time.RFC3339), plan.Claim.CreatedAt.Format(time.RFC3339))
		if err != nil {
			return smart_contract.Claim{}, err
		}
		claimedAt := plan.Claim.CreatedAt.Format(time.RFC3339)
		expiresAt := plan.Claim.ExpiresAt.Format(time.RFC3339)
		_, err = s.txExec(tx, context.Background(), `
UPDATE mcp_tasks SET status=?, claimed_by=?, claimed_at=?, claim_expires_at=? WHERE task_id=?
`, plan.TaskStatus, plan.ClaimedBy, claimedAt, expiresAt, taskID)
		if err != nil {
			return smart_contract.Claim{}, err
		}
	} else {
		_, _ = s.txExec(tx, context.Background(), `UPDATE mcp_tasks SET status=?, claimed_by=? WHERE task_id=?`, plan.TaskStatus, plan.ClaimedBy, taskID)
	}

	if plan.UpdateProof && plan.Proof != nil {
		proofJSON, err := json.Marshal(plan.Proof)
		if err != nil {
			return smart_contract.Claim{}, err
		}
		if _, err := s.txExec(tx, context.Background(), `UPDATE mcp_tasks SET merkle_proof=? WHERE task_id=?`, string(proofJSON), taskID); err != nil {
			return smart_contract.Claim{}, err
		}
	}

	_ = estimatedCompletion
	if err := tx.Commit(); err != nil {
		return smart_contract.Claim{}, err
	}
	return plan.Claim, nil
}

func (s *SQLStore) SubmitWork(claimID string, deliverables map[string]interface{}, proof map[string]interface{}) (smart_contract.Submission, error) {
	var claim smart_contract.Claim
	var expiresAt, createdAt sql.NullString
	err := s.queryRowContext(context.Background(), `SELECT claim_id, task_id, ai_identifier, status, expires_at, created_at FROM mcp_claims WHERE claim_id=?`, claimID).
		Scan(&claim.ClaimID, &claim.TaskID, &claim.AiIdentifier, &claim.Status, &expiresAt, &createdAt)
	if err != nil {
		return smart_contract.Submission{}, ErrClaimNotFound
	}
	if createdAt.Valid {
		if t, err := parseSQLiteTime(createdAt.String); err == nil && t != nil {
			claim.CreatedAt = *t
		}
	}
	if expiresAt.Valid {
		if t, err := parseSQLiteTime(expiresAt.String); err == nil && t != nil {
			claim.ExpiresAt = *t
		}
	}

	rows, err := s.query(`SELECT status FROM mcp_submissions WHERE claim_id=?`, claimID)
	if err != nil {
		return smart_contract.Submission{}, err
	}
	var statuses []string
	for rows.Next() {
		var st string
		if err := rows.Scan(&st); err != nil {
			rows.Close()
			return smart_contract.Submission{}, err
		}
		statuses = append(statuses, st)
	}
	rows.Close()

	now := time.Now()
	plan, err := DecideSubmit(SubmitInput{
		Claim:                      claim,
		ExistingSubmissionStatuses: statuses,
		Deliverables:               deliverables,
		Proof:                      proof,
		Now:                        now,
		NewSubmissionID:            fmt.Sprintf("SUB-%d", now.UnixNano()),
	})
	if plan.MarkClaimExpired {
		_, _ = s.exec(`UPDATE mcp_claims SET status='expired' WHERE claim_id=?`, claimID)
	}
	if err != nil {
		return smart_contract.Submission{}, err
	}

	delivJSON, _ := json.Marshal(plan.Submission.Deliverables)
	proofJSON, _ := json.Marshal(plan.Submission.CompletionProof)
	_, err = s.exec(`
INSERT OR IGNORE INTO mcp_submissions (submission_id, claim_id, task_id, status, deliverables, completion_proof, created_at)
VALUES (?,?,?,?,?,?,?)
`, plan.Submission.SubmissionID, plan.Submission.ClaimID, plan.Submission.TaskID, plan.Submission.Status,
		string(delivJSON), string(proofJSON), plan.Submission.CreatedAt.Format(time.RFC3339))
	if err != nil {
		return smart_contract.Submission{}, err
	}

	_, _ = s.exec(`UPDATE mcp_claims SET status='submitted' WHERE claim_id=?`, claimID)
	_, _ = s.exec(`UPDATE mcp_tasks SET status='submitted' WHERE task_id=?`, claim.TaskID)

	return plan.Submission, nil
}

func (s *SQLStore) ListSubmissions(ctx context.Context, taskIDs []string) ([]smart_contract.Submission, error) {
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

	rows, err := s.queryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []smart_contract.Submission
	for rows.Next() {
		var sub smart_contract.Submission
		var delivJSON, proofJSON, rejectionReason, rejectionType []byte
		var rejectedAtStr, createdAtStr sql.NullString
		if err := rows.Scan(&sub.SubmissionID, &sub.ClaimID, &sub.TaskID, &sub.Status, &delivJSON, &proofJSON, &rejectionReason, &rejectionType, &rejectedAtStr, &createdAtStr); err != nil {
			return nil, err
		}
		if rejectedAtStr.Valid {
			if t, err := parseSQLiteTime(rejectedAtStr.String); err == nil {
				sub.RejectedAt = t
			}
		}
		if createdAtStr.Valid {
			if t, err := parseSQLiteTime(createdAtStr.String); err == nil && t != nil {
				sub.CreatedAt = *t
			}
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

func (s *SQLStore) GetSubmission(ctx context.Context, id string) (smart_contract.Submission, error) {
	rows, err := s.queryContext(ctx, `
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
		var rejectedAtStr, createdAtStr sql.NullString
		if err := rows.Scan(&sub.SubmissionID, &sub.ClaimID, &sub.TaskID, &sub.Status, &delivJSON, &proofJSON, &sub.RejectionReason, &sub.RejectionType, &rejectedAtStr, &createdAtStr); err != nil {
			return smart_contract.Submission{}, err
		}
		if rejectedAtStr.Valid {
			if t, err := parseSQLiteTime(rejectedAtStr.String); err == nil {
				sub.RejectedAt = t
			}
		}
		if createdAtStr.Valid {
			if t, err := parseSQLiteTime(createdAtStr.String); err == nil && t != nil {
				sub.CreatedAt = *t
			}
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

func (s *SQLStore) TaskStatus(taskID string) (map[string]interface{}, error) {
	task, err := s.GetTask(taskID)
	if err != nil {
		return nil, err
	}

	var claim smart_contract.Claim
	var expiresAt, createdAt sql.NullString
	err = s.queryRowContext(context.Background(), `
SELECT claim_id, task_id, ai_identifier, status, expires_at, created_at
FROM mcp_claims
WHERE task_id=? AND status IN ('active','submitted','pending_review')
ORDER BY created_at DESC
LIMIT 1
`, taskID).Scan(&claim.ClaimID, &claim.TaskID, &claim.AiIdentifier, &claim.Status, &expiresAt, &createdAt)
	if err != nil {
		claim = smart_contract.Claim{}
	} else {
		if expiresAt.Valid {
			if t, err := parseSQLiteTime(expiresAt.String); err == nil && t != nil {
				claim.ExpiresAt = *t
			}
		}
		if createdAt.Valid {
			if t, err := parseSQLiteTime(createdAt.String); err == nil && t != nil {
				claim.CreatedAt = *t
			}
		}
	}

	resp := map[string]interface{}{
		"task_id":           task.TaskID,
		"status":            task.Status,
		"claimed_by":        task.ClaimedBy,
		"claim_expires_at":  task.ClaimExpires,
		"claimed_at":        task.ClaimedAt,
		"time_remaining_hr": nil,
	}

	// Compute submission attempts (unified with MemoryStore for Cat 4.2)
	var submissionAttempts int
	_ = s.queryRowContext(context.Background(), `
		SELECT COUNT(*) FROM mcp_submissions s
		JOIN mcp_claims c ON c.claim_id = s.claim_id
		WHERE c.task_id = ?
	`, taskID).Scan(&submissionAttempts)
	resp["submission_attempts"] = submissionAttempts

	if claim.ClaimID != "" {
		remaining := time.Until(claim.ExpiresAt).Hours()
		resp["time_remaining_hr"] = remaining
		resp["claim_id"] = claim.ClaimID

		// Status override logic extracted/shared in spirit (Cat 4.2). Match MemoryStore behavior.
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
	}
	return resp, nil
}

func (s *SQLStore) GetTaskProof(taskID string) (*smart_contract.MerkleProof, error) {
	task, err := s.GetTask(taskID)
	if err != nil {
		return nil, err
	}
	return task.MerkleProof, nil
}

func (s *SQLStore) ContractFunding(contractID string) (smart_contract.Contract, []smart_contract.MerkleProof, error) {
	contract, err := s.GetContract(contractID)
	if err != nil {
		return smart_contract.Contract{}, nil, err
	}
	rows, err := s.queryContext(context.Background(), `SELECT merkle_proof FROM mcp_tasks WHERE contract_id=?`, contractID)
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

func (s *SQLStore) UpsertContractWithTasks(ctx context.Context, contract smart_contract.Contract, tasks []smart_contract.Task) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	metadata, _ := json.Marshal(contract.Metadata)
	skills := s.encodeSkills(contract.Skills)
	createdAt := time.Now().Format(time.RFC3339)
	if !contract.CreatedAt.IsZero() {
		createdAt = contract.CreatedAt.Format(time.RFC3339)
	}

	// Protected statuses (confirmed/completed/superseded) keep title, budget,
	// and status; stego URL / skills / task counts may still refresh.
	_, err = s.txExec(tx, ctx, `
INSERT INTO mcp_contracts (contract_id, title, total_budget_sats, goals_count, available_tasks_count, status, skills, stego_image_url, created_at, metadata)
VALUES (?,?,?,?,?,?,`+s.skillsBind()+`,?,?,`+s.jsonBind()+`)
ON CONFLICT(contract_id) DO UPDATE SET
  title = CASE WHEN lower(mcp_contracts.status) IN ('confirmed','completed','superseded') THEN mcp_contracts.title ELSE excluded.title END,
  total_budget_sats = CASE WHEN lower(mcp_contracts.status) IN ('confirmed','completed','superseded') THEN mcp_contracts.total_budget_sats ELSE excluded.total_budget_sats END,
  goals_count = CASE WHEN lower(mcp_contracts.status) IN ('confirmed','completed','superseded') THEN mcp_contracts.goals_count ELSE excluded.goals_count END,
  available_tasks_count = excluded.available_tasks_count,
  status = CASE WHEN lower(mcp_contracts.status) IN ('confirmed','completed','superseded') THEN mcp_contracts.status ELSE excluded.status END,
  skills = CASE WHEN excluded.skills IS NOT NULL THEN excluded.skills ELSE mcp_contracts.skills END,
  stego_image_url = CASE WHEN excluded.stego_image_url IS NOT NULL AND excluded.stego_image_url <> '' THEN excluded.stego_image_url ELSE mcp_contracts.stego_image_url END
`, contract.ContractID, contract.Title, contract.TotalBudgetSats, contract.GoalsCount, contract.AvailableTasksCount, contract.Status, skills, contract.StegoImageURL, createdAt, string(metadata))
	if err != nil {
		return err
	}

	for _, t := range tasks {
		reqJSON, _ := json.Marshal(t.Requirements)
		var proofStr *string
		if t.MerkleProof != nil {
			proofJSON, _ := json.Marshal(t.MerkleProof)
			ps := string(proofJSON)
			proofStr = &ps
		}
		taskSkills := s.encodeSkills(t.Skills)
		_, err := s.txExec(tx, ctx, `
INSERT INTO mcp_tasks (task_id, contract_id, goal_id, title, description, budget_sats, skills, status, claimed_by, claimed_at, claim_expires_at, difficulty, estimated_hours, requirements, merkle_proof)
VALUES (?,?,?,?,?,?,`+s.skillsBind()+`,?,?,?,?,?,?,`+s.jsonBind()+`,`+s.jsonBind()+`)
ON CONFLICT(task_id) DO UPDATE SET
  contract_id = excluded.contract_id,
  goal_id = excluded.goal_id,
  title = excluded.title,
  description = excluded.description,
  budget_sats = excluded.budget_sats,
  skills = excluded.skills,
  status = CASE
    WHEN excluded.status = 'available' THEN 'available'
    ELSE mcp_tasks.status
  END,
  claimed_by = COALESCE(excluded.claimed_by, mcp_tasks.claimed_by),
  claimed_at = COALESCE(excluded.claimed_at, mcp_tasks.claimed_at),
  claim_expires_at = COALESCE(excluded.claim_expires_at, mcp_tasks.claim_expires_at),
  difficulty = excluded.difficulty,
  estimated_hours = excluded.estimated_hours,
  requirements = excluded.requirements,
  merkle_proof = COALESCE(excluded.merkle_proof, mcp_tasks.merkle_proof)
`, t.TaskID, t.ContractID, t.GoalID, t.Title, t.Description, t.BudgetSats, taskSkills, t.Status, t.ClaimedBy, t.ClaimedAt, t.ClaimExpires, t.Difficulty, t.EstimatedHours, string(reqJSON), proofStr)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *SQLStore) UpdateTaskProof(ctx context.Context, taskID string, proof *smart_contract.MerkleProof) error {
	if proof == nil {
		return nil
	}
	// Preserve contractor wallet set at claim time when callers rewrite provisional proofs.
	if existing, err := s.GetTaskProof(taskID); err == nil && existing != nil {
		if strings.TrimSpace(existing.ContractorWallet) != "" && strings.TrimSpace(proof.ContractorWallet) == "" {
			proof.ContractorWallet = strings.TrimSpace(existing.ContractorWallet)
		}
	}
	b, err := json.Marshal(proof)
	if err != nil {
		return err
	}
	_, err = s.execContext(ctx, `UPDATE mcp_tasks SET merkle_proof=? WHERE task_id=?`, string(b), taskID)
	return err
}

func (s *SQLStore) UpdateContractStatus(ctx context.Context, contractID, status string) error {
	contractID = strings.TrimSpace(contractID)
	status = strings.TrimSpace(status)
	if contractID == "" || status == "" {
		return nil
	}
	_, err := s.execContext(ctx, `UPDATE mcp_contracts SET status=? WHERE contract_id=?`, status, contractID)
	return err
}

func (s *SQLStore) ConfirmContract(ctx context.Context, contractID string, blockHeight int, txid string) error {
	contractID = strings.TrimSpace(contractID)
	if contractID == "" {
		return nil
	}

	apply := BuildConfirmApply(contractID, blockHeight, "")
	plan := apply.Plan
	normalized := apply.Normalized
	wishID := apply.WishID
	stegoImageURL := apply.StegoImageURL
	_ = apply // keep for alias fields below

	confirmRow := func(id string) (bool, error) {
		var existingMeta []byte
		_ = s.queryRowContext(ctx, `SELECT COALESCE(metadata, '{}') FROM mcp_contracts WHERE contract_id=?`, id).Scan(&existingMeta)
		meta := map[string]interface{}{}
		if len(existingMeta) > 0 {
			_ = json.Unmarshal(existingMeta, &meta)
		}
		meta = MergeConfirmMetadata(meta, txid, blockHeight)
		updatedMeta, _ := json.Marshal(meta)

		res, err := s.execContext(ctx, `
UPDATE mcp_contracts
SET status='confirmed', confirmed_block_height=?, confirmed_at=CURRENT_TIMESTAMP,
    stego_image_url=COALESCE(?, stego_image_url),
    metadata=?
WHERE contract_id=?
`, blockHeight, stegoImageURL, string(updatedMeta), id)
		if err != nil {
			return false, err
		}
		n, _ := res.RowsAffected()
		return n > 0, nil
	}

	// Prefer canonical wish-<hash> for pixel hashes so ensureMatchedContract +
	// markIngestionConfirmed + stego reconcile all land on one row.
	var confirmedID string
	for _, id := range plan.ConfirmTryOrder(contractID) {
		ok, err := confirmRow(id)
		if err != nil {
			return err
		}
		if ok {
			confirmedID = id
			break
		}
	}

	// Peer bootstrap: only the wish row exists under the other id form.
	// Confirm in place onto the canonical wish id (never mint a second confirmed row).
	if confirmedID == "" && plan.IsPixelHash {
		sourceID := ""
		for _, candidate := range []string{wishID, normalized, contractID} {
			var n int
			_ = s.queryRowContext(ctx, `SELECT COUNT(1) FROM mcp_contracts WHERE contract_id=?`, candidate).Scan(&n)
			if n > 0 {
				sourceID = candidate
				break
			}
		}
		if sourceID != "" && sourceID != wishID {
			// Copy source → canonical wish id, then confirm wish (dialect-safe upsert).
			if err := s.bootstrapConfirmContract(ctx, sourceID, wishID, blockHeight, txid, stegoImageURL); err != nil {
				return err
			}
			confirmedID = wishID
		} else if sourceID == wishID {
			// Should have been caught by confirmRow; retry once.
			if ok, err := confirmRow(wishID); err != nil {
				return err
			} else if ok {
				confirmedID = wishID
			}
		}
	}

	// Collapse dual IDs: after confirm, force-supersede bare-hash aliases even if
	// they were already confirmed. ConfirmContract is the trusted chain path —
	// untrusted stego/IPFS demotion still uses supersedeEligibleStatusSQL elsewhere.
	if plan.IsPixelHash {
		var wishStatus string
		_ = s.queryRowContext(ctx, `SELECT COALESCE(status,'') FROM mcp_contracts WHERE contract_id=?`, wishID).Scan(&wishStatus)
		if strings.EqualFold(strings.TrimSpace(wishStatus), "confirmed") {
			for _, alias := range plan.Aliases {
				// Force supersede any twin row so /contracts cannot list both.
				_, _ = s.execContext(ctx, `UPDATE mcp_contracts SET status='superseded' WHERE contract_id=? AND lower(status)<>'superseded'`, alias)
			}
		}
	} else {
		// Non-pixel: historical supersede of wish- twin if present.
		_, _ = s.execContext(ctx, `UPDATE mcp_contracts SET status='superseded' WHERE contract_id=? AND `+supersedeEligibleStatusSQL, wishID)
	}

	// Update matching proposals to confirmed (dialect-safe JSON extraction).
	cidExpr := s.jsonTextExpr("metadata", "contract_id")
	ingExpr := s.jsonTextExpr("metadata", "ingestion_id")
	vphExpr := s.jsonTextExpr("metadata", "visible_pixel_hash")
	_, err := s.execContext(ctx, `
UPDATE mcp_proposals SET status='confirmed'
WHERE status='approved' AND (
  `+cidExpr+` IN (?, ?) OR
  `+ingExpr+` IN (?, ?) OR
  `+vphExpr+` IN (?, ?) OR
  id IN (?, ?)
)`, normalized, wishID, normalized, wishID, normalized, wishID, normalized, wishID)
	return err
}

// bootstrapConfirmContract copies a bare/source contract row onto the canonical wish id
// and remaps tasks. Works for SQLite and Postgres (skills + metadata casts).
func (s *SQLStore) bootstrapConfirmContract(ctx context.Context, sourceID, wishID string, blockHeight int, txid, stegoImageURL string) error {
	var title string
	var budget int64
	var goals, avail int
	var skillsCSV string
	var srcMeta []byte
	err := s.queryRowContext(ctx, `
SELECT COALESCE(title, ''), COALESCE(total_budget_sats, 0), COALESCE(goals_count, 0), COALESCE(available_tasks_count, 0),
       `+s.skillsExpr("skills")+`, COALESCE(metadata, '{}')
FROM mcp_contracts WHERE contract_id=?`, sourceID).Scan(&title, &budget, &goals, &avail, &skillsCSV, &srcMeta)
	if err != nil {
		return err
	}
	metaMap := map[string]interface{}{}
	if len(srcMeta) > 0 {
		_ = json.Unmarshal(srcMeta, &metaMap)
	}
	metaMap = MergeConfirmMetadata(metaMap, txid, blockHeight)
	mergedMeta, _ := json.Marshal(metaMap)
	skillsEnc := s.encodeSkills(decodeSkillsCSV(skillsCSV))

	_, err = s.execContext(ctx, `
INSERT INTO mcp_contracts (contract_id, title, total_budget_sats, goals_count, available_tasks_count, status, skills, stego_image_url, confirmed_block_height, confirmed_at, created_at, metadata)
VALUES (?,?,?,?,?,'confirmed',`+s.skillsBind()+`,?,?,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,`+s.jsonBind()+`)
ON CONFLICT(contract_id) DO UPDATE SET
  status='confirmed', confirmed_block_height=excluded.confirmed_block_height,
  confirmed_at=CURRENT_TIMESTAMP,
  stego_image_url=COALESCE(excluded.stego_image_url, mcp_contracts.stego_image_url),
  metadata=excluded.metadata
`, wishID, title, budget, goals, avail, skillsEnc, stegoImageURL, blockHeight, string(mergedMeta))
	if err != nil {
		return err
	}

	// Remap tasks onto wish contract id (shared SQL; normalizeInsert handles PG ignore).
	_, err = s.execContext(ctx, `
INSERT OR IGNORE INTO mcp_tasks (task_id, contract_id, goal_id, title, description, budget_sats, skills, status, claimed_by, claimed_at, claim_expires_at, difficulty, estimated_hours, requirements, merkle_proof)
SELECT replace(task_id, ?, ?) AS task_id, ? AS contract_id, goal_id, title, description, budget_sats, skills, status, claimed_by, claimed_at, claim_expires_at, difficulty, estimated_hours, requirements, merkle_proof
FROM mcp_tasks WHERE contract_id=?
`, sourceID, wishID, wishID, sourceID)
	return err
}

func (s *SQLStore) SyncClaim(ctx context.Context, claim smart_contract.Claim) error {
	_, err := s.execContext(ctx, `
INSERT OR IGNORE INTO mcp_claims (claim_id, task_id, ai_identifier, status, expires_at, created_at)
VALUES (?,?,?,?,?,?)
ON CONFLICT(claim_id) DO UPDATE SET
  status = excluded.status,
  expires_at = excluded.expires_at
`, claim.ClaimID, claim.TaskID, claim.AiIdentifier, claim.Status, claim.ExpiresAt.Format(time.RFC3339), claim.CreatedAt.Format(time.RFC3339))
	return err
}

func (s *SQLStore) SyncSubmission(ctx context.Context, sub smart_contract.Submission) error {
	// Ensure parent chain exists so FK constraints are satisfied.
	// During cross-node sync, submissions may arrive before their claim/task/contract.
	if sub.TaskID != "" {
		s.execContext(ctx, `INSERT OR IGNORE INTO mcp_contracts (contract_id, status, created_at) VALUES (?, 'pending', CURRENT_TIMESTAMP)`, sub.TaskID)
		s.execContext(ctx, `INSERT OR IGNORE INTO mcp_tasks (task_id, contract_id, status) VALUES (?, ?, 'available')`, sub.TaskID, sub.TaskID)
	}
	if sub.ClaimID != "" {
		taskID := sub.TaskID
		if taskID == "" {
			taskID = sub.ClaimID
		}
		s.execContext(ctx, `INSERT OR IGNORE INTO mcp_claims (claim_id, task_id, status, created_at) VALUES (?, ?, 'active', CURRENT_TIMESTAMP)`, sub.ClaimID, taskID)
	}

	if sub.Deliverables != nil {
		delete(sub.Deliverables, "status")
	}
	if sub.CompletionProof != nil {
		delete(sub.CompletionProof, "status")
	}

	delivJSON, _ := json.Marshal(sub.Deliverables)
	proofJSON, _ := json.Marshal(sub.CompletionProof)
	_, err := s.execContext(ctx, `
INSERT OR IGNORE INTO mcp_submissions (submission_id, claim_id, task_id, status, deliverables, completion_proof, rejection_reason, rejection_type, rejected_at, created_at)
VALUES (?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(submission_id) DO UPDATE SET
  status = excluded.status,
  deliverables = excluded.deliverables,
  completion_proof = excluded.completion_proof,
  task_id = excluded.task_id
`, sub.SubmissionID, sub.ClaimID, sub.TaskID, sub.Status, string(delivJSON), string(proofJSON), sub.RejectionReason, sub.RejectionType, sub.RejectedAt, sub.CreatedAt.Format(time.RFC3339))
	return err
}

func (s *SQLStore) UpsertTask(ctx context.Context, t smart_contract.Task) error {
	// Ensure parent contract exists so the FK constraint is satisfied.
	// During cross-node sync, tasks may arrive before their contracts.
	if t.ContractID != "" {
		s.execContext(ctx, `INSERT OR IGNORE INTO mcp_contracts (contract_id, status, created_at) VALUES (?, 'pending', CURRENT_TIMESTAMP)`, t.ContractID)
	}
	reqJSON, _ := json.Marshal(t.Requirements)
	var proofJSON []byte
	if t.MerkleProof != nil {
		proofJSON, _ = json.Marshal(t.MerkleProof)
	}
	taskSkills := strings.Join(t.Skills, ",")
	_, err := s.execContext(ctx, `
INSERT OR IGNORE INTO mcp_tasks (task_id, contract_id, goal_id, title, description, budget_sats, skills, status, claimed_by, claimed_at, claim_expires_at, difficulty, estimated_hours, requirements, merkle_proof)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(task_id) DO UPDATE SET
  contract_id = excluded.contract_id,
  goal_id = excluded.goal_id,
  title = excluded.title,
  description = excluded.description,
  budget_sats = excluded.budget_sats,
  skills = excluded.skills,
  status = excluded.status,
  claimed_by = COALESCE(excluded.claimed_by, mcp_tasks.claimed_by),
  claimed_at = COALESCE(excluded.claimed_at, mcp_tasks.claimed_at),
  claim_expires_at = COALESCE(excluded.claim_expires_at, mcp_tasks.claim_expires_at),
  difficulty = excluded.difficulty,
  estimated_hours = excluded.estimated_hours,
  requirements = excluded.requirements,
  merkle_proof = COALESCE(excluded.merkle_proof, mcp_tasks.merkle_proof)
`, t.TaskID, t.ContractID, t.GoalID, t.Title, t.Description, t.BudgetSats, taskSkills, t.Status, t.ClaimedBy, t.ClaimedAt, t.ClaimExpires, t.Difficulty, t.EstimatedHours, string(reqJSON), string(proofJSON))
	return err
}

func (s *SQLStore) SyncEscortStatus(ctx context.Context, status smart_contract.EscortStatus) error {
	payload, _ := json.Marshal(status)
	_, err := s.execContext(ctx, `
INSERT OR IGNORE INTO mcp_escort_status (task_id, proof_status, last_checked, payload)
VALUES (?,?,?,?)
ON CONFLICT(task_id) DO UPDATE SET
  proof_status = excluded.proof_status,
  last_checked = excluded.last_checked,
  payload = excluded.payload
`, status.TaskID, status.ProofStatus, status.LastChecked.Format(time.RFC3339), string(payload))
	return err
}

func (s *SQLStore) CreateProposal(ctx context.Context, p smart_contract.Proposal) error {
	visibleHash, metadata, wishToSupersede, err := PrepareProposalForCreate(&p)
	if err != nil {
		return err
	}
	if visibleHash != "" {
		var conflictID string
		err := s.queryRowContext(ctx, `
		SELECT id FROM mcp_proposals
		WHERE visible_pixel_hash=? AND id<>?
		AND status IN ('approved','published')
		LIMIT 1
		`, visibleHash, p.ID).Scan(&conflictID)
		if err == nil && conflictID != "" {
			return ProposalConflictApprovedMsg(visibleHash, conflictID)
		}
		var count int
		err = s.queryRowContext(ctx, `
		SELECT COUNT(*) FROM mcp_proposals
		WHERE visible_pixel_hash=? AND id<>?
		`, visibleHash, p.ID).Scan(&count)
		if err == nil && count >= MaxProposalsPerWish {
			return ProposalMaxPerWishMsg(visibleHash)
		}
	}
	_, err = s.execContext(ctx, `
INSERT OR IGNORE INTO mcp_proposals (id, title, description_md, visible_pixel_hash, budget_sats, status, metadata, created_at)
VALUES (?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET
  status = excluded.status,
  metadata = excluded.metadata,
  title = excluded.title,
  description_md = excluded.description_md,
  visible_pixel_hash = excluded.visible_pixel_hash,
  budget_sats = excluded.budget_sats
`, p.ID, p.Title, p.DescriptionMD, p.VisiblePixelHash, p.BudgetSats, p.Status, string(metadata), p.CreatedAt.Format(time.RFC3339))
	if err != nil {
		return err
	}
	if wishToSupersede != "" {
		// Never demote confirmed/completed wishes via proposal create (stego/IPFS attack surface).
		_, _ = s.execContext(ctx, `UPDATE mcp_contracts SET status='superseded' WHERE contract_id=? AND `+supersedeEligibleStatusSQL, wishToSupersede)
	}
	return nil
}

func (s *SQLStore) ListProposals(ctx context.Context, filter smart_contract.ProposalFilter) ([]smart_contract.Proposal, error) {
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

	// Only apply SQL LIMIT when no Go-side filters (ContractID, MinBudget, Skills)
	// will further reduce rows.  When those filters are active the SQL LIMIT would
	// silently discard matching rows before Go can see them (PG store already works
	// this way — no SQL LIMIT, Go applies MaxResults at the end).
	hasGoFilter := filter.ContractID != "" || filter.MinBudget > 0 || len(filter.Skills) > 0
	if filter.MaxResults > 0 && !hasGoFilter {
		query += fmt.Sprintf(" LIMIT %d", filter.MaxResults)
	}

	rows, err := s.queryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []smart_contract.Proposal
	for rows.Next() {
		var p smart_contract.Proposal
		var metadata []byte
		var createdAtStr sql.NullString
		if err := rows.Scan(&p.ID, &p.Title, &p.DescriptionMD, &p.VisiblePixelHash, &p.BudgetSats, &p.Status, &metadata, &createdAtStr); err != nil {
			return nil, err
		}
		if createdAtStr.Valid {
			if t, err := parseSQLiteTime(createdAtStr.String); err == nil && t != nil {
				p.CreatedAt = *t
			}
		}
		if len(metadata) > 0 {
			_ = json.Unmarshal(metadata, &p.Metadata)
		}
		populateProposalTasks(&p)
		s.hydrateProposalTasks(ctx, &p)

		// Filter by ContractID (mirrors PG: match against multiple metadata fields).
		// Normalize wish- prefix: if the filter is "wish-abc" also match "abc" and vice versa.
		if filter.ContractID != "" {
			var candidates []string
			if v, ok := p.Metadata["contract_id"].(string); ok {
				candidates = append(candidates, v)
			}
			if v, ok := p.Metadata["ingestion_id"].(string); ok {
				candidates = append(candidates, v)
			}
			if v, ok := p.Metadata["visible_pixel_hash"].(string); ok {
				candidates = append(candidates, v)
			}
			candidates = append(candidates, p.VisiblePixelHash, p.ID)

			// Build normalized search terms: both with and without wish- prefix
			filterNorm := strings.TrimSpace(filter.ContractID)
			filterBare := strings.TrimPrefix(filterNorm, "wish-")
			filterWish := "wish-" + filterBare

			match := false
			for _, candidate := range candidates {
				c := strings.TrimSpace(candidate)
				if c == filterNorm || c == filterBare || c == filterWish {
					match = true
					break
				}
			}
			if !match {
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

func (s *SQLStore) GetProposal(ctx context.Context, id string) (smart_contract.Proposal, error) {
	var p smart_contract.Proposal
	var metadata []byte
	var createdAtStr sql.NullString
	err := s.queryRowContext(ctx, `
SELECT id, title, description_md, visible_pixel_hash, budget_sats, status, metadata, created_at
FROM mcp_proposals WHERE id=?
`, id).Scan(&p.ID, &p.Title, &p.DescriptionMD, &p.VisiblePixelHash, &p.BudgetSats, &p.Status, &metadata, &createdAtStr)
	if err != nil {
		return smart_contract.Proposal{}, fmt.Errorf("proposal %s not found", id)
	}
	if createdAtStr.Valid {
		if t, err := parseSQLiteTime(createdAtStr.String); err == nil && t != nil {
			p.CreatedAt = *t
		}
	}
	if len(metadata) > 0 {
		_ = json.Unmarshal(metadata, &p.Metadata)
	}
	populateProposalTasks(&p)
	s.hydrateProposalTasks(ctx, &p)
	return p, nil
}

func (s *SQLStore) UpdateProposal(ctx context.Context, p smart_contract.Proposal) error {
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
	if p.Status == "" {
		p.Status = existing.Status
	}

	metadata, _ := json.Marshal(p.Metadata)
	_, err = s.execContext(ctx, `
UPDATE mcp_proposals SET title=?, description_md=?, budget_sats=?, status=?, metadata=? WHERE id=?
`, p.Title, p.DescriptionMD, p.BudgetSats, p.Status, string(metadata), p.ID)
	return err
}

func (s *SQLStore) UpdateProposalMetadata(ctx context.Context, id string, updates map[string]interface{}) error {
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
	_, err = s.execContext(ctx, `UPDATE mcp_proposals SET metadata=? WHERE id=?`, string(metadata), id)
	return err
}

func (s *SQLStore) ApproveProposal(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var metaJSON []byte
	var currentStatus string
	var visiblePixelHash string
	if err := s.txQueryRow(tx, ctx, `SELECT metadata, status, visible_pixel_hash FROM mcp_proposals WHERE id=?`, id).Scan(&metaJSON, &currentStatus, &visiblePixelHash); err != nil {
		return err
	}

	var meta map[string]interface{}
	if len(metaJSON) > 0 {
		if err := json.Unmarshal(metaJSON, &meta); err != nil {
			return err
		}
	}
	meta = EnsureVisibleInMeta(meta, visiblePixelHash)

	proposal, err := s.GetProposal(ctx, id)
	if err != nil {
		return err
	}
	proposal.Metadata = meta
	if strings.TrimSpace(proposal.VisiblePixelHash) == "" {
		proposal.VisiblePixelHash = strings.TrimSpace(visiblePixelHash)
	}
	populateProposalTasks(&proposal)

	// Task count for shared BuildApprovePlan
	preKeys := ResolveApproveKeys(meta, id)
	var taskCount int
	if err := s.txQueryRow(tx, ctx, `SELECT count(*) FROM mcp_tasks WHERE contract_id=?`, preKeys.ContractID).Scan(&taskCount); err != nil {
		return err
	}
	plan, err := BuildApprovePlan(id, currentStatus, &proposal, taskCount)
	if err != nil {
		return err
	}
	keys := plan.Keys
	n, w := keys.NormalizedContractID, keys.WishContractID

	var conflict int
	if err := s.txQueryRow(tx, ctx, `
SELECT count(*) FROM mcp_proposals
WHERE id<>? AND status IN ('approved','published')
AND (
  id IN (?, ?) OR
  visible_pixel_hash IN (?, ?)
)
`, id, n, w, n, w).Scan(&conflict); err != nil {
		return err
	}
	if conflict > 0 {
		return ErrApproveConflict(keys.NormalizedContractID)
	}

	if _, err := s.txExec(tx, ctx, `
UPDATE mcp_proposals SET status='rejected'
WHERE id<>? AND status='pending' AND (
  id IN (?, ?) OR
  visible_pixel_hash IN (?, ?)
)
`, id, n, w, n, w); err != nil {
		return err
	}

	if _, err := s.txExec(tx, ctx, `UPDATE mcp_proposals SET status=? WHERE id=?`, plan.NewProposalStatus, id); err != nil {
		return err
	}

	if plan.WishToSupersede != "" {
		if _, err := s.txExec(tx, ctx, `UPDATE mcp_contracts SET status='superseded' WHERE contract_id=? AND `+supersedeEligibleStatusSQL, plan.WishToSupersede); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *SQLStore) PublishProposal(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var status string
	var metaJSON []byte
	if err := s.txQueryRow(tx, ctx, `SELECT status, metadata FROM mcp_proposals WHERE id=?`, id).Scan(&status, &metaJSON); err != nil {
		return err
	}
	var meta map[string]interface{}
	if len(metaJSON) > 0 {
		_ = json.Unmarshal(metaJSON, &meta)
	}
	plan, err := BuildPublishPlan(id, status, meta)
	if err != nil {
		return err
	}

	if _, err := s.txExec(tx, ctx, `UPDATE mcp_tasks SET status='published' WHERE contract_id=? AND status IN (`+PublishTaskStatusSQL+`)`, plan.ContractID); err != nil {
		return err
	}
	if _, err := s.txExec(tx, ctx, `UPDATE mcp_claims SET status='complete' WHERE task_id IN (SELECT task_id FROM mcp_tasks WHERE contract_id=?) AND status IN (`+PublishClaimStatusSQL+`)`, plan.ContractID); err != nil {
		return err
	}
	if _, err := s.txExec(tx, ctx, `UPDATE mcp_proposals SET status=? WHERE id=?`, plan.NewProposalStatus, id); err != nil {
		return err
	}

	return tx.Commit()
}

func (s *SQLStore) UpdateSubmissionStatus(ctx context.Context, submissionID, status, reviewerNotes, rejectionType string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var claimID string
	if err := s.txQueryRow(tx, ctx, `SELECT claim_id FROM mcp_submissions WHERE submission_id=?`, submissionID).Scan(&claimID); err != nil {
		return err
	}

	plan := DecideSubmissionStatusUpdate(status, reviewerNotes, rejectionType, time.Now())
	var rejectedAt interface{}
	if plan.RejectedAt != nil {
		rejectedAt = plan.RejectedAt.Format(time.RFC3339)
	}
	if _, err := s.txExec(tx, ctx, `
UPDATE mcp_submissions SET status=?, rejection_reason=?, rejection_type=?, rejected_at=? WHERE submission_id=?
`, plan.Status, plan.RejectionReason, plan.RejectionType, rejectedAt, submissionID); err != nil {
		return err
	}

	switch plan.Cascade {
	case SubmissionCascadeApprove:
		var taskID string
		if err := s.txQueryRow(tx, ctx, `SELECT task_id FROM mcp_claims WHERE claim_id=?`, claimID).Scan(&taskID); err == nil {
			_, _ = s.txExec(tx, ctx, `UPDATE mcp_tasks SET status=? WHERE task_id=?`, plan.TaskStatus, taskID)
			_, _ = s.txExec(tx, ctx, `UPDATE mcp_claims SET status=? WHERE claim_id=?`, plan.ClaimStatus, claimID)
		}
	case SubmissionCascadeReject:
		var taskID string
		if err := s.txQueryRow(tx, ctx, `SELECT task_id FROM mcp_claims WHERE claim_id=?`, claimID).Scan(&taskID); err == nil {
			_, _ = s.txExec(tx, ctx, `UPDATE mcp_tasks SET status=?, claimed_by=NULL, claimed_at=NULL, claim_expires_at=NULL WHERE task_id=?`, plan.TaskStatus, taskID)
			_, _ = s.txExec(tx, ctx, `UPDATE mcp_claims SET status=? WHERE claim_id=?`, plan.ClaimStatus, claimID)
		}
	}

	return tx.Commit()
}

func (s *SQLStore) UpdateSubmission(ctx context.Context, sub smart_contract.Submission) error {
	if sub.Deliverables != nil {
		delete(sub.Deliverables, "status")
	}
	if sub.CompletionProof != nil {
		delete(sub.CompletionProof, "status")
	}

	delivJSON, _ := json.Marshal(sub.Deliverables)
	proofJSON, _ := json.Marshal(sub.CompletionProof)
	_, err := s.execContext(ctx, `
UPDATE mcp_submissions SET status=?, deliverables=?, completion_proof=? WHERE submission_id=?
`, sub.Status, string(delivJSON), string(proofJSON), sub.SubmissionID)
	return err
}

func (s *SQLStore) DeleteWish(ctx context.Context, visiblePixelHash string) error {
	plan, err := BuildDeleteWishPlan(visiblePixelHash)
	if err != nil {
		return err
	}

	_, err = s.execContext(ctx, `DELETE FROM mcp_proposals WHERE id=? OR visible_pixel_hash=?`, plan.WishID, plan.VisiblePixelHash)
	if err != nil {
		return err
	}

	_, err = s.execContext(ctx, `DELETE FROM mcp_tasks WHERE contract_id=?`, plan.WishID)
	if err != nil {
		return err
	}

	_, err = s.execContext(ctx, `DELETE FROM mcp_contracts WHERE contract_id=?`, plan.WishID)
	return err
}

func (s *SQLStore) CreateContractReworkRequest(ctx context.Context, contractID, requester, notes string) (smart_contract.ContractReworkRequest, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return smart_contract.ContractReworkRequest{}, err
	}
	defer tx.Rollback()

	var metadataBytes []byte
	if err := s.txQueryRow(tx, ctx, `SELECT metadata FROM mcp_contracts WHERE contract_id=?`, contractID).Scan(&metadataBytes); err != nil {
		if err == sql.ErrNoRows {
			return smart_contract.ContractReworkRequest{}, fmt.Errorf("contract %s not found", contractID)
		}
		return smart_contract.ContractReworkRequest{}, err
	}

	now := time.Now()
	reworkReq, err := BuildReworkRequest(contractID, requester, notes, now, "")
	if err != nil {
		return smart_contract.ContractReworkRequest{}, err
	}

	meta := map[string]interface{}{}
	if len(metadataBytes) > 0 {
		_ = json.Unmarshal(metadataBytes, &meta)
	}
	meta = AppendReworkRequestToMetadata(meta, reworkReq)
	updatedMetadata, err := json.Marshal(meta)
	if err != nil {
		return smart_contract.ContractReworkRequest{}, err
	}

	if _, err := s.txExec(tx, ctx, `UPDATE mcp_contracts SET metadata=? WHERE contract_id=?`, string(updatedMetadata), contractID); err != nil {
		return smart_contract.ContractReworkRequest{}, err
	}
	if _, err := s.txExec(tx, ctx, `UPDATE mcp_tasks SET status=? WHERE contract_id=?`, ReworkTaskStatusOnCreate(), contractID); err != nil {
		return smart_contract.ContractReworkRequest{}, err
	}
	if err := tx.Commit(); err != nil {
		return smart_contract.ContractReworkRequest{}, err
	}
	return reworkReq, nil
}

func (s *SQLStore) GetContractReworkRequests(ctx context.Context, contractID string) ([]smart_contract.ContractReworkRequest, error) {
	var metadataBytes []byte
	if err := s.queryRowContext(ctx, `SELECT metadata FROM mcp_contracts WHERE contract_id=?`, contractID).Scan(&metadataBytes); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("contract %s not found", contractID)
		}
		return nil, err
	}
	var reqs []smart_contract.ContractReworkRequest
	if len(metadataBytes) > 0 {
		var meta map[string]interface{}
		if err := json.Unmarshal(metadataBytes, &meta); err == nil {
			reqs = ParseReworkRequestsFromMetadata(meta)
		}
	}
	return reqs, nil
}

func (s *SQLStore) ResolveContractReworkRequest(ctx context.Context, contractID, requestID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var metadataBytes []byte
	if err := s.txQueryRow(tx, ctx, `SELECT metadata FROM mcp_contracts WHERE contract_id=?`, contractID).Scan(&metadataBytes); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("contract %s not found", contractID)
		}
		return err
	}

	meta := map[string]interface{}{}
	if len(metadataBytes) > 0 {
		_ = json.Unmarshal(metadataBytes, &meta)
	}
	meta, err = ResolveReworkRequestInMetadata(meta, requestID, time.Now())
	if err != nil {
		return err
	}
	updatedMetadata, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	if _, err := s.txExec(tx, ctx, `UPDATE mcp_contracts SET metadata=? WHERE contract_id=?`, string(updatedMetadata), contractID); err != nil {
		return err
	}
	return tx.Commit()
}

// hydrateProposalTasks enriches proposal tasks with live statuses from the DB.
// Mirrors PGStore.hydrateProposalTasks.
func (s *SQLStore) hydrateProposalTasks(ctx context.Context, p *smart_contract.Proposal) {
	if p == nil {
		return
	}
	contractIDs := []string{}
	if cid, ok := p.Metadata["contract_id"].(string); ok && strings.TrimSpace(cid) != "" {
		contractIDs = append(contractIDs, cid)
	}
	if cid, ok := p.Metadata["visible_pixel_hash"].(string); ok && strings.TrimSpace(cid) != "" {
		contractIDs = append(contractIDs, cid)
	}
	if cid, ok := p.Metadata["ingestion_id"].(string); ok && strings.TrimSpace(cid) != "" {
		contractIDs = append(contractIDs, cid)
	}
	if strings.TrimSpace(p.ID) != "" {
		contractIDs = append(contractIDs, p.ID)
	}
	if len(contractIDs) == 0 {
		return
	}

	// Build IN clause for SQLite
	placeholders := make([]string, len(contractIDs))
	args := make([]interface{}, len(contractIDs))
	for i, id := range contractIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	rows, err := s.queryContext(ctx, `
SELECT task_id, contract_id, goal_id, title, description, budget_sats, skills, status,
       claimed_by, claimed_at, claim_expires_at, difficulty, estimated_hours, requirements, merkle_proof
FROM mcp_tasks WHERE contract_id IN (`+strings.Join(placeholders, ",")+`)
`, args...)
	if err != nil {
		return
	}
	defer rows.Close()

	liveTasks := make(map[string]smart_contract.Task)
	for rows.Next() {
		t, err := scanTaskSQLite(rows)
		if err != nil {
			continue
		}
		liveTasks[t.TaskID] = t
	}
	if len(liveTasks) == 0 {
		return
	}

	if len(p.Tasks) == 0 || len(liveTasks) > len(p.Tasks) {
		// DB tasks are authoritative — replace auto-generated placeholder tasks
		p.Tasks = make([]smart_contract.Task, 0, len(liveTasks))
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
