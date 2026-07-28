import { fileURLToPath, URL } from 'node:url';
import { defineConfig } from 'vite';
import vue from '@vitejs/plugin-vue';

// Assets are served by the Go binary under /app/ (embedded at build time);
// the SPA index.html is served for whichever page routes have been cut over.
export default defineConfig({
  base: '/app/',
  plugins: [vue()],
  resolve: {
    alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) },
  },
  build: {
    outDir: '../cmd/junkie/static/app',
    emptyOutDir: true,
  },
  // `npm test` (vitest run). jsdom gives the DOM and localStorage the app code
  // reaches for; anything browser-only that jsdom lacks — Notification, service
  // workers — is stubbed per test, since faking those is precisely the point.
  test: {
    environment: 'jsdom',
    include: ['src/**/*.test.js'],
    restoreMocks: true,
  },
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://localhost:8080',
      '/avatar': 'http://localhost:8080',
      '/manifest.webmanifest': 'http://localhost:8080',
      '/assets': 'http://localhost:8080',
      '/ws': { target: 'ws://localhost:8080', ws: true },
    },
  },
});
