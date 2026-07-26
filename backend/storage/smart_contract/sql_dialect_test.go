package smart_contract

import (
	"strings"
	"testing"

	"stargate-backend/storage/gormdb"
)

func TestNormalizeInsertPostgres(t *testing.T) {
	s := &SQLStore{dialect: gormdb.DialectPostgres}
	q := s.normalizeInsert(`INSERT OR IGNORE INTO mcp_claims (claim_id) VALUES (?)`)
	if strings.Contains(q, "OR IGNORE") {
		t.Fatalf("OR IGNORE should be stripped: %s", q)
	}
	if !strings.Contains(strings.ToUpper(q), "ON CONFLICT DO NOTHING") {
		t.Fatalf("expected DO NOTHING: %s", q)
	}
	// Already has ON CONFLICT update — do not append DO NOTHING
	q2 := s.normalizeInsert(`INSERT OR IGNORE INTO mcp_tasks (task_id) VALUES (?) ON CONFLICT(task_id) DO UPDATE SET status=excluded.status`)
	if strings.Contains(strings.ToUpper(q2), "DO NOTHING") {
		t.Fatalf("should not append DO NOTHING: %s", q2)
	}
	if strings.Contains(q2, "OR IGNORE") {
		t.Fatalf("OR IGNORE should be stripped: %s", q2)
	}
}

func TestNormalizeInsertSQLiteUnchanged(t *testing.T) {
	s := &SQLStore{dialect: gormdb.DialectSQLite}
	in := `INSERT OR IGNORE INTO mcp_claims (claim_id) VALUES (?)`
	if s.normalizeInsert(in) != in {
		t.Fatal("sqlite should keep INSERT OR IGNORE")
	}
}

func TestSkillsExprAndEncode(t *testing.T) {
	pg := &SQLStore{dialect: gormdb.DialectPostgres}
	if !strings.Contains(pg.skillsExpr("skills"), "array_to_string") {
		t.Fatal(pg.skillsExpr("skills"))
	}
	enc := pg.encodeSkills([]string{"go", "rust"})
	if enc != `{"go","rust"}` {
		t.Fatalf("pg encode: %s", enc)
	}
	sq := &SQLStore{dialect: gormdb.DialectSQLite}
	if sq.encodeSkills([]string{"go", "rust"}) != "go,rust" {
		t.Fatal(sq.encodeSkills([]string{"go", "rust"}))
	}
	if got := decodeSkillsCSV(" go, rust , "); len(got) != 2 || got[0] != "go" {
		t.Fatalf("%v", got)
	}
}

func TestJSONTextExpr(t *testing.T) {
	pg := &SQLStore{dialect: gormdb.DialectPostgres}
	if pg.jsonTextExpr("metadata", "contract_id") != "(metadata->>'contract_id')" {
		t.Fatal(pg.jsonTextExpr("metadata", "contract_id"))
	}
	sq := &SQLStore{dialect: gormdb.DialectSQLite}
	if sq.jsonTextExpr("metadata", "contract_id") != "json_extract(metadata, '$.contract_id')" {
		t.Fatal(sq.jsonTextExpr("metadata", "contract_id"))
	}
}

func TestRebindSkipsCastSuffix(t *testing.T) {
	s := &SQLStore{dialect: gormdb.DialectPostgres}
	got := s.rebind(`INSERT INTO t (a,b) VALUES (?::text[], ?::jsonb)`)
	if got != `INSERT INTO t (a,b) VALUES ($1::text[], $2::jsonb)` {
		t.Fatalf("got %q", got)
	}
}
