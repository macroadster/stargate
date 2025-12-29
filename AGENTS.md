# Agent Instructions (Stargate)

## Beads Workflow

- Issue lifecycle: `bd ready` → `bd update <id> --status in_progress` → work → `bd close <id>`.
- Keep bd synced with git: prefer working inside `stargate/` so bd can read git status.

### Quick Reference

```bash
bd ready              # Find available work
bd show <id>          # View issue details
bd update <id> --status in_progress  # Claim work
bd close <id>         # Complete work
bd sync               # Sync with git
```

## Issue Tracking with bd (beads)

**IMPORTANT**: This project uses **bd (beads)** for ALL issue tracking. Do NOT use markdown TODOs, task lists, or other tracking methods.

### Why bd?

- Dependency-aware: Track blockers and relationships between issues
- Git-friendly: Auto-syncs to JSONL for version control
- Agent-optimized: JSON output, ready work detection, discovered-from links
- Prevents duplicate tracking systems and confusion

### Quick Start

**Check for ready work:**
```bash
bd ready --json
```

**Create new issues:**
```bash
bd create "Issue title" -t bug|feature|task -p 0-4 --json
bd create "Issue title" -p 1 --deps discovered-from:bd-123 --json
bd create "Subtask" --parent <epic-id> --json  # Hierarchical subtask (gets ID like epic-id.1)
```

**Claim and update:**
```bash
bd update bd-42 --status in_progress --json
bd update bd-42 --priority 1 --json
```

**Complete work:**
```bash
bd close bd-42 --reason "Completed" --json
```

### Issue Types

- `bug` - Something broken
- `feature` - New functionality
- `task` - Work item (tests, docs, refactoring)
- `epic` - Large feature with subtasks
- `chore` - Maintenance (dependencies, tooling)

### Priorities

- `0` - Critical (security, data loss, broken builds)
- `1` - High (major features, important bugs)
- `2` - Medium (default, nice-to-have)
- `3` - Low (polish, optimization)
- `4` - Backlog (future ideas)

### Workflow for AI Agents

1. **Check ready work**: `bd ready --json`
2. **Claim your task**: `bd update <id> --status in_progress`
3. **Work on it**: Implement, test, document
4. **Discover new work?** Create linked issue:
   - `bd create "Found bug" -p 1 --deps discovered-from:<parent-id>`
5. **Complete**: `bd close <id> --reason "Done"`
6. **Commit together**: Always commit the `.beads/issues.jsonl` file together with the code changes so issue state stays in sync with code state

### Auto-Sync

bd automatically syncs with git:
- Exports to `.beads/issues.jsonl` after changes (5s debounce)
- Imports from JSONL when newer (e.g., after `git pull`)
- No manual export/import needed!

### GitHub Copilot Integration

If using GitHub Copilot, also create `.github/copilot-instructions.md` for automatic instruction loading.
Run `bd onboard` to get the content, or see step 2 of the onboard instructions.

### MCP Server (Recommended)

If using Claude or MCP-compatible clients, install the beads MCP server:

```bash
pip install beads-mcp
```

Add to MCP config (e.g., `~/.config/claude/config.json`):
```json
{
  "beads": {
    "command": "beads-mcp",
    "args": []
  }
}
```

Then use `mcp__beads__*` functions instead of CLI commands.

### Managing AI-Generated Planning Documents

AI assistants often create planning and design documents during development:
- PLAN.md, IMPLEMENTATION.md, ARCHITECTURE.md
- DESIGN.md, CODEBASE_SUMMARY.md, INTEGRATION_PLAN.md
- TESTING_GUIDE.md, TECHNICAL_DESIGN.md, and similar files

**Best Practice: Use a dedicated directory for these ephemeral files**

**Recommended approach:**
- Create a `history/` directory in the project root
- Store ALL AI-generated planning/design docs in `history/`
- Keep the repository root clean and focused on permanent project files
- Only access `history/` when explicitly asked to review past planning

**Example .gitignore entry (optional):**
```
# AI planning documents (ephemeral)
history/
```

**Benefits:**
- Clean repository root
- Clear separation between ephemeral and permanent documentation
- Easy to exclude from version control if desired
- Preserves planning history for archeological research
- Reduces noise when browsing the project

### CLI Help

Run `bd <command> --help` to see all available flags for any command.
For example: `bd create --help` shows `--parent`, `--deps`, `--assignee`, etc.

### Important Rules

- Use bd for ALL task tracking
- Always use `--json` flag for programmatic use
- Link discovered work with `discovered-from` dependencies
- Check `bd ready` before asking "what should I work on?"
- Store AI planning docs in `history/` directory
- Run `bd <cmd> --help` to discover available flags
- Do NOT create markdown TODO lists
- Do NOT use external issue trackers
- Do NOT duplicate tracking systems
- Do NOT clutter repo root with planning documents

