package smart_contract

// Compile-time checks: durable SQLStore (SQLite+Postgres via gormdb) and Memory.
var (
	_ Store = (*SQLStore)(nil)
	_ Store = (*MemoryStore)(nil)
)
