# Quantum Shell (`/desktop`)

Self-contained copy of the Quantum Shell desktop environment, integrated into
the stargate frontend. The starlight nav stays visible above it at the
`/desktop` route (see `src/pages/DesktopPage.jsx`); the files here are served
verbatim by Vite at `/desktop/shell.html`.

Source of truth: `~/sandbox/shell` (standalone checkout). When the shell
evolves there, port the relevant changes into this directory and re-run
`npm run build`.

Differences from the standalone copy:

- `shell.html` derives `API_BASE` from `window.location.origin` (local dev on
  `:3000` targets the Go backend on `:3001`) instead of pointing at the
  public deployment, so the shell talks to the stargate node that serves it
  and same-origin `httpOnly` cookie auth applies automatically.
- The `+ Add Hash` menu item and its dialog were removed. Contracts are
  installed from the stargate UI instead: **Smart Contract Details → Get**
  writes a `confirmedContracts` entry the shell renders on boot.
- Contract names and URLs are escaped/encoded at the render sites they flow
  into (desktop icons, dock, side-stack, launcher, task list, window chrome,
  wallpaper). Chain-derived metadata is not trusted markup.
- Build-time only files (`starlight_sdk.sh`, `vault-*`, `.playwright-mcp/`)
  are not part of this integration.

`stargate.shell.pendingInstall` (localStorage) is set by the Get button so a
paired Quantum Sync client broadcasts the new desktop entry on next boot.
