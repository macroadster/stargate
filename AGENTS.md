# Agent Instructions (Stargate)

## Common Stuck Points

**CRITICAL: Never assume deployment worked without verification.**

1. **`make backend` doesn't deploy automatically**
   - Only builds Docker image locally 
   - Must follow "Deployment Workflow" to actually deploy
   - Build ≠ Deploy

2. **Never blame "image not deployed" without verification**
   - Check actual pod image: `kubectl describe pod <name> | grep "Image:"`
   - Compare with local build: `docker images | grep stargate-backend`
   - Check code first before blaming deployment

3. **Always verify deployment**
   - Use kubectl commands to confirm changes are live
   - Check backend logs for actual errors
   - See "Deployment Verification" section

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
3. **Understand the task first**:
   - Read relevant code files before making changes
   - Check existing patterns and conventions
   - Identify what actually needs to be fixed/improved
4. **Work on it**: Implement, test, document
5. **Deploy and verify** (if code changes):
   - **MANDATORY**: Build and deploy to Kubernetes cluster BEFORE pushing code
   - Follow "Deployment Workflow" section
   - Verify the deployed code actually has your changes in the cluster
   - NEVER assume deployment worked without verification
6. **Discover new work?** Create linked issue:
   - `bd create "Found bug" -p 1 --deps discovered-from:<parent-id>`
7. **Complete**: `bd close <id> --reason "Done"`
8. **Commit together**: Always commit the `.beads/issues.jsonl` file together with the code changes so issue state stays in sync with code state

### Before Making Code Changes

**ALWAYS do this first:**
1. Read the file(s) you'll be editing
2. Understand the current code structure
3. Identify the specific location of the issue
4. Check for similar patterns in the codebase

**This prevents:**
- Making random guesses about what code does
- Breaking things you didn't understand
- Getting stuck on build/deployment issues
- Blaming "image not deployed" when code is wrong

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
- Store ALL AI-generated planning/design docs in `docs/history/`
- Keep the repository root clean and focused on permanent project files
- Only access `docs/history/` when explicitly asked to review past planning

**Example .gitignore entry (optional):**
```
# AI planning documents (ephemeral)
docs/history/
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
- Store AI planning docs in `docs/history/` directory
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
3. **DEPLOY AND VERIFY IN CLUSTER** - Mandatory for all code changes:
   - Build Docker images: `make backend` / `make frontend`
   - Deploy to cluster: Follow "Deployment Workflow"
   - Verify: Check logs and pod image IDs
4. **Update issue status** - Close finished work, update in-progress items
5. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   bd sync
   git push
   git status  # MUST show "up to date with origin"
   ```
6. **Clean up** - Clear stashes, prune remote branches
7. **Verify** - All changes committed AND pushed
8. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds
- Before blaming "image not deployed", follow "Deployment Verification" section above

## Stargate Development Guide

### Build & Deployment Workflow

**CRITICAL: Docker vs Local Images**

This project uses TWO different build methods. Do NOT confuse them:

1. **Docker builds (for deployment)**:
   ```bash
   make backend  # Builds stargate-backend:latest Docker image
   make frontend # Builds stargate-frontend:latest Docker image
   ```
   - Creates `stargate-backend:latest` and `stargate-frontend:latest` locally
   - Used for Kubernetes deployment
   - Images live in your local Docker daemon

2. **Local dev builds (for local testing only)**:
   ```bash
   cd backend && go run stargate_backend.go  # Run Go locally
   cd frontend && npm start                    # Run React locally
   ```
   - No Docker involved
   - Only for local development
   - Does NOT affect deployed services

**DEPLOYMENT RULES:**

1. **NEVER assume `make backend` automatically deploys** - it only builds locally
2. **NEVER blame "image not deployed" without verifying** - check actual pod image
3. **ALWAYS verify deployment** with `kubectl get pods` and `kubectl describe pod <name>`
4. **If deployment uses Docker Hub images**, you must push there first
5. **If using local images**, set `imagePullPolicy: Never` in deployment

### Deployment Workflow

When you need to deploy code changes:

**Deploy local Docker images (for testing)**
```bash
# 1. Build locally
make backend
make frontend

# 2. Update deployment to use local images
kubectl rollout restart deployment/stargate-backend:latest -n default
kubectl rollout restart deployment/stargate-frontend:latest -n default

# 3. Wait for rollout
kubectl wait --for=condition=available --timeout=60s deployment/stargate-backend -n default
kubectl wait --for=condition=available --timeout=60s deployment/stargate-frontend -n default
```

**VERIFYING DEPLOYMENT:**
```bash
# Check running pods
kubectl get pods -n default

# Get new pod name
kubectl get pods -n default | grep stargate-backend | grep Running | awk '{print $1}'

# Check actual image deployed
kubectl describe pod <pod-name> -n default | grep "Image ID:"
# Should show: stargate-backend:latest (local) or macroadster/stargate-backend:latest (Docker Hub)

# Verify image ID matches what you built
docker images | grep stargate-backend  # Note the Image ID (SHA256)
```

### When to Use Build Commands

**Use `make backend` / `make frontend` ONLY for deployment:**
- When you need to deploy code changes to Kubernetes cluster
- When testing deployment workflow
- NOT for local development

**Use local dev builds for local testing:**
- `cd backend && go run stargate_backend.go &`
- `cd frontend && npm start &`

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

