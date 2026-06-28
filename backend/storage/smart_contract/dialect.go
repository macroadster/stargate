package smart_contract

// Compile-time checks: both durable dialects implement Store.
// Memory store is for tests/fixtures; SQLite and Postgres are production dialects (ADR 0002).
var (
	_ Store = (*SQLiteStore)(nil)
	_ Store = (*PGStore)(nil)
	_ Store = (*MemoryStore)(nil)
)
