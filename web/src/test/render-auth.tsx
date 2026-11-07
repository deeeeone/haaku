import { render } from '@testing-library/react'
import type { ReactNode } from 'react'
import { AuthContext, type AuthContextValue } from '@/auth/context'
import type { AuthConfig } from '@/auth/types'

export const testConfig: AuthConfig = {
  mode: 'local',
  local_auth: true,
  signup_policy: 'approval',
  first_run: false,
  password_min_length: 10,
  password_kdf: 'argon2id',
  password_reset_delivery: 'admin_handover',
  session: { token_prefix: 'hs_', transport: 'bearer', ttl_seconds: 2592000 },
}

export function makeAuthValue(
  overrides: Partial<AuthContextValue> = {}
): AuthContextValue {
  return {
    mode: 'local',
    config: testConfig,
    isLoaded: true,
    isSignedIn: false,
    user: null,
    isAdmin: false,
    getToken: async () => null,
    signOut: async () => {},
    setSession: () => {},
    ...overrides,
  }
}

/** Render a bare screen inside a fake auth context. */
export function renderWithAuth(
  ui: ReactNode,
  overrides: Partial<AuthContextValue> = {}
) {
  const value = makeAuthValue(overrides)
  const result = render(
    <AuthContext.Provider value={value}>{ui}</AuthContext.Provider>
  )
  return { ...result, value }
}
