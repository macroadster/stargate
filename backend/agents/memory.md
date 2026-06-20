# Agents Module — Persistent Memory

## Module Identity
- **Path:** `backend/agents/`
- **Purpose:** Built-in Go autonomous agents for steganographic analysis orchestration (replaces former Python `starlight.agents`)

## Architecture
- **Orchestrator** (`orchestrator.go`): Central lifecycle coordinator, watches for pending tasks, dispatches to worker
- **Worker** (`worker.go`): Task execution engine with pluggable `Executor` interface
- **Watcher** (`watcher.go`): Task queue monitor, polls MCP for new tasks
- **Executor** (`executor.go`): Default `PlaceholderExecutor` (stub); wire custom executor for real LLM work
- **Config** (`config.go`): Env-based configuration with sane defaults
- **State** (`state.go`): Runtime state management
- **Types** (`types.go`): Shared domain types

## Key Configuration
| Variable | Default | Description |
|---|---|---|
| `STARGATE_AGENT_ENABLED` | `false` | Enable agent system |
| `STARGATE_AGENT_WATCHER_ENABLED` | `false` | Enable watcher loop |
| `STARGATE_AGENT_WORKER_ENABLED` | `false` | Enable worker goroutine |
| `STARGATE_AGENT_POLL_INTERVAL` | `60` | Poll interval in seconds |
| `STARGATE_AGENT_AI_IDENTIFIER` | `stargate-builtin-agent` | AI agent ID for task claims |

## Results Storage
- Written to `UPLOADS_DIR/results/<hash>/`
- Served at `/uploads/` and `/sandbox/`

## Session Log
- **2026-06-19:** Initial setup. Created `index.html` (agents frontend) and `memory.md` (persistent state).
- **2026-06-19:** Verified issues stargate-wgo.7.1 and stargate-wgo.9.2 were already fixed in reconciliation commit 0b6222d but never closed. Closed them.
