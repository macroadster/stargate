# Trin / GGUF Starlight Scanner (Go)

Workstream 3 wires a **Trin/GGUF-backed** Starlight detector behind the existing
`core.StarlightScannerInterface` in the Stargate Go backend (`backend/starlight`).

No Python runs on the scan path. **Path A is live**: neural forward uses the public
package `github.com/macroadster/trin/pkg/starlight` (aliased as `trinstar` because
Stargate's local package is also named `starlight`).

## Environment variables

| Variable | Role | Default |
|----------|------|---------|
| `STARLIGHT_GGUF` | Preferred path to a Starlight GGUF (used only if the file exists) | — |
| `STARLIGHT_TRIN_MODEL` | Fallback alias if `STARLIGHT_GGUF` is unset/missing | — |
| `STARGATE_DATA_DIR` | Data root; default model lands at `$STARGATE_DATA_DIR/models/starlight.gguf` | `data` |
| `STARLIGHT_HF_REPO` | Hugging Face repo for auto-download | `macroadster/starlight-prod` |
| `STARLIGHT_HF_FILE` | File name inside the HF repo | `starlight.gguf` |
| `STARLIGHT_HF_REVISION` | HF revision (branch/tag/commit) | `main` |
| `STARLIGHT_GGUF_FORCE_DOWNLOAD` | If `1`/`true`/`yes`, re-download into the default path even if a local file exists | unset |

### Path resolution (existence-checked)

`ResolveTrinModelPath` / `EnsureTrinModel` (`backend/starlight/trin_model.go`):

1. `STARLIGHT_GGUF` if set **and** the path is a regular file
2. `STARLIGHT_TRIN_MODEL` if set **and** the path is a regular file
3. Well-known default: `$STARGATE_DATA_DIR/models/starlight.gguf` (or `data/models/starlight.gguf`) if it exists
4. **Ensure only:** download from Hugging Face into that default path
5. On download failure → empty string so Alpha fallthrough works (no crash)

Env values that point at a missing file are **skipped** (not treated as hard failure, and download is never written into the env-specified missing path).

```bash
# Optional: pin a local GGUF (must exist)
export STARLIGHT_GGUF=/path/to/starlight.gguf
# alias: STARLIGHT_TRIN_MODEL

# Or rely on auto-download into the data dir (happy path)
# unset STARLIGHT_GGUF; first init fetches starlight.gguf from HF
export STARGATE_DATA_DIR=/var/lib/stargate   # optional
```

URL shape: `https://huggingface.co/{repo}/resolve/{revision}/{file}`.

## Fallback selection chain

`ScannerManager.InitializeScanner` → `tryInitScanners()`:

1. **Trin** (`scannerType = "trin-gguf"`) — if `EnsureTrinModel()` returns a path **and**
   `NewTrinScanner(path).Initialize()` succeeds (file exists, `trinstar.Open` loads
   GGUF, tensor list non-empty). Missing local GGUF triggers HF auto-download.
2. **Alpha** (`"alpha"`) — Go-native alpha LSB scanner (default when no GGUF is
   available or Trin init fails). Download/Trin failures are logged and **do not** hard-fail the process.
3. **Mock** (`"mock"`) — last resort if Alpha init fails.

## What is real vs stubbed

| Component | Status |
|-----------|--------|
| `LoadUnifiedInput` multi-stream preprocess (pixel/meta/alpha/lsb/palette/features) | **Real** — Go port of `scripts/starlight_utils.py` `load_unified_input` |
| JPEG APP1 EXIF + post-image tail (`extract_post_tail`) | **Real** (EXIF: JPEG only; PNG/etc. usually empty EXIF) |
| GGUF open / weight session | **Real** — `trinstar.Open` (`github.com/macroadster/trin/pkg/starlight`) |
| Neural forward (`TrinScanner.forward` → `session.Forward`) | **Real** — BalancedStarlightDetector eval path |
| Alpha message extract on scan | **Real** — delegates to `stego.ExtractAlpha` when method is alpha/auto |
| Other extract methods (lsb/palette/exif/eoi) | MVP not-found |

Model version string: `trin-pkg-starlight`.

## go.mod linkage

Stargate depends on Trin's public API only (never `trin/internal/*`). Prefer a pure
GitHub `require` (no machine-local `replace`):

```go
// backend/go.mod
require github.com/macroadster/trin v0.0.0-<pseudo> // pinned via go get @commit or @main
```

```bash
cd backend
GOPROXY=direct go get github.com/macroadster/trin@62863b8
# or: GOPROXY=direct go get github.com/macroadster/trin@main
go mod tidy
```

If the module is private and fetch fails, set `GOPRIVATE=github.com/macroadster/*`
and retry with `GOPROXY=direct`. A local `replace` is only a last-resort fallback
for offline work — not the supported default.

Import alias (required — package name collision with local `package starlight`):

```go
import trinstar "github.com/macroadster/trin/pkg/starlight"
```

## Unified input layout

Matches the Python training/inference bundle (CHW-ready floats). **Production spatial
size is always H=W=256** (`unifiedSize`); original image width/height are retained on
`UnifiedInput.Width` / `Height` for feature norms only.

- `Pixel` `(3,256,256)` ∈ [0,1]
- `Meta` `(2048,)` = pad/truncate(exif+tail)/255
- `Alpha` `(2,256,256)` full α/255 + α LSB
- `LSB` `(3,256,256)` RGB LSB 0/1 CHW
- `Palette` `(768,)` /255
- `PaletteLSB` `(1,256,256)`
- `FormatFeatures` `(6,)` = has_alpha, alpha_std, is_palette, is_rgb, w/256, h/256
- `ContentFeatures` `(6,)` = lsb_content(3) + alpha_content(3)

**Center crop:** if both dims ≥ 256, center crop; if smaller, zero-pad out-of-bounds (black) then center — documented near-equivalent to torchvision `CenterCrop` pad behavior.

Forward call:

```go
out, err := s.session.Forward(trinstar.Inputs{
    Pixel: in.Pixel, Meta: in.Meta, Alpha: in.Alpha, LSB: in.LSB,
    Palette: in.Palette, PaletteLSB: in.PaletteLSB,
    FormatFeatures: in.FormatFeatures, ContentFeatures: in.ContentFeatures,
    H: unifiedSize, W: unifiedSize, // always 256 for UnifiedInput
    MetaWeight: 0.3,                // 0 ⇒ 0.3 inside Trin
})
// stego logit = out.Stego[0]; method logits = out.Method[0:5]
// ScanImage applies sigmoid → StegoProbability and argmax → MethodID / StegoType
```

## Lifecycle

- `Initialize`: closes any previous session, `trinstar.Open(modelPath)`, requires non-empty tensor list.
- `Close`: releases the weight session (call on re-init and when tearing down if held).
- `ScanImage`: `LoadUnifiedInput` → `forward` → sigmoid / argmax / optional alpha extract.

## Package map

- `backend/starlight/unified_input.go` — preprocess
- `backend/starlight/trin_model.go` — path resolve + HF auto-download (`EnsureTrinModel`)
- `backend/starlight/trin_scanner.go` — `TrinScanner`, session, real `forward`
- `backend/starlight/scanner_manager.go` — selection order
- `backend/starlight/trin_model_test.go` / `trin_scanner_test.go` — resolve/download (httptest), selection, real-forward (skips if no GGUF)

## Operator note

**Happy path:** leave GGUF envs unset; on first init Stargate downloads
`starlight.gguf` into `$STARGATE_DATA_DIR/models/starlight.gguf` (or `data/models/…`)
and selects Trin. Offline / missing HF asset / bad weights → log and fall through to
Alpha; the process keeps serving scans.

A missing `STARLIGHT_GGUF` path is skipped (not fatal). An existing but invalid GGUF
fails Trin init and falls through to Alpha.

```bash
cd backend
go test ./starlight/ -count=1
STARLIGHT_GGUF=/path/to/starlight.gguf go test ./starlight/ -count=1 -v -run RealForward
```

Trin API reference: `trin/docs/STARLIGHT_INFER.md`.
