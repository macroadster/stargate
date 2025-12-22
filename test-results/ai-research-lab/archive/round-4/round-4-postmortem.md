# Round 4 Consolidated Postmortem

## Goal
Validate the full lifecycle: wish → proposal → contract → claim → submit → review → PSBT → confirm → close. Intended multi‑agent handoff: agent 1 inscribes a wish, agent 2 proposes, agent 1 approves, agent 2 executes, agent 1 reviews/PSBT, both monitor confirmation and close.

## What Worked
- Core MCP tool invocations (`/mcp/call`) are stable once correct endpoints are known.
- Contract listing via `/api/smart_contract/contracts` and task/submission flows operate reliably once a contract exists.
- Submission review and status updates are functional; UI shows deliverables after fixes.

## What Broke / Blocked the Ideal Workflow
- **Discovery/onboarding gaps:** `/api/inscribe` is the contract creation endpoint but is not discoverable via `/mcp/tools` or `/mcp/discover`. Agents tried `/api/smart_contract/inscribe` and other paths, hitting 404s.
- **Contract vs proposal ambiguity:** The system supports both proposals and contracts but lacks a clear state model that connects wishes, proposals, and contracts, leading to confusion about the correct creation path.
- **Unclear required fields:** `/api/inscribe` requires `message`, but docs/examples omit it, causing 400s and repeated trial‑and‑error.
- **Availability mismatches:** `list_tasks` returned empty/null while `list_contracts` reported available tasks.
- **Inconsistent error responses:** JSON vs plain text errors, missing “endpoint moved” hints slowed recovery.
- **No canonical lifecycle doc:** There is no single role‑based walkthrough mapping the ideal workflow to endpoints and expected state changes.

## Impact
Multi‑agent coordination stalled at contract creation and task discovery. Round 4 ended early because end‑to‑end execution (create/claim/submit/review/PSBT/confirm/close) could not be completed reliably.

## Recommendations (Prioritized)
1) Publish a single authoritative lifecycle doc mapping the ideal workflow to API endpoints and UI screens.
2) Clarify and unify the proposal/contract lifecycle state model.
3) Make `/api/inscribe` discoverable in MCP discovery/docs, including required fields.
4) Fix task availability inconsistencies (`list_tasks` vs `list_contracts`).
5) Standardize errors and improve onboarding at `/mcp/`.

## bd Issues Created
- `starlight-5le` (P1): Define and document end‑to‑end wish→proposal→contract workflow.
- `starlight-0dl` (P1): Unify contract vs proposal lifecycle state model.
- `starlight-rn6` (P1): Fix list_tasks availability mismatch.
- `starlight-wnc` (P2): Expose `/api/inscribe` in MCP discovery/docs.
- `starlight-eh9` (P2): Document `/api/inscribe` required fields (message).
- `starlight-ppo` (P2): Implement GET `/mcp/` discovery response.
- `starlight-dk9` (P2): Standardize error payloads across /mcp and /api.
- `starlight-85l` (P3): Add contract/task filters for agent coordination.
