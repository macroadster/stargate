# Content Endpoint Spec and Migration Plan

## Goals
- Provide a `/content` API compatible with existing ordinals explorers (simple raw fetch).
- Add safer, explicit access to inscription content when multiple witnesses/parts exist.
- Fix MIME/text parsing issues (trailing characters, incorrect content types).
- Support both combined and per-witness retrieval with integrity metadata.

## Proposed Endpoints
1) `GET /content/{txid}`
   - Returns the primary inscription payload (first inscription for the tx/witness 0 by default).
   - Query `?witness={index}` to fetch a specific witness payload.
   - Response:
     - `Content-Type`: inferred mime (fallback `application/octet-stream`).
     - Body: raw payload bytes.
     - Headers: `X-Inscription-Mime`, `X-Inscription-Size`, `X-Inscription-Hash` (sha256 of payload).

2) `GET /content/{txid}/manifest`
   - Returns JSON manifest for all inscription-bearing witnesses for the tx.
   - Response shape:
     ```json
     {
       "tx_id": "...",
       "parts": [
         {
           "witness_index": 0,
           "size_bytes": 1234,
           "mime_type": "text/html",
           "hash": "sha256:...",
           "primary": true,
           "url": "/content/{txid}?witness=0"
         }
       ],
       "stitch_hint": "single|multi|unknown"
     }
     ```
   - Allows clients to decide whether to fetch one or all parts; `stitch_hint` signals whether combining is expected.

## Parsing/MIME Fixes
- Ensure inscription parser trims trailing marker bytes (e.g., stray `h` from script parsing).
- Infer MIME from content when `ContentType` is missing or generic:
  - `text/html` if starts with `<html`/`<!doctype`.
  - `application/json` if valid JSON and no conflicting type.
  - `text/plain` for UTF-8 printable text.
  - Otherwise fallback `application/octet-stream`.
- Store computed hash and normalized mime with inscriptions to reuse in API responses.

## Backend Changes (Stargate)
- Add new handlers in `backend/api/data_api.go`:
  - `HandleContentRaw(w, r)` for `/content/{txid}`.
  - `HandleContentManifest(w, r)` for `/content/{txid}/manifest`.
  - Wire routes in `stargate_backend.go` mux.
- Extend storage layer to expose per-tx inscription lookups:
  - Given txid (and optional witness index), return payload bytes + metadata.
  - For file-backed blocks, read from `blocks/{height}_.../images/{file}` using txid→file mapping.
- Update parser (bitcoin/block_monitor.go or raw parser) to:
  - Normalize `ContentType`.
  - Strip trailing script artifacts.
  - Compute `Sha256` for each inscription payload.
  - Store witness index and txid on each `InscriptionData`.

## Frontend Impact
- Keep existing block/inscription flows unchanged.
- Optionally add a “Copy content URL” using `/content/{txid}` for inscription detail pages.

## Migration Plan
1. Implement parser fixes (mime normalization, trailing-byte trim, hashes).
2. Add storage lookup helpers for txid + witness index.
3. Implement `/content/{txid}` and `/content/{txid}/manifest` handlers; add routing.
4. Validate against existing blocks with known text/HTML inscriptions to ensure correct MIME and no trailing garbage.
5. (Optional) Add a frontend affordance to copy content URL; keep existing APIs intact.

## Open Questions
- Auth/rate-limit: should `/content` be public or gated for large payloads?
- Max payload size: enforce a cap per request? Stream-only for large binaries?
- Primary selection: default to witness 0 or first inscription by size/type?
