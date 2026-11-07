/**
 * Mode discovery — the first thing the SPA does at boot.
 *
 * `GET /v1/auth/config` is registered in BOTH modes, and its `mode` field is the
 * single source of truth for whether the local flow or Clerk is rendered:
 *   - `"clerk"`: today's ClerkProvider/useAuth path, unchanged.
 *   - `"local"`: the local signup/login flow; the Clerk provider is inert.
 */

import type { AuthConfig } from './types'

export const DEFAULT_PASSWORD_MIN_LENGTH = 10

/** Coarse fallback used only when the config endpoint is unreachable. */
export function fallbackConfig(
  clerkConfigured: boolean
): Pick<AuthConfig, 'mode' | 'password_min_length' | 'signup_policy'> {
  return {
    mode: clerkConfigured ? 'clerk' : 'local',
    password_min_length: DEFAULT_PASSWORD_MIN_LENGTH,
    signup_policy: 'approval',
  }
}

function normalize(raw: unknown): AuthConfig {
  const value = (raw ?? {}) as Partial<AuthConfig>
  const session = (value.session ?? {}) as Partial<AuthConfig['session']>
  return {
    mode: value.mode === 'clerk' ? 'clerk' : 'local',
    local_auth: Boolean(value.local_auth),
    signup_policy:
      value.signup_policy === 'open' || value.signup_policy === 'closed'
        ? value.signup_policy
        : 'approval',
    first_run: Boolean(value.first_run),
    password_min_length:
      typeof value.password_min_length === 'number' &&
      value.password_min_length > 0
        ? value.password_min_length
        : DEFAULT_PASSWORD_MIN_LENGTH,
    password_kdf: value.password_kdf ?? 'argon2id',
    password_reset_delivery: value.password_reset_delivery ?? 'admin_handover',
    session: {
      token_prefix: session.token_prefix ?? 'hs_',
      transport: session.transport ?? 'bearer',
      ttl_seconds: session.ttl_seconds ?? 0,
    },
  }
}

/**
 * Fetch the instance's identity config. Throws on a non-2xx response so the
 * caller can decide whether to degrade to {@link fallbackConfig}.
 */
export async function fetchAuthConfig(
  baseUrl = import.meta.env.VITE_API_URL || '/'
): Promise<AuthConfig> {
  const response = await fetch(`${baseUrl}v1/auth/config`, {
    headers: { accept: 'application/json' },
  })
  if (!response.ok) {
    throw new Error(`auth config request failed (${response.status})`)
  }
  return normalize(await response.json())
}
