# Trin / GGUF Starlight Scanner (Go)

Workstream 3 wires a **Trin/GGUF-backed** Starlight detector behind the existing
`core.StarlightScannerInterface` in the Stargate Go backend (`backend/starlight`).

No Python runs on the scan path.

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

Produce `starlight.gguf` via the Trin export pipeline once available; point either env at that file and restart the Stargate process.

## Fallback selection chain

`ScannerManager.InitializeScanner` → `tryInitScanners()`:

1. **Trin** (`scannerType = "trin-gguf"`) — only if env path is non-empty **and** `NewTrinScanner(path).Initialize()` succeeds (file exists, GGUF magic/version valid).
2. **Alpha** (`"alpha"`) — Go-native alpha LSB scanner (default when env unset or Trin init fails). Trin failures are logged and **do not** hard-fail the process.
3. **Mock** (`"mock"`) — last resort if Alpha init fails.

## What is real vs stubbed

| Component | Status |
|-----------|--------|
| `LoadUnifiedInput` multi-stream preprocess (pixel/meta/alpha/lsb/palette/features) | **Real** — Go port of `scripts/starlight_utils.py` `load_unified_input` |
| JPEG APP1 EXIF + post-image tail (`extract_post_tail`) | **Real** (EXIF: JPEG only; PNG/etc. usually empty EXIF) |
| Minimal GGUF open (magic `GGUF`, version 2/3, tensor/kv counts) | **Real** — validates file; no full dequant |
| `ForwardStarlight` neural inference | **Stubbed** — returns neutral logits (stego logit 0 → prob 0.5) |
| Alpha message extract on scan | **Real** — delegates to `stego.ExtractAlpha` when method is alpha/auto |
| Other extract methods (lsb/palette/exif/eoi) | MVP not-found |

## Unified input layout

Matches the Python training/inference bundle (CHW-ready floats):

- `Pixel` `(3,256,256)` ∈ [0,1]
- `Meta` `(2048,)` = pad/truncate(exif+tail)/255
- `Alpha` `(2,256,256)` full α/255 + α LSB
- `LSB` `(3,256,256)` RGB LSB 0/1 CHW
- `Palette` `(768,)` /255
- `PaletteLSB` `(1,256,256)`
- `FormatFeatures` `(6,)` = has_alpha, alpha_std, is_palette, is_rgb, w/256, h/256
- `ContentFeatures` `(6,)` = lsb_content(3) + alpha_content(3)

**Center crop:** if both dims ≥ 256, center crop; if smaller, zero-pad out-of-bounds (black) then center — documented near-equivalent to torchvision `CenterCrop` pad behavior.

## Drop-in path when Trin emit-go lands

1. Export / generate Go inference package from Trin (e.g. `starlight_infer` or emit-go output).
2. Replace the body of `ForwardStarlight` in `backend/starlight/trin_scanner.go` to call the generated forward, mapping `UnifiedInput` tensors onto the model inputs.
3. Keep `OpenMinimalGGUF` or switch to the generated weight loader; **do not** import `trin/internal/*` from Stargate.
4. Re-run:

   ```bash
   cd backend && go test ./starlight/ -count=1
   ```

5. Point `STARLIGHT_GGUF` at the real `starlight.gguf` and verify `GetScannerType() == "trin-gguf"` plus non-neutral probabilities on known stego fixtures.

## Package map

- `backend/starlight/unified_input.go` — preprocess
- `backend/starlight/trin_scanner.go` — `TrinScanner`, GGUF open, `ForwardStarlight` stub
- `backend/starlight/scanner_manager.go` — selection order
- `backend/starlight/trin_scanner_test.go` — shapes, path resolution, selection

## Operator note

Unset env → production behavior unchanged (Alpha). Setting a bad GGUF path logs an error and falls through to Alpha; the process keeps serving scans.
