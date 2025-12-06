# Stargate UX & Data Pipeline Improvements

Targets the horizontal block rail, smart contract pagination, persistent storage, and Starlight scanner callbacks. Focus is on removing filesystem coupling and delivering smoother scrolling and loading behavior.

---

## 1) Seamless Horizontal Block Rail

- **Problem:** `frontend/src/hooks/useBlocks.js` pulls a fixed window from `/api/data/blocks?limit=` and hand-pins historic heights. Users hit gaps when scrolling back because older blocks are not discoverable without manual height fetches.
- **Backend**
  - Add paged block summary endpoint: `GET /api/data/blocks?limit=50&cursor_height=926320&direction=backward`.
  - Source summaries from persistent storage (see Section 3) with fields: `block_height`, `block_hash`, `timestamp`, `tx_count`, `inscription_count`, `smart_contract_count`, `stego_counts`, `preview_inscriptions` (first 3 file names or thumbnails).
  - Return cursors: `next_cursor_height` (oldest height in page - 1) and `has_more`.
  - Keep a lightweight cache in `DataStorage` keyed by height range; expire on new block writes or on-demand scans.
  - If data missing, trigger async scan via `blockMonitor.ProcessBlock(height)` but respond with `status: warming` plus a `retry_after` hint; UI can show skeletons.
- **Frontend**
  - Convert the horizontal scroller to a cursor-based loader: on near-end scroll, request the next page via `cursor_height`.
  - Render summary cards first (height/hash/timestamp/inscription + stego counts), lazy load thumbnails when `preview_inscriptions` are present.
  - Preserve pinned milestones (genesis/halvings/taproot) by injecting them into the rail but keep them outside pagination math.

## 2) Smart Contract / Inscription Pagination

- **Problem:** `useInscriptions` fetches the full block image list from `/api/block-images?height=...` then slices locally. Large stego-heavy blocks stall initial render.
- **Backend**
  - New endpoint: `GET /api/data/block/{height}/inscriptions?limit=20&cursor=file_0021&filter=text|all`.
  - Query storage for inscription metadata with optional filters (text-only, stego-only). Include `total`, `returned`, `next_cursor`.
  - Offer `fields=summary|full` to allow initial lightweight fetch (no embedded text bodies, no large metadata) followed by detail fetch per inscription on demand.
  - Maintain backward compatibility by keeping `/api/block-images` but mark as deprecated once UI migrates.
- **Frontend**
  - Update `useInscriptions` to request pages with cursors and append results; use `fields=summary` for the grid, fetch `fields=full` when a card is opened.
  - Ensure infinite scroll sentinel drives page fetches instead of preloading all images; show “warming scan” state when backend responds with `status: warming`.

## 3) Move Filesystem Data to Database

- **Problem:** Blocks live under `backend/blocks` and uploads in `/data/uploads`; JSON files (`inscriptions.json`, `block.json`) and raw images create cold-start latency and inconsistent state across replicas.
- **Storage Strategy (Postgres-first)**
  - Reuse `storage.PostgresStorage` as the default when `STARGATE_PG_DSN` is set; keep filesystem only as a dev fallback.
  - Schema additions:
    - `block_scans` (existing) extended with `inscription_count`, `tx_count`, `preview_inscriptions JSONB`.
    - `block_inscriptions` (`block_height`, `tx_id`, `file_name`, `content_type`, `size_bytes`, `content BYTEA or object_url`, `scan_result JSONB`, `created_at`).
    - `uploads` (`id`, `filename`, `mime_type`, `size_bytes`, `payload BYTEA or object_url`, `metadata JSONB`, `created_at`).
  - Store large binaries either in `BYTEA` (for fast local dev) or behind an object-store adapter (S3-compatible) while keeping metadata + signed URL in Postgres.
  - Introduce a `StorageProvider` interface for uploads and block artifacts so services (`InscriptionService`, block monitor) can write to DB or object store without touching the filesystem.
- **Migration**
  - One-time importer to read existing `backend/blocks/**/inscriptions.json` and `backend/uploads/*` into the new tables.
  - Backfill hashes/checksums to detect duplicates; keep the original path as `legacy_path` for audit.
  - Flip default envs: `STARGATE_STORAGE=postgres`, `DATA_DIR` only used for temp buffers.

## 4) Starlight Scanner Callback API (No More `inscriptions.json` Writes)

- **Problem:** The Python scanner currently writes results to filesystem JSON (`inscriptions.json`) which the Go services then read. This breaks in multi-instance deployments and under Postgres storage.
- **API Design**
  - Endpoint: `POST /api/stego/callback` with HMAC-authenticated header `X-Starlight-Signature` (HMAC-SHA256 over body using shared secret `STARLIGHT_CALLBACK_SECRET`).
  - Payload:
    ```json
    {
      "request_id": "scan-uuid",
      "block_height": 926320,
      "tx_id": "abcd...1234",
      "file_name": "tx_00.png",
      "content_type": "image/png",
      "size_bytes": 128773,
      "scan_result": {
        "is_stego": true,
        "stego_probability": 0.93,
        "stego_type": "lsb",
        "confidence": 0.91,
        "extracted_message": "hello",
        "prediction": "stego",
        "scanned_at": 1735083900
      },
      "image_bytes_b64": "<optional when proxy wants backend to persist>",
      "metadata": { "model_version": "1.2.0" }
    }
    ```
  - Idempotency: require `Idempotency-Key` header; dedupe on `(request_id, tx_id, file_name)`.
  - Responses: `202 Accepted` when enqueued, `409` on duplicate, `401` on bad signature.
- **Processing Flow**
  - Handler validates signature, enqueues to a work queue (channel/Redis/SQS) to avoid blocking scanner.
  - Worker writes `block_inscriptions` and updates `block_scans` summary counters; triggers SSE/WS update via `dataStorage.CreateRealtimeUpdate`.
  - Frontend polls `/api/data/block/{height}/inscriptions` and receives the fresh scan results without touching the filesystem.
- **Deprecation**
  - Mark filesystem-based `inscriptions.json` writes as dev-only. Keep reader compatibility during migration with a feature flag `ENABLE_FS_FALLBACK=false` by default.

## Rollout Plan

1. **Storage First:** Ship Postgres-backed storage provider and migration scripts; run importer and flip `STARGATE_STORAGE` default.
2. **Block Rail Pagination:** Add cursorized `/api/data/blocks` and migrate `useBlocks` to use cursors.
3. **Inscriptions Pagination:** Release `/api/data/block/{height}/inscriptions` and update `useInscriptions` + UI sentinel logic.
4. **Scanner Callback:** Deploy `/api/stego/callback`, enable in Python scanner, disable filesystem writes after parity verification.
5. **Cleanup:** Remove legacy `blocks/*/*.json` reliance from runtime code paths; keep optional dev flag for offline demos.

## Success Criteria

- Horizontal scroll never “dead ends”; loading older blocks fetches summaries without manual height entry.
- Initial block render stays sub-1s on stego-heavy blocks because data arrives in pages.
- No new JSON/asset writes under `backend/blocks` or `/data/uploads` in production mode; all persisted via Postgres/object store.
- Scanner can restart independently and still deliver results through the callback API; Go backend reflects updates without reading `inscriptions.json`.
