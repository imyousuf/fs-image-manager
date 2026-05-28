/// <reference types="vitest/config" />
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import { VitePWA } from 'vite-plugin-pwa';

// fs-image-manager web UI.
//
// Production builds output to `web/dist`, which the Go backend embeds via
// `//go:embed` and serves with `http.FileServerFS` (see docs/TECH_SPEC.md §4,14).
// In dev, `/api` is proxied to a locally running `serve` so there is no Go rebuild
// on UI changes.
export default defineConfig({
  plugins: [
    react(),
    VitePWA({
      registerType: 'autoUpdate',
      // The dev SW is opt-in; tests and `vite build` get a real manifest + SW.
      includeAssets: ['favicon.svg'],
      manifest: {
        name: 'fs-image-manager',
        short_name: 'Media',
        description: 'Browse and search your self-hosted media library',
        theme_color: '#0f172a',
        background_color: '#0f172a',
        display: 'standalone',
        start_url: '/',
        icons: [
          { src: 'pwa-192.png', sizes: '192x192', type: 'image/png' },
          { src: 'pwa-512.png', sizes: '512x512', type: 'image/png' },
          {
            src: 'pwa-512.png',
            sizes: '512x512',
            type: 'image/png',
            purpose: 'maskable',
          },
        ],
      },
      workbox: {
        // Never cache the API; only the app shell + static assets.
        navigateFallbackDenylist: [/^\/api/],
        globPatterns: ['**/*.{js,css,html,svg,png,ico,woff2}'],
      },
    }),
  ],
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
  server: {
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
  test: {
    globals: true,
    environment: 'jsdom',
    setupFiles: ['./vitest.setup.ts'],
    css: false,
    // MSW + the service worker registration are irrelevant under jsdom.
    exclude: ['**/node_modules/**', '**/dist/**'],
  },
});
