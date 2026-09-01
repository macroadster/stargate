# Image decode security audit (2026-08-03)

## Context

`POST /bitcoin/v1/extract` returned HTTP 500 for an inscription image in block
146754. Root cause: the payload was **AVIF** (`ftypavif`). Go’s registered
decoders cannot decode AVIF, so `image.Decode` failed and the handler mapped any
error to 500.

AVIF support was **not** added. Instead, unsupported / oversize formats soft-fail
as non-stego (HTTP 200, `message_found: false`). Adding libavif/dav1d was rejected
because untrusted on-chain images would hit native codecs with a history of
memory-corruption CVEs (e.g. CVE-2025-48174 libavif, CVE-2024-1580 dav1d).

## Fix summary

| Change | Purpose |
|--------|---------|
| `stego.DecodeImage` | Central safe decode: size cap, pixel/dimension limits, AVIF/HEIF sniff, no native codecs |
| Alpha/Trin scanners | Soft-fail unsupported formats (no circuit-breaker trip) |
| `HandleExtract` | Log failures; soft-fail path returns 200 |
| Frontend MIME map | Recognize `image/avif` / `image/svg+xml` extensions for form filenames |

## Attack surface inventory

### A. Server-side raster decode (in-process)

| Path | Input source | Decoder | Risk |
|------|----------------|---------|------|
| `/bitcoin/v1/extract` | User/UI multipart upload (often from inscription) | `stego.DecodeImage` → stdlib+x/image | Medium (DoS), low RCE |
| Block monitor `scanImagesDirectly` | Untrusted chain inscriptions | Same via scanners | Medium (DoS at scale) |
| Trin `LoadUnifiedInput` | Same | `stego.DecodeImage` | Same |
| `stego.Inscribe` / smart-contract publish | Cover images from ingestion | `stego.DecodeImage` | Auth-gated; still validate size |
| `extractStegoNative` (reconcile) | Stored stego images | `stego.DecodeImage` | Same |

**Registered formats only:** PNG, JPEG, GIF, WebP, BMP (all pure-Go / x/image).

**Deliberately not decoded:** AVIF, HEIC/HEIF, TIFF, SVG (as raster).

### B. Storage / serve-only (no raster decode)

| Path | Notes |
|------|--------|
| Block image storage under `/data/blocks/...` | Opaque bytes; AVIF allowed |
| `/uploads/`, `/api/block-image/` | Served with MIME sniffing; browser may decode |
| Security allowlist | `.avif`, `.svg` allowed as *filenames*, not server decode |

### C. Client-side

Browsers decode AVIF/SVG when displaying inscriptions. XSS risk is mainly **SVG
served same-origin** as `image/svg+xml` if scripts execute in document context.
Raster AVIF in `<img>` is generally safer than inline SVG.

## Vulnerability assessment

### 1. Remote code execution via codec bugs

| Vector | Status after fix |
|--------|------------------|
| Malicious AVIF → libavif RCE | **Not applicable** — we do not link libavif |
| Malicious HEIC | **Not applicable** |
| Malicious PNG/JPEG/GIF/WebP/BMP | **Residual, low** — pure-Go decoders; still update Go regularly |
| ImageMagick / graphicsmagick / ffmpeg shell-outs | **None found** in backend image paths |

### 2. Denial of service (decompression bombs)

| Control | Status |
|---------|--------|
| Multipart form cap | 32 MiB (`ParseMultipartForm`) |
| Image byte cap | 10 MiB (`stego.MaxDecodeBytes`) |
| Max dimension | 16384 per side |
| Max pixels | 64 megapixels before full decode (`DecodeConfig` first) |
| Alpha extract bit cap | 1M bits (~125 KiB payload scan) |
| Concurrent scan limits | Circuit breaker exists; soft-fail no longer trips it on AVIF |

**Residual:** Many concurrent large *valid* images can still burn CPU/RAM in the
block monitor. Recommend future work: worker pool + per-scan timeout.

### 3. Circuit-breaker availability issue (fixed)

Previously every AVIF decode error was a **hard** scanner failure → circuit
breaker open → *all* stego scans fail. Soft-fail removes this self-DoS.

### 4. SVG / HTML content XSS (residual)

- Extensions allow `.svg`; content type may be `image/svg+xml`.
- If opened as a navigable document on the same origin as the API/UI, script in
  SVG can be dangerous.
- **Mitigations to consider (not implemented in this change):**
  - Serve user/inscription content with `Content-Disposition: attachment` or
    `Content-Security-Policy: default-src 'none'; sandbox`
  - Prefer `image/svg+xml` only with CSP sandbox, or rewrite to raster for display
  - `X-Content-Type-Options: nosniff` on content routes

### 5. Path traversal / file write

- Uploads use path sanitization / partitioned layout; not re-audited deeply here.
- Extension allowlists exist (`security.AllowedImageExtensions`).

### 6. Polyglot / format confusion

- Sniffer refuses ISO-BMFF brands (`avif`/`heic`/…).
- Stego still trusts registered decoders’ format detection (normal).
- Frontend may label wrong extension if MIME is empty; server sniffs bytes.

## Intentional non-goals

- Full AVIF stego extraction (alpha LSB is not a practical AVIF carrier for Starlight).
- CGO image codecs of any kind in the main process.

## Verification

```bash
cd backend && go test ./stego/ ./starlight/ -count=1
# With sample: /tmp/block146754.avif → ExtractMessage soft-fails unsupported_format
```

## Follow-ups (optional)

1. CSP / sandbox headers on `/uploads/` and block-image content routes (SVG XSS).
2. Per-image scan timeout + global concurrency limit in block monitor.
3. Align `AllowedImageExtensions` docs: “store/serve” vs “server-decode”.
4. Structured metrics: `extract_unsupported_format_total{format=avif}`.