**SECURITY - File Operations (CRITICAL):**
- **NEVER use `filepath.Join()` directly with user-controlled input**
- **ALWAYS use `security.SafeFilePath(baseDir, filename)` for file writes**
- **ALWAYS use `security.SanitizePath(baseDir, userPath)` for file reads**
- **ALWAYS validate extensions**: `security.ValidateExtension(filename, allowed)`
- **Valid examples**:
  ```go
  // File write:
  path := security.SafeFilePath(uploadsDir, userFilename)
  os.WriteFile(path, data, 0644)

  // File read:
  safePath, err := security.SanitizePath(baseDir, userPath)
  if err != nil {
      return fmt.Errorf("invalid path")
  }
  data, _ := os.ReadFile(safePath)
  ```
- **Invalid examples**:
  ```go
  // DON'T:
  path := filepath.Join(baseDir, userFilename)  // VULNERABLE
  path = baseDir + "/" + userFilename         // VULNERABLE
  data, _ := os.ReadFile(userPath)             // VULNERABLE
  ```

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

**LOCAL DEVELOPMENT (testing code changes locally):**
```bash
# Start backend locally (no Docker)
cd backend && go run stargate_backend.go > backend.log &

# Start frontend locally (no Docker)
cd frontend && npm start > frontend.log &
```

**DEPLOYMENT (pushing code changes to cluster):**
```bash
# 1. Build Docker images
make backend
make frontend

# 2. Deploy using "Deployment Workflow" above

# 3. Verify deployment using "Deployment Verification" section
```

### Deployment Verification

**Before blaming deployment, check in this order:**

1. **Did the code actually change?**
   ```bash
   git diff HEAD~1 <file-you-edited>
   ```

2. **Is the deployed code the new version?**
   ```bash
   kubectl describe pod <pod-name> -n default | grep -A 5 "Image:" | grep "Image ID:"
   docker images | grep stargate-backend
   # Compare Image ID values
   ```

3. **Is the bug actually in the code?**
   - Read the file you edited
   - Check logic and error handling
   - Look for typos or missing imports
   - Don't assume "deployment failed" when code might be wrong

4. **Check backend logs for actual errors:**
   ```bash
   kubectl logs <pod-name> -n default --tail=50 | grep -i error
   ```

5. **Then consider deployment issues:**
   - Image not found
   - ImagePullBackOff
   - CrashLoopBackOff
   - Pods not rolling out

**Common mistakes to avoid:**
- Assuming `make backend` automatically deploys to cluster
- Assuming `helm upgrade` always pulls latest local images
- Assuming `kubectl rollout restart` uses newly built images
- Not verifying actual pod image after deployment

**If images don't match:**
- Check deployment `imagePullPolicy` setting
- Verify you're building to right image name
- Check if Helm values override image settings

**Rule of thumb:** Check code first, then verify deployment, only then blame deployment.

### Troubleshooting Common Issues

**"Changes not visible after deployment"**
```bash
# Get pod name and check image ID
kubectl get pods -n default | grep stargate-backend | grep Running | awk '{print $1}'
kubectl describe pod <pod-name> -n default | grep -A 5 "Image:" | grep "Image ID:"
docker images | grep stargate-backend  # Compare Image IDs
```

**"Pod keeps crashing with ImagePullBackOff"**
- Verify image exists locally: `docker images | grep stargate-backend`
- If using local images, ensure `imagePullPolicy: Never` is set
- If using Docker Hub, verify image exists: `docker pull macroadster/stargate-backend:latest`

**"Old code still running"**
```bash
kubectl get deployment stargate-backend -n default -o yaml | grep image:
kubectl rollout restart deployment/stargate-backend -n default
kubectl rollout status deployment/stargate-backend -n default
```

**"Approval still times out after deployment"**
- Check backend logs: `kubectl logs <pod> -n default | grep -i "timeout\|stego"`
- Review fix logic - check both `stego_contract_id` AND skip reinscribing condition
- Verify metadata is being set correctly during `/api/inscribe`

<!-- bv-agent-instructions-v1 -->

---

## Beads Workflow Integration

This project uses [beads_viewer](https://github.com/Dicklesworthstone/beads_viewer) for issue tracking. Issues are stored in `.beads/` and tracked in git.

### Essential Commands

```bash
# View issues (launches TUI - avoid in automated sessions)
bv

# CLI commands for agents (use these instead)
bd ready              # Show issues ready to work (no blockers)
bd list --status=open # All open issues
bd show <id>          # Full issue details with dependencies
bd create --title="..." --type=task --priority=2
bd update <id> --status=in_progress
bd close <id> --reason="Completed"
bd close <id1> <id2>  # Close multiple issues at once
bd sync               # Commit and push changes
```

### Workflow Pattern

1. **Start**: Run `bd ready` to find actionable work
2. **Claim**: Use `bd update <id> --status=in_progress`
3. **Work**: Implement the task
4. **Complete**: Use `bd close <id>`
5. **Sync**: Always run `bd sync` at session end

### Key Concepts

- **Dependencies**: Issues can block other issues. `bd ready` shows only unblocked work.
- **Priority**: P0=critical, P1=high, P2=medium, P3=low, P4=backlog (use numbers, not words)
- **Types**: task, bug, feature, epic, question, docs
- **Blocking**: `bd dep add <issue> <depends-on>` to add dependencies

### Session Protocol

**Before ending any session, run this checklist:**

```bash
git status              # Check what changed
git add <files>         # Stage code changes
bd sync                 # Commit beads changes
git commit -m "..."     # Commit code
bd sync                 # Commit any new beads changes
git push                # Push to remote
```

### Best Practices

- Check `bd ready` at session start to find available work
- Update status as you work (in_progress → closed)
- Create new issues with `bd create` when you discover tasks
- Use descriptive titles and set appropriate priority/type
- Always `bd sync` before ending session

<!-- end-bv-agent-instructions -->
