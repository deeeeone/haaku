import path from 'node:path'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': path.resolve(import.meta.dirname, './src'),
      // Point design-kit imports to the actual design-kit source
      '@design-kit': path.resolve(import.meta.dirname, '../design-kit/src'),
    },
  },
  server: {
    port: 5173,
    open: true,
  },
  css: {
    postcss: './postcss.config.mjs',
  },
})
