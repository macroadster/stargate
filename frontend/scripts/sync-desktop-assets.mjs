#!/usr/bin/env node
/**
 * Syncs the Starlight Shell's shared dependencies from the installed
 * @howssatoshi/quantumcss package into frontend/public/desktop/.
 *
 * The shell ships as static files, so it cannot resolve node_modules at
 * runtime. Keeping the copies generated here (rather than hand-maintained)
 * means `npm install` is the only thing that updates the framework:
 *
 *   - quantum.min.css  ← dist/quantum.min.css   (framework + icons)
 *   - starlight.js     ← src/starlight.js       (window manager, theme)
 *
 * Shell-specific integration (title escaping, live-install storage listener)
 * lives in shell.html itself, so the synced files stay byte-identical to the
 * package and survive `npm run build` untouched.
 */
import { copyFileSync, statSync } from 'node:fs';
import { createRequire } from 'node:module';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const require = createRequire(import.meta.url);
const here = dirname(fileURLToPath(import.meta.url));
const destDir = resolve(here, '../public/desktop');

const pkgPath = require.resolve('@howssatoshi/quantumcss/package.json');
const pkg = require(pkgPath);
const pkgDir = dirname(pkgPath);

const assets = [
  { src: resolve(pkgDir, 'dist/quantum.min.css'), dest: resolve(destDir, 'quantum.min.css') },
  { src: resolve(pkgDir, 'src/starlight.js'), dest: resolve(destDir, 'starlight.js') },
];

for (const { src, dest } of assets) {
  copyFileSync(src, dest);
  const kb = (statSync(dest).size / 1024).toFixed(1);
  console.log(`synced ${dest.replace(destDir + '/', '')} (${kb} kB) from @howssatoshi/quantumcss@${pkg.version}`);
}
