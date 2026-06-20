# Agents Package - Task Memory

## Session 1 (2026-06-19)

### Work Done
- Fixed `UpdateProposal` status bug in all 3 store implementations:
  - **MemoryStore** (`storage/smart_contract/memory_store.go:947`): `p.Status = existing.Status` overwrote the caller's status. Changed to `if p.Status == "" { p.Status = existing.Status }`.
  - **PGStore** (`storage/smart_contract/pg_store.go:1768`): Same bug. Changed to same conditional.
  - **SQLiteStore** (`storage/smart_contract/sqlite_store.go:1227`): Status column was omitted from the UPDATE query entirely. Added `status=?` to SET clause.
- Fixed `findAvailableTasks` in `watcher.go` to search using canonical contract ID (`wish-` prefix) when the bare VPH search returns no tasks.
- Updated watcher tests to assert on proposal status after rejection.

### Root Cause
The `UpdateProposal` stores always overwrote the caller-provided `Status` with the existing status, making it impossible to change a proposal's status (e.g. from "pending" to "rejected"). The watcher's `rejectProposal` appeared to succeed silently but never persisted the rejection.

### Key Decisions
- Allow status changes through `UpdateProposal` when the caller provides a non-empty status
- Keep the "must be pending" gate since we only want status transitions from pending
- SQLiteStore now properly persists status column

## Session 2 (2026-06-20)

### Work Done
- Fixed stargate-wgo.8.1 — README was badly outdated vs reality:
  - Features/Frontend: "Tailwind CSS" → "QuantumCSS"
  - Project structure: `starlight_backend.go` → `stargate_backend.go`
  - Added all 17 backend packages to project structure (agents/, api/, bitcoin/, cmd/, container/, core/, docs/, handlers/, mcp/, middleware/, models/, scripts/, security/, services/, starlight/, stego/, storage/)
  - Added frontend subdirectories (components/, pages/, context/, hooks/, utils/) plus vite.config.js and index.html

### Changes Made
- `README.md`: Updated line 107 (Tailwind CSS → QuantumCSS), lines 226-260 (updated project structure with all actual backend packages and frontend subdirectories)

## Session 3 (2026-06-20)

### Work Done
- Closed stargate-wgo.8.1 and stargate-wgo.10 — both were already fixed in code (commits 9984959, b583e10) but never closed in bd.
- Verified no remaining ready work in `bd ready`.
