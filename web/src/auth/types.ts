/**
 * Identity types shared by the mode-discovery, the local session store and the
 * screens. Kept dependency-free so `client.ts` can import them without pulling
 * Clerk (or React) into its graph.
 */

export type AuthMode = 'local' | 'clerk'

export type SignupPolicy = 'approval' | 'open' | 'closed'

export interface AuthSessionConfig {
  token_prefix: string
  transport: string
  ttl_seconds: number
}

/**
 * The body of `GET /v1/auth/config`. This endpoint is registered in BOTH modes,
 * so it is the only thing the SPA may call before it knows how it authenticates.
 */
export interface AuthConfig {
  mode: AuthMode
  local_auth: boolean
  signup_policy: SignupPolicy
  first_run: boolean
  password_min_length: number
  password_kdf: string
  password_reset_delivery: string
  session: AuthSessionConfig
}

/** The account shape the API returns for local users (`local_`-prefixed). */
export interface AuthUser {
  id: string
  email: string | null
  username: string | null
  is_admin?: boolean
  status?: string
  created_at?: string | null
  last_active_at?: string | null
  /** Clerk-only: avatar URL. Absent for local accounts. */
  imageUrl?: string | null
  /** Clerk-only: human display name, preferred over username when present. */
  displayName?: string | null
}

/** What a local signup/login hands back; `session` is null while pending. */
export interface LocalSession {
  token: string
  token_type?: string
  expires_at?: string
}

export interface LocalSignupResult {
  user: AuthUser
  status: string
  session: LocalSession | null
  message?: string
}

export interface LocalLoginResult {
  user: AuthUser
  session: LocalSession | null
}
