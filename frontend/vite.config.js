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
    rollupOptions: {
      output: {
        manualChunks: {
          'vendor-react': ['react', 'react-dom', 'react-router-dom', 'scheduler'],
          'vendor-markdown': ['react-markdown', 'remark-gfm'],
          'vendor-ui': ['lucide-react', 'qrcode.react', 'react-hot-toast'],
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
