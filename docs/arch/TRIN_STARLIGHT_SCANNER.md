# Trin / GGUF Starlight Scanner (Go)

Workstream 3 wires a **Trin/GGUF-backed** Starlight detector behind the existing
`core.StarlightScannerInterface` in the Stargate Go backend (`backend/starlight`).

No Python runs on the scan path. **Path A is live**: neural forward uses the public
package `github.com/ericchien/trin/pkg/starlight` (aliased as `trinstar` because
Stargate's local package is also named `starlight`).

## Environment variables

| Variable | Role |
|----------|------|
| `STARLIGHT_GGUF` | Preferred path to a Starlight GGUF weights file (e.g. `starlight.gguf`) |
| `STARLIGHT_TRIN_MODEL` | Fallback alias if `STARLIGHT_GGUF` is unset |

When **neither** is set, selection stays **Alpha → Mock** (regression-safe; GGUF not required).

```bash
export STARLIGHT_GGUF=/path/to/starlight.gguf
# or
export STARLIGHT_TRIN_MODEL=/path/to/starlight.gguf
```

Produce `starlight.gguf` via the Trin / Project Starlight export pipeline; point either
env at that file and restart the Stargate process.

## Fallback selection chain

`ScannerManager.InitializeScanner` → `tryInitScanners()`:

1. **Trin** (`scannerType = "trin-gguf"`) — only if env path is non-empty **and**
   `NewTrinScanner(path).Initialize()` succeeds (file exists, `trinstar.Open` loads
   GGUF, tensor list non-empty).
2. **Alpha** (`"alpha"`) — Go-native alpha LSB scanner (default when env unset or Trin init fails). Trin failures are logged and **do not** hard-fail the process.
3. **Mock** (`"mock"`) — last resort if Alpha init fails.

## What is real vs stubbed

| Component | Status |
|-----------|--------|
| `LoadUnifiedInput` multi-stream preprocess (pixel/meta/alpha/lsb/palette/features) | **Real** — Go port of `scripts/starlight_utils.py` `load_unified_input` |
| JPEG APP1 EXIF + post-image tail (`extract_post_tail`) | **Real** (EXIF: JPEG only; PNG/etc. usually empty EXIF) |
| GGUF open / weight session | **Real** — `trinstar.Open` (`github.com/ericchien/trin/pkg/starlight`) |
| Neural forward (`TrinScanner.forward` → `session.Forward`) | **Real** — BalancedStarlightDetector eval path |
| Alpha message extract on scan | **Real** — delegates to `stego.ExtractAlpha` when method is alpha/auto |
| Other extract methods (lsb/palette/exif/eoi) | MVP not-found |

Model version string: `trin-pkg-starlight`.

## go.mod linkage (local Trin)

Stargate depends on Trin's public API only (never `trin/internal/*`):

```go
// backend/go.mod
require github.com/ericchien/trin v0.0.0

// Dev / monorepo checkout — replace with the path to your trin clone:
replace github.com/ericchien/trin => /Users/eric/sandbox/trin
```

```bash
cd backend && go mod tidy
```

Import alias (required — package name collision with local `package starlight`):

```go
import trinstar "github.com/ericchien/trin/pkg/starlight"
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
- `backend/starlight/trin_scanner.go` — `TrinScanner`, session, real `forward`
- `backend/starlight/scanner_manager.go` — selection order
- `backend/starlight/trin_scanner_test.go` — shapes, path resolution, selection, real-forward (skips if no GGUF)

## Operator note

Unset env → production behavior unchanged (Alpha). Setting a bad or empty GGUF path
logs an error and falls through to Alpha; the process keeps serving scans.

```bash
cd backend
go test ./starlight/ -count=1
STARLIGHT_GGUF=/path/to/starlight.gguf go test ./starlight/ -count=1 -v -run RealForward
```

Trin API reference: `trin/docs/STARLIGHT_INFER.md`.
