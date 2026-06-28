# Stargate Frontend

React UI for Starlight / Stargate, built with **Vite** and **QuantumCSS** (utility-first, dark mode via `dark:`).

In production the UI is **embedded in the unified `stargate` binary** (`make docker` / release builds). This package is for local UI development and tests.

## Scripts

| Command | Description |
|---------|-------------|
| `npm start` | Dev server at [http://localhost:3000](http://localhost:3000) |
| `npm run build` | Production build (output folder used by embedding / Docker) |
| `npm run preview` | Serve the production build locally |
| `npm test` | Vitest once |
| `npm run test:watch` | Vitest watch mode |

```bash
npm install
npm start
```

Point the UI at a backend with the usual Vite/proxy or `API_BASE` configuration for your environment. User-facing manuals live in [`public/docs/`](./public/docs/) and are served under `/docs` in the app.
