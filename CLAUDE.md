# Project Instructions for AI Agents

This file provides instructions and context for AI coding agents working on this project.

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


## Build & Test

Stargate is a **single binary** (embedded UI). Prefer the targets in the repo root `Makefile` — do **not** use retired split `make backend` / `make frontend` images.

```bash
# Unit tests (sources in frontend/ and backend/)
cd frontend && npm test
cd backend && go test ./...

# Unified local binary (embeds frontend → ./stargate)
make single-binary

# Unified Docker image for Helm (stargate:latest)
make docker
```

See **Agents.md** → “Stargate Development Guide” for deploy/verify with Helm (`starlight-stack`, one `stargate` container).

## Architecture Overview

Single process serves UI + `/api/*` + `/mcp/*` + `/bitcoin/v1/*`. Layers: transport (`handlers`, `mcp`, `api`) → app (`app/smart_contract`) → domain (`core`, `stego`, `bitcoin`) → storage (SQLite default and Postgres). Details: `docs/adr/`, `docs/arch/PACKAGE_BOUNDARIES.md`.

## Conventions & Patterns

_Add your project-specific conventions here_
