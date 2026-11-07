/**
 * `<AuthProvider>` — mode discovery + the one place that touches Clerk.
 *
 * Boot order (requirement 1):
 *   1. `GET /v1/auth/config` is fetched once, before anything renders.
 *   2. `mode:"clerk"` mounts the EXACT ClerkProvider that shipped before and
 *      feeds the shared context from Clerk's hooks — unchanged behaviour.
 *   3. `mode:"local"` mounts no ClerkProvider at all; the context is fed by the
 *      local session store, so Clerk is inert.
 *
 * If the config request fails we degrade to a heuristic (Clerk if a publishable
 * key is built in, otherwise local) rather than white-screening the app.
 */

import { ClerkProvider, useAuth, useClerk } from '@clerk/react'
import {
  type PropsWithChildren,
  useCallback,
  useEffect,
  useMemo,
  useState,
} from 'react'
import { logout } from '@/api/local-auth'
import { fallbackConfig, fetchAuthConfig } from './config'
import { AuthContext, type AuthContextValue } from './context'
import {
  clearSession,
  readSession,
  subscribeSession,
  writeSession,
} from './session-store'
import type { AuthConfig, AuthUser } from './types'

const clerkPubKey = import.meta.env.VITE_CLERK_PUBLISHABLE_KEY
const clerkConfigured = Boolean(
  clerkPubKey && !clerkPubKey.includes('YOUR_CLERK')
)
const clerkProxyUrl = import.meta.env.VITE_CLERK_PROXY_URL || undefined

/** Feed the shared context from Clerk — only ever mounted under ClerkProvider. */
function ClerkAuthBridge({
  config,
  children,
}: PropsWithChildren<{ config: AuthConfig | null }>) {
  const { isSignedIn, getToken } = useAuth()
  const { user, signOut } = useClerk()

  const mappedUser: AuthUser | null = user
    ? {
        id: user.id,
        email: user.primaryEmailAddress?.emailAddress ?? null,
        username: user.username ?? null,
        displayName: user.fullName ?? null,
        imageUrl: user.imageUrl ?? null,
        is_admin: false,
      }
    : null

  const value: AuthContextValue = useMemo(
    () => ({
      mode: 'clerk',
      config,
      isLoaded: true,
      isSignedIn: Boolean(isSignedIn),
      user: mappedUser,
      isAdmin: false,
      getToken: () => getToken(),
      signOut: async () => {
        await signOut()
      },
      // Clerk owns its own session lifecycle; these are inert here.
      setSession: () => {},
    }),
    [config, isSignedIn, mappedUser, getToken, signOut]
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

/** Feed the shared context from the local session store. */
function LocalAuthBridge({
  config,
  children,
}: PropsWithChildren<{ config: AuthConfig | null }>) {
  const [session, setSessionState] = useState(() => readSession())

  useEffect(() => subscribeSession(setSessionState), [])

  const getToken = useCallback(async () => readSession()?.token ?? null, [])

  const signOut = useCallback(async () => {
    const token = readSession()?.token ?? null
    try {
      await logout(token)
    } catch {
      // Revocation is best-effort; the client token is discarded regardless.
    }
    clearSession()
  }, [])

  const setSession = useCallback((next: ReturnType<typeof readSession>) => {
    if (next) writeSession(next)
    else clearSession()
  }, [])

  const value: AuthContextValue = useMemo(
    () => ({
      mode: 'local',
      config,
      isLoaded: true,
      isSignedIn: Boolean(session),
      user: session?.user ?? null,
      isAdmin: Boolean(session?.user?.is_admin),
      getToken,
      signOut,
      setSession,
    }),
    [config, session, getToken, signOut, setSession]
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function AuthProvider({ children }: PropsWithChildren) {
  const [config, setConfig] = useState<AuthConfig | null>(null)

  useEffect(() => {
    let cancelled = false
    fetchAuthConfig()
      .then((next) => {
        if (!cancelled) setConfig(next)
      })
      .catch(() => {
        // Degrade instead of blocking the whole app on a transient outage.
        if (!cancelled) {
          setConfig({
            local_auth: !clerkConfigured,
            first_run: false,
            password_kdf: 'argon2id',
            password_reset_delivery: 'admin_handover',
            session: {
              token_prefix: 'hs_',
              transport: 'bearer',
              ttl_seconds: 0,
            },
            ...fallbackConfig(clerkConfigured),
          })
        }
      })
    return () => {
      cancelled = true
    }
  }, [])

  if (!config) {
    return (
      <div className="grid min-h-screen place-items-center text-sm text-sf-ink-muted">
        Loading…
      </div>
    )
  }

  if (config.mode === 'clerk' && clerkConfigured) {
    return (
      <ClerkProvider
        publishableKey={clerkPubKey}
        proxyUrl={clerkProxyUrl}
        afterSignOutUrl="/"
        appearance={{
          variables: {
            colorPrimary: '#8c6bff',
            colorBackground: '#0c1122',
            colorForeground: '#f8fbff',
            colorMutedForeground: 'rgba(248, 251, 255, 0.66)',
            colorInput: 'rgba(255, 255, 255, 0.072)',
            colorInputForeground: '#f8fbff',
            borderRadius: '18px',
          },
        }}
      >
        <ClerkAuthBridge config={config}>{children}</ClerkAuthBridge>
      </ClerkProvider>
    )
  }

  return <LocalAuthBridge config={config}>{children}</LocalAuthBridge>
}
