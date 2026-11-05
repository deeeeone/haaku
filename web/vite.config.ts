import path from 'node:path'
import react from '@vitejs/plugin-react'
import { VitePWA } from 'vite-plugin-pwa'
import { defineConfig } from 'vitest/config'

export default defineConfig({
  plugins: [
    react(),
    VitePWA({
      registerType: 'autoUpdate',
      includeAssets: ['icon-192.svg', 'icon-512.svg'],
      manifest: {
        name: 'TKiller',
        short_name: 'TKiller',
        description: 'Track what you watch — movies, shows, and more',
        theme_color: '#050711',
        background_color: '#050711',
        display: 'standalone',
        start_url: '/',
        orientation: 'portrait-primary',
        icons: [
          {
            src: '/icon-192.svg',
            sizes: '192x192',
            type: 'image/svg+xml',
            purpose: 'any maskable',
          },
          {
            src: '/icon-512.svg',
            sizes: '512x512',
            type: 'image/svg+xml',
            purpose: 'any maskable',
          },
        ],
      },
      workbox: {
        globPatterns: ['**/*.{js,css,html,svg,png,woff2,json}'],
        runtimeCaching: [
          {
            urlPattern: /\.(?:js|css|html|svg|png|woff2|json)$/,
            handler: 'CacheFirst',
            options: {
              cacheName: 'static-assets',
              expiration: {
                maxEntries: 200,
                maxAgeSeconds: 30 * 24 * 60 * 60, // 30 days
              },
            },
          },
          {
            urlPattern: /\/api\//,
            handler: 'NetworkFirst',
            options: {
              cacheName: 'api-responses',
              networkTimeoutSeconds: 10,
              expiration: {
                maxEntries: 100,
                maxAgeSeconds: 60 * 60, // 1 hour
              },
            },
          },
        ],
      },
    }),
  ],
  resolve: {
    alias: {
      '@': path.resolve(import.meta.dirname, './src'),
      // The design kit is vendored under src/design-kit-components/. The old
      // `@design-kit` alias pointed at a sibling `../design-kit/src` checkout
      // that does not exist in this repository and that nothing imports, so it
      // is gone rather than silently resolving to the vendored copy — see the
      // "Known gaps" section of README.md.
    },
  },
  server: {
    port: 5173,
    open: false,
    // Dev only: the SPA calls the API on relative paths, so the dev server
    // forwards the API prefixes to the Go server (make dev-backend). Override
    // the target when the backend does not listen on :8080.
    proxy: {
      '/v1': {
        target: process.env.VITE_API_PROXY_TARGET ?? 'http://localhost:8080',
        changeOrigin: true,
      },
      '/oauth': {
        target: process.env.VITE_API_PROXY_TARGET ?? 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
  css: {
    postcss: './postcss.config.mjs',
  },
  // Vitest. Unit tests must not accidentally pick up the Playwright specs in
  // e2e/ (they import @playwright/test and need a browser), so exclude them
  // explicitly instead of relying on the default glob.
  test: {
    environment: 'node',
    include: ['src/**/*.{test,spec}.{ts,tsx}'],
    exclude: ['e2e/**', 'node_modules/**', 'dist/**'],
  },
})
