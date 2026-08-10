package smart_contract

import (
	"fmt"
	"strings"
	"time"
)

// normalizeInsert rewrites SQLite-only INSERT OR IGNORE for Postgres.
// When ON CONFLICT ... DO UPDATE is already present, only strips OR IGNORE.
// When no ON CONFLICT clause exists, appends ON CONFLICT DO NOTHING for PG.
func (s *SQLStore) normalizeInsert(q string) string {
	if s == nil || !s.isPostgres() {
		return q
	}
	upper := strings.ToUpper(q)
	if !strings.Contains(upper, "INSERT OR IGNORE") {
		return q
	}
	q = strings.Replace(q, "INSERT OR IGNORE INTO", "INSERT INTO", 1)
	q = strings.Replace(q, "insert or ignore into", "INSERT INTO", 1)
	if !strings.Contains(strings.ToUpper(q), "ON CONFLICT") {
		q = strings.TrimRight(strings.TrimSpace(q), ";")
		q += "\nON CONFLICT DO NOTHING"
	}
	return q
}

// encodeSkills serializes skills for INSERT/UPDATE binding.
// Postgres TEXT[] receives a '{a,b}' literal; SQLite stores comma-separated TEXT.
func (s *SQLStore) encodeSkills(skills []string) string {
	if s.isPostgres() {
		return pgTextArray(skills)
	}
	return strings.Join(skills, ",")
}

// skillsBind returns a placeholder with optional ::text[] cast for Postgres.
func (s *SQLStore) skillsBind() string {
	if s.isPostgres() {
		return "?::text[]"
	}
	return "?"
}

// jsonBind returns a placeholder with optional ::jsonb cast for Postgres.
func (s *SQLStore) jsonBind() string {
	if s.isPostgres() {
		return "?::jsonb"
	}
	return "?"
}

// decodeSkillsCSV parses comma-separated skills from skillsExpr SELECTs.
func decodeSkillsCSV(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// taskSelectList is the column list for mcp_tasks reads (skills normalized to text).
func (s *SQLStore) taskSelectList() string {
	return fmt.Sprintf(
		`task_id, contract_id, goal_id, title, description, budget_sats, %s AS skills, status, claimed_by, claimed_at, claim_expires_at, difficulty, estimated_hours, requirements, merkle_proof`,
		s.skillsExpr("skills"),
	)
}

// contractSkillsSelect is skills expression for contract rows.

// jsonTextExpr returns dialect SQL for extracting a text field from a JSON column.
// path is a bare key (e.g. "contract_id"), not a JSONPath.
func (s *SQLStore) jsonTextExpr(column, path string) string {
	if s.isPostgres() {
		return fmt.Sprintf("(%s->>'%s')", column, path)
	}
	return fmt.Sprintf("json_extract(%s, '$.%s')", column, path)
}

// formatTime stores timestamps portably (RFC3339 works for both TEXT and TIMESTAMPTZ drivers).
func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

// formatTimePtr formats a pointer or returns nil for SQL NULL.
