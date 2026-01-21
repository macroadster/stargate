# Epic: Fix Ingestion Duplication and Visible Pixel Hash Mismatch

## Context
The project supports multiple ingestion methods (MCP, REST, IPFS). Currently, these methods can create feedback loops where the same contract/image is ingested multiple times as different entities. This leads to:
1.  **Duplicate Proposals:** Clones of the same contract.
2.  **Hash Mismatches:** The `visible_pixel_hash` used for P2WSH hash locks may differ from the original intent due to re-encoding or metadata changes during re-ingestion.
3.  **Premature File Creation:** Images are written to the `uploads/` directory immediately upon receipt, causing clutter and race conditions.

## Goal
Ensure a single source of truth for ingestion, prevent duplicate processing, and enforce deterministic `visible_pixel_hash` calculation. Images should only be materialized to the filesystem (for stego/download) when they reach an "approved" or "active" state.

## Strategy

### 1. Centralize Ingestion Logic
-   **Source of Truth:** The `starlight_ingestions` database table is the primary record.
-   **Disable Legacy File Write:** In `HandleCreateInscription`, stop writing files to disk via `inscriptionService` if `ingestionService` (DB) is active.
-   **Deferred Materialization:** Only write images to `uploads/` when necessary (e.g., upon approval or specific API request requiring the file) and if the status warrants it.

### 2. Fix `HandleCreateInscription`
-   **Location:** `handlers/handlers.go`
-   **Change:** Check if `h.ingestionService` is not nil. If so, create the DB record but **skip** the legacy `h.inscriptionService.CreateInscription` call that writes to disk.
-   **Return:** Return the `ingestion_id` and `visible_pixel_hash` from the DB record.

### 3. Update `fromIngestion` Materialization
-   **Location:** `handlers/handlers.go`
-   **Change:** In the `fromIngestion` method (used for listing pending transactions), add a check before writing `rec.ImageBase64` to disk.
-   **Condition:** Only write if `rec.Status` is `approved`, `confirmed`, `active`, or `published`. Pending items should rely on the DB blob or be served dynamically if needed (though typically listing doesn't need the file on disk immediately).

### 4. Verify P2WSH Hash Consistency
-   Ensure that the `visible_pixel_hash` used in the contract/proposal matches the one calculated from the raw image bytes + message at the moment of ingestion.
-   The `ingestionService` should be the authoritative source for this hash.

## Implementation Tasks

- [ ] **Task 1: Disable Legacy Write in `HandleCreateInscription`**
    -   Modify `handlers/handlers.go`.
    -   If `ingestionService` is available, skip `inscriptionService.CreateInscription`.
    -   Ensure response returns the correct `id`.

- [ ] **Task 2: Deferred File Materialization**
    -   Modify `fromIngestion` in `handlers/handlers.go`.
    -   Guard the `os.WriteFile` call with a status check (e.g., `!isPending(rec.Status)`).

- [ ] **Task 3: Cleanup Duplicate Code**
    -   Review `mcp/http_mcp_server.go` and `middleware/smart_contract` to ensure they aren't redundantly re-ingesting known hashes.

- [ ] **Task 4: Verification**
    -   Test the flow: Inscribe (REST) -> Check DB (should exist) -> Check `uploads/` (should NOT exist) -> Approve -> Check `uploads/` (should exist).
