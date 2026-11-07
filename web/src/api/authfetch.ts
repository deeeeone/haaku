/**
 * JSON fetch helper for the local-auth and admin endpoints.
 *
 * Unlike the generated openapi-fetch client (whose typed responses for these
 * routes are untyped `content?: never`), the auth routes have hand-written
 * response shapes in `@/auth/types`, so this module talks to them with a typed
 * wrapper of its own. The bearer token is passed EXPLICITLY — nothing ambient.
 */

import { ApiError } from '@/api/fetch'
import { clearSessionOnUnauthorized } from '@/auth/session-store'

const BASE_URL = import.meta.env.VITE_API_URL || '/'

/** An API error that also carries the machine code and Retry-After, if any. */
export class AuthRequestError extends ApiError {
  code?: string
  retryAfterSeconds?: number
  serverMessage?: string

  constructor(
    status: number,
    code: string | undefined,
    retryAfterSeconds: number | undefined,
    serverMessage: string | undefined
  ) {
    super(status, serverMessage ?? `Request failed (${status})`, null)
    this.name = 'AuthRequestError'
    this.code = code
    this.retryAfterSeconds = retryAfterSeconds
    this.serverMessage = serverMessage
  }
}

export interface AuthFetchOptions {
  method?: string
  body?: unknown
  token?: string | null
}

function parseRetryAfter(value: string | null): number | undefined {
  if (!value) return undefined
  const seconds = Number.parseInt(value, 10)
  return Number.isFinite(seconds) && seconds >= 0 ? seconds : undefined
}

/**
 * Perform an auth/admin request. Resolves with the parsed JSON body on 2xx
 * (or `undefined` for an empty 204); throws {@link AuthRequestError} otherwise.
 */
export async function authFetch<T>(
  path: string,
  options: AuthFetchOptions = {}
): Promise<T> {
  const { method = 'GET', body, token } = options

  const headers: Record<string, string> = { accept: 'application/json' }
  if (body !== undefined) headers['content-type'] = 'application/json'
  if (token) headers.authorization = `Bearer ${token}`

  const response = await fetch(`${BASE_URL}${path}`, {
    method,
    headers,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })

  if (!response.ok) {
    // A 401 while holding a local token means it was revoked (banned, signed
    // out elsewhere, expired). Drop it so the guard returns to /login.
    clearSessionOnUnauthorized(response)
    const payload = (await response.json().catch(() => null)) as {
      error?: { code?: string; message?: string }
    } | null
    throw new AuthRequestError(
      response.status,
      payload?.error?.code,
      parseRetryAfter(response.headers.get('retry-after')),
      payload?.error?.message
    )
  }

  const text = await response.text()
  if (!text) return undefined as T
  return JSON.parse(text) as T
}
