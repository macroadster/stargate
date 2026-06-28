# Stargate Frontend (Vite)

## CSS Framework

This project uses **QuantumCSS** instead of Tailwind CSS. QuantumCSS is a modern utility-first CSS framework with:

- Utility-first approach similar to Tailwind CSS
- Built-in dark mode support via `dark:` prefix
- Semantic color system
- Cosmic animations and glass effects
- Component presets for rapid development

### Migration Notes

The project was migrated from Tailwind CSS to QuantumCSS. Most utility classes work the same way, with additional compatibility utilities added for a smooth transition.

See `TAILWIND_TO_QUANTUMCSS_MIGRATION.md` for detailed migration information.

## Available Scripts

### `npm start`

Runs the app in development mode at [http://localhost:3000](http://localhost:3000).

### `npm run build`

Builds the app for production to the `build` folder.

### `npm run preview`

Serves the production build locally at [http://localhost:3000](http://localhost:3000).

### `npm test`

Runs the Vitest test runner.

### `npm run test:watch`

Runs Vitest in watch mode.
