/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_API_URL: string
  readonly VITE_CLERK_PUBLISHABLE_KEY: string
  /** Optional Clerk frontend-API proxy (dev clerk-emulator / self-hosted FAPI). */
  readonly VITE_CLERK_PROXY_URL?: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}
