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
   - Build the single-binary Docker image: `make docker` (produces `stargate:latest`)
   - Deploy/upgrade via Helm (starlight-helm stack): Follow "Deployment Workflow"
   - Verify: Check logs and pod image IDs (the chart now deploys the unified `stargate` container)
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

### Development Workflow

**For code changes, follow this sequence:**

1. **Compile and test locally**:
   ```bash
   # Frontend
   cd frontend && npm install
   npm test
   
   # Backend  
   cd backend && go build
   go test ./...
   ```

2. **Build Docker image** (single-binary model):
   ```bash
   make docker   # Build stargate:latest (unified binary with embedded frontend)
   ```

   Split backend/frontend images are retired (`make backend-legacy` fails by design); use `make docker` / `make single-binary` only.

3. **Deploy to cluster** (see Deployment Workflow section)

### Testing Commands

#### Frontend (React)
```bash
cd frontend
npm test                          # Run Jest tests
npm test -- --testNamePattern="SpecificTest"  # Run single test
```

#### Backend (Go)
```bash
cd backend
go test ./...      # Run all tests (when implemented)
go test -run TestSpecificFunction  # Run single test
```

### Built-in Autonomous Agents (Go)

Stargate can now run the former Python `starlight.agents` orchestration logic natively.

Enable with environment variables (all optional, sensible defaults exist):

- `STARGATE_AGENT_ENABLED=true`
- `STARGATE_AGENT_WATCHER_ENABLED=true`
- `STARGATE_AGENT_WORKER_ENABLED=true`
- `STARGATE_AGENT_AI_IDENTIFIER="stargate-builtin-agent"`
- `STARGATE_AGENT_POLL_INTERVAL=60`
- `STARLIGHT_DONATION_ADDRESS` (gives the agent global auditor powers for approvals)

The agent writes results under `UPLOADS_DIR/results/<hash>/` (served at `/uploads/` and `/sandbox/`).

For real LLM-driven work, wire a custom `agents.Executor` (the default is a safe stub that produces placeholder reports). A future `opencode_run` MCP tool or sidecar executor will provide the real implementation path.

The Python agents in the starlight repo continue to work against the same MCP surface.

### Deployment Workflow

When you need to deploy code changes:

**Deploy local Docker image (single-binary) for testing**
```bash
# 1. Build the unified image locally
make docker   # Builds stargate:latest (single binary containing frontend + backend)

# 2. Upgrade the starlight-helm stack (adjust path/chart name to your checkout)
cd ../starlight-helm   # or the location of your starlight-helm chart
helm upgrade --install starlight-stack . \
  --set stargate.image.repository=stargate \
  --set stargate.image.tag=latest \
  --set stargate.image.pullPolicy=Never \
  # (or the equivalent values your chart uses under the stargate: section)

# 3. Wait for rollout (Helm handles the underlying Deployment/StatefulSet)
helm upgrade --install ...   # (re-run or use kubectl rollout status on the resources created by the chart)
```

**VERIFYING DEPLOYMENT (Helm-based single-binary stack):**
```bash
# Check running pods (use labels from your Helm release)
kubectl get pods -l app.kubernetes.io/instance=starlight-stack   # common Helm label; adjust if your chart uses different selectors
# or
kubectl get pods | grep -E 'starlight|stargate'

# Get a pod (the chart typically creates pods running the unified 'stargate' container)
POD=$(kubectl get pods -l app.kubernetes.io/instance=starlight-stack -o name | head -1)

# Check actual image deployed
kubectl describe $POD | grep -E "Image:|Image ID:"
# Should show: stargate:latest (local) or your-registry/stargate:...

# Verify image ID matches what you just built
docker images | grep stargate   # Note the Image ID (SHA256)
```

Use https://starlight.local for testing deployed changes

**DEPLOYMENT RULES (single-binary + Helm era):**

1. **NEVER assume `make docker` automatically deploys** - it only builds the local `stargate:latest` image
2. **NEVER blame "image not deployed" without verifying** - check actual pod image via the Helm release
3. **ALWAYS verify deployment** with `helm list`, `kubectl get pods -l ...` (from the chart) and `kubectl describe pod`
4. **If deployment uses Docker Hub images**, you must push there first (or use your registry)
5. **If using local images**, set the corresponding `image.pullPolicy: Never` (or equivalent) via Helm `--set` / values override
6. The legacy separate `stargate-backend` / `stargate-frontend` Deployments and `make backend` / `make frontend` paths are deprecated in favor of the unified single-binary image.

### Troubleshooting Common Issues

**"Changes not visible after deployment"**
```bash
# Get pod(s) managed by the Helm release and check image ID
kubectl get pods -l app.kubernetes.io/instance=starlight-stack
POD=$(kubectl get pods -l app.kubernetes.io/instance=starlight-stack -o name | head -1)
kubectl describe $POD | grep -A 5 "Image:" | grep "Image ID:"
docker images | grep stargate   # Compare Image IDs (look for the unified stargate image)
```

**"Pod keeps crashing with ImagePullBackOff"**
- Verify image exists locally: `docker images | grep stargate`
- If using local images with the Helm chart, ensure you passed `image.pullPolicy=Never` (or the chart's equivalent key) on upgrade
- If using a remote registry, verify the image was pushed: `docker pull .../stargate:latest`

**"Old code still running"**
```bash
# Inspect the resources created by your Helm release (Deployment, etc.)
helm get manifest starlight-stack | grep -A 20 "kind: Deployment" | grep -E 'image:|name:'
# or
kubectl get deployment -l app.kubernetes.io/instance=starlight-stack -o yaml | grep image:
helm upgrade --install starlight-stack . ...   # re-apply with updated image settings
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

<!-- BEGIN BEADS INTEGRATION v:1 profile:minimal hash:ca08a54f -->
## Beads Issue Tracker

This project uses **bd (beads)** for issue tracking. Run `bd prime` to see full workflow context and commands.

### Quick Reference

```bash
bd ready              # Find available work
bd show <id>          # View issue details
bd update <id> --claim  # Claim work
bd close <id>         # Complete work
```

### Rules

- Use `bd` for ALL task tracking — do NOT use TodoWrite, TaskCreate, or markdown TODO lists
- Run `bd prime` for detailed command reference and session close protocol
- Use `bd remember` for persistent knowledge — do NOT use MEMORY.md files

## Session Completion

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   bd dolt push
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
<!-- END BEADS INTEGRATION -->
