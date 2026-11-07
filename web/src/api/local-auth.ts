/**
 * Typed client for the local-auth endpoints (PR #73 contract).
 *
 * Every call is explicit and stateless — the caller owns the session token and
 * passes it in; nothing is attached ambiently.
 */

import { authFetch } from '@/api/authfetch'
import type { LocalLoginResult, LocalSignupResult } from '@/auth/types'

export interface SignupInput {
  email: string
  password: string
  username?: string
}

export interface LoginInput {
  email: string
  password: string
}

/**
 * `POST /v1/auth/local/signup` → 201 `{user,status,session|null,message?}`.
 * On a fresh instance (`first_run`) the created account is the active owner.
 * Otherwise it is `pending` with a null session until an admin approves it.
 */
export function signup(input: SignupInput): Promise<LocalSignupResult> {
  return authFetch<LocalSignupResult>('v1/auth/local/signup', {
    method: 'POST',
    body: input,
  })
}

/** `POST /v1/auth/local/login` → 200 `{user,session}`. */
export function login(input: LoginInput): Promise<LocalLoginResult> {
  return authFetch<LocalLoginResult>('v1/auth/local/login', {
    method: 'POST',
    body: input,
  })
}

/** `POST /v1/auth/local/logout` (bearer) → 204. Discard the token client-side. */
export function logout(token: string | null): Promise<void> {
  return authFetch<void>('v1/auth/local/logout', { method: 'POST', token })
}

/** `POST /v1/auth/local/password-reset` → 200 `{status:"ok"}` / 400. */
export function consumePasswordReset(
  token: string,
  newPassword: string
): Promise<{ status: string }> {
  return authFetch<{ status: string }>('v1/auth/local/password-reset', {
    method: 'POST',
    body: { token, new_password: newPassword },
  })
}
