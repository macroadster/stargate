# Starlight Shell (`/desktop`)

Static copy of the Starlight Shell desktop environment, integrated into the
stargate frontend. It occupies the whole viewport at the `/desktop` route
(see `src/pages/DesktopPage.jsx`) — the shell's own menu bar is the only
chrome, and its brand menu carries the way back to Starlight.

## Latest framework assets

`quantum.min.css` and `starlight.js` are **not** hand-maintained copies.
They are generated from the installed `@howssatoshi/quantumcss` package by
`npm run sync:desktop` (runs automatically via `prebuild` and `prestart`):

- `quantum.min.css` ← `dist/quantum.min.css`
- `starlight.js`    ← `src/starlight.js` (window manager, theme bootstrap)

Do not edit those two files here; bump the package instead and re-sync.
Shell-specific integration lives in `shell.html` itself:

- `windowManager.createWindow` is wrapped to escape window title/subtitle
  before the (HTML-interpolating) chrome renders them. Upstream stays
  byte-identical to the package. The wrapper also owns placement, because
  the upstream cascade counts windows ever created instead of windows open
  (restored windows bump it too), so new windows could stack exactly on top
  of open ones. Explicit numeric `left`/`top` wins; `center: true` centers
  the window in the stack (used by About Starlight); otherwise the wrapper
  cascades off the live open-window set, skipping occupied slots.
- A `storage` listener re-reads `confirmedContracts` / `dockPins` /
  `stackPins` on change, so installs made by the stargate **Get** button
  (or in another tab) appear without reloading the shell.
- The brand menu gains **Back to Starlight** (`backToStarlight()`), which
  navigates the top window — the whole tab leaves the shell for the
  Starlight app (works both standalone at `/desktop/shell.html` and inside
  the app's iframe).

## Source of truth

`~/sandbox/shell` (standalone checkout). When the shell evolves there, port
the relevant changes into `shell.html` and re-run `npm run build`.

Differences from the standalone copy:

- `shell.html` derives `API_BASE` from `window.location.origin` (local dev on
  `:3000` targets the Go backend on `:3001`) instead of pointing at the
  public deployment, so the shell talks to the stargate node that serves it
  and same-origin `httpOnly` cookie auth applies automatically.
- The `+ Add Hash` menu item and its dialog were removed. Contracts are
  installed from the stargate UI instead: **Smart Contract Details → Get**
  writes a `confirmedContracts` entry the shell renders on boot and live.
- Contract names and URLs are escaped/encoded at the render sites they flow
  into (desktop icons, dock, side-stack, launcher, task list, events,
  wallpaper). Chain-derived metadata is not trusted markup.
- Dead `icon-atom`/`icon-info` glyphs map to `icon-starlight` (the vendored
  framework never shipped them).
- Build-time only files (`starlight_sdk.sh`, `vault-*`, `.playwright-mcp/`)
  are not part of this integration.

`stargate.shell.pendingInstall` (localStorage) is set by the Get button so a
paired Quantum Sync client broadcasts the new desktop entry on next boot.
