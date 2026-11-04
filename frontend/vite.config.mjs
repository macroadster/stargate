import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [
    react({
      include: '**/*.{js,jsx,ts,tsx}',
    }),
  ],
  build: {
    outDir: 'build',
    // Lazy routes/modals in App.jsx; vendors split without a catch-all "vendor"
    // chunk (avoids circular vendor ↔ vendor-react edges).
    chunkSizeWarningLimit: 600,
    rolldownOptions: {
      output: {
        codeSplitting: {
          groups: [
            {
              name: 'vendor-react',
              test: /node_modules[\\/](react|react-dom|react-router-dom|scheduler)[\\/]/,
              priority: 30,
            },
            {
              name: 'vendor-markdown',
              test: /node_modules[\\/](react-markdown|remark-gfm)[\\/]/,
              priority: 20,
            },
            {
              // Keep goober with react-hot-toast. Splitting it into the app
              // chunk makes keyframes undefined when the toast module loads.
              name: 'vendor-ui',
              test: /node_modules[\\/](lucide-react|qrcode\.react|react-hot-toast|goober)[\\/]/,
              priority: 20,
            },
          ],
        },
      },
    },
  },
  server: {
    port: 3000,
  },
  preview: {
    port: 3000,
  },
  test: {
    environment: 'jsdom',
    setupFiles: './src/setupTests.js',
    globals: true,
    include: ['src/**/*.test.{js,jsx,ts,tsx}'],
    exclude: ['**/tests/**', '**/node_modules/**'],
  },
});
