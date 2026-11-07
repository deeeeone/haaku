/**
 * The auth context consumed by the whole app.
 *
 * THIS is the abstraction requirement 3 asks for: consumers (the API client,
 * the sidebar, the shell, route guards) read `useAuth()` and never Clerk's
 * `useAuth`/`useClerk` directly. In clerk mode the value is fed by Clerk hooks
 * inside `<AuthProvider>`; in local mode it is fed by the session store. The
 * provider only calls Clerk hooks when a ClerkProvider is actually mounted, so
 * this module stays free of Clerk imports and can be pulled in by `client.ts`
 * (and by tests) without a ClerkProvider present.
 */

import { createContext, useContext } from 'react'
import type { StoredSession } from './session-store'
import type { AuthConfig, AuthMode, AuthUser } from './types'

export { DEFAULT_PASSWORD_MIN_LENGTH } from './config'

export interface AuthContextValue {
  mode: AuthMode
  config: AuthConfig | null
  /** False until mode discovery has settled. */
  isLoaded: boolean
  isSignedIn: boolean
  user: AuthUser | null
  isAdmin: boolean
  /** Resolve the bearer token for a request, or null when signed out. */
  getToken: () => Promise<string | null>
  /** Revoke the session (local: POST logout + discard) and clear state. */
  signOut: () => Promise<void>
  /** Local only: adopt a new session after login/signup. No-op in clerk mode. */
  setSession: (session: StoredSession | null) => void
}

export const AuthContext = createContext<AuthContextValue | null>(null)

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext)
  if (!ctx) {
    throw new Error('useAuth must be used inside <AuthProvider>')
  }
  return ctx
}

/** Convenience: just the discovered config (may be null while loading). */
export function useAuthConfig(): AuthConfig | null {
  return useAuth().config
}
