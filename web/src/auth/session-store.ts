/**
 * Local session token storage.
 *
 * The transport is an `Authorization: Bearer <token>` header — NOT a cookie —
 * so the SPA owns the token and must persist it across reloads. Nothing is sent
 * ambiently: the token is read from here and turned into a header explicitly by
 * the API client, so there is no CSRF surface.
 *
 * A tiny subscriber list lets the auth provider re-render when the token is set
 * (login/pending), cleared (logout) or dropped after the API answers 401.
 */

import type { AuthUser } from './types'

const STORAGE_KEY = 'haaku.local.session'

export interface StoredSession {
  token: string
  user: AuthUser
  expires_at?: string
}

type Listener = (session: StoredSession | null) => void

const listeners = new Set<Listener>()

function hasStorage(): boolean {
  return typeof localStorage !== 'undefined'
}

/** Read the persisted session, or null when absent/malformed. Never throws. */
export function readSession(): StoredSession | null {
  if (!hasStorage()) return null
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return null
    const parsed = JSON.parse(raw) as Partial<StoredSession>
    if (!parsed || typeof parsed.token !== 'string' || !parsed.token) {
      return null
    }
    return {
      token: parsed.token,
      user: (parsed.user ?? null) as AuthUser,
      expires_at: parsed.expires_at,
    }
  } catch {
    return null
  }
}

function emit(): void {
  const current = readSession()
  for (const listener of listeners) listener(current)
}

/** Persist the token + account. The caller has already validated the response. */
export function writeSession(session: StoredSession): void {
  if (!hasStorage()) return
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(session))
  } catch {
    // A full or unavailable store must not crash the login flow.
  }
  emit()
}

/** Drop the token. Called on logout and whenever the API answers 401. */
export function clearSession(): void {
  if (!hasStorage()) return
  try {
    localStorage.removeItem(STORAGE_KEY)
  } catch {
    // ignore
  }
  emit()
}

/**
 * Drop the stored local session when the API answers 401 while a token is
 * held. A 401 means the token was revoked (banned, signed out elsewhere,
 * expired) — retrying it only fails again, so the session is cleared and the
 * auth guard sends the user back to /login.
 *
 * Both frontends' error paths (the auth/admin `authFetch` helper and the
 * generated `createApiClient` response interceptor) call this one function so
 * the two copies of this security-relevant rule cannot drift.
 */
export function clearSessionOnUnauthorized(response: Response): void {
  if (response.status === 401 && readSession()) clearSession()
}

/** Subscribe to set/clear events. Returns an unsubscribe function. */
export function subscribeSession(listener: Listener): () => void {
  listeners.add(listener)
  return () => listeners.delete(listener)
}