For more details, see README.md and QUICKSTART.md.

### Landing the Plane (session completion)

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   bd sync
   git push
   git status  # MUST show "up to date with origin"
   ```
5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds

## Stargate Development Guide

### Build Commands

#### Frontend (React)
```bash
cd frontend
npm install
npm start > frontend.log &        # Dev server on localhost:3000
npm run build                     # Production build
npm test                          # Run Jest tests
npm test -- --testNamePattern="SpecificTest"  # Run single test
```

#### Backend (Go)
```bash
cd backend
go mod tidy        # Install dependencies
go run stargate_backend.go > backend.log & # Dev server on localhost:3001
go build           # Build binary
go test ./...      # Run all tests (when implemented)
go test -run TestSpecificFunction  # Run single test
```

### Kubernetes Smoke Testing (starlight-stack)

Stargate is already deployed via the starlight-stack Helm chart. For quick
cluster validation after code changes:

```bash
make all
```

Then trigger a rolling restart for the updated workload(s) in the cluster
(deployment names in default namespace):

```bash
kubectl rollout restart deployment/stargate-frontend -n default
kubectl rollout restart deployment/stargate-backend -n default
```

### PSBT Verification (Commitment + Multi-Payout)

```bash
curl -sS -X POST http://starlight.local/api/smart_contract/contracts/<contract_id>/psbt \
  -H 'Content-Type: application/json' \
  -H 'X-API-Key: <payer_api_key>' \
  -d '{
    "contractor_wallet": "<fallback_address>",
    "task_id": "<task_id>",
    "budget_sats": <budget_sats>,
    "fee_rate_sats_vb": 1,
    "pixel_hash": "<visible_pixel_hash_hex>",
    "payouts": [
      {"address": "<contractor_a>", "amount_sats": 500},
      {"address": "<contractor_b>", "amount_sats": 500}
    ]
  }' | jq .
```

Confirm the response includes `payout_scripts`, `payout_amounts`, `commitment_script`,
`redeem_script`, and `commitment_address`.

```bash
curl -sS -X POST http://starlight.local/api/smart_contract/contracts/<contract_id>/commitment-psbt \
  -H 'Content-Type: application/json' \
  -d '{
    "task_id": "<task_id>",
    "preimage": "<visible_pixel_hash_hex>",
    "fee_rate_sats_vb": 1
  }' | jq .
```

### Code Style Guidelines

#### Frontend (React/JavaScript)
- **Components**: PascalCase (BlockCard, InscriptionModal)
- **Files**: PascalCase.js for components
- **Hooks**: camelCase with use prefix (useBlocks, useInscriptions)
- **Constants**: UPPER_SNAKE_CASE
- **Functions**: camelCase
- **Imports**: ES6 imports, React hooks first
- **Error Handling**: Try-catch with user-friendly messages
- **State Management**: React hooks (useState, useEffect, useCallback)

#### Backend (Go)
- **Packages**: lowercase, single word (handlers, services, models)
- **Files**: snake_case.go (data_storage.go, block_handler.go)
- **Structs**: PascalCase (BlockData, InscriptionService)
- **Functions**: PascalCase for exported, camelCase for unexported
- **Constants**: UPPER_SNAKE_CASE or PascalCase for exported
- **Error Handling**: Explicit error returns, wrap errors with context
- **Imports**: Grouped (stdlib, third-party, local packages)

### Testing

#### Frontend
- **Framework**: Jest with React Testing Library
- **Test Files**: *.test.js alongside components
- **Run Single**: `npm test -- --testNamePattern="TestName"`

#### Backend
- **Framework**: Go testing package
- **Test Files**: *_test.go alongside source files
- **Run Single**: `go test -run TestFunctionName`

### Linting/Formatting

#### Frontend
- **ESLint**: Configured via package.json (react-app preset)
- **Prettier**: Uses Create React App defaults
- **Fix**: `npm run lint` (if configured)

#### Backend
- **Format**: `go fmt ./...`
- **Lint**: `golint ./...` (if installed)
- **Vet**: `go vet ./...`

### Key Patterns

#### Frontend
- Use custom hooks for API calls and state management
- Component composition over inheritance
- Tailwind classes for styling (no inline styles)
- Proper error boundaries and loading states

#### Backend
- Dependency injection via container pattern
- Middleware chain for cross-cutting concerns
- File-based storage with proper error handling
- RESTful API design with JSON responses

### Development Workflow

1. Start backend: `cd backend && go run stargate_backend.go &`
2. Start frontend: `cd frontend && npm start`
3. Backend runs on :3001, Frontend on :3000
4. Use background processes for long-running servers
