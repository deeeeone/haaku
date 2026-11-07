/**
 * Authenticated API client.
 * Uses Clerk for auth token injection. Fails gracefully when not signed in.
 */

import { ApiError, api } from '@/api/fetch'
import { useAuth } from '@/auth/context'

export { ApiError }

const BASE_URL = import.meta.env.VITE_API_URL || '/'

interface RequestOptions extends Omit<RequestInit, 'body'> {
  body?: unknown
  params?: Record<string, string | number | undefined>
}

async function authRequest<T>(
  getToken: () => Promise<string | null>,
  path: string,
  options: RequestOptions = {}
): Promise<T> {
  const { params, body, headers: extraHeaders, ...init } = options

  let url = `${BASE_URL}${path}`
  if (params) {
    const searchParams = new URLSearchParams()
    for (const [key, value] of Object.entries(params)) {
      if (value !== undefined && value !== '') {
        searchParams.set(key, String(value))
      }
    }
    const qs = searchParams.toString()
    if (qs) url += `?${qs}`
  }

  const token = await getToken()
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(extraHeaders as Record<string, string>),
  }
  if (token) {
    headers.Authorization = `Bearer ${token}`
  }

  const response = await fetch(url, {
    ...init,
    headers,
    body: body ? JSON.stringify(body) : undefined,
  })

  if (!response.ok) {
    const errorBody = await response.json().catch(() => null)
    const message =
      errorBody?.error?.message ||
      errorBody?.message ||
      `Request failed (${response.status})`
    throw new ApiError(response.status, message, errorBody)
  }

  const text = await response.text()
  if (!text) return undefined as T
  return JSON.parse(text) as T
}

/**
 * React hook returning an authenticated API client.
 * Returns null when user is not signed in (for conditional rendering).
 */
export function useAuthApi() {
  const { getToken, isSignedIn } = useAuth()

  if (!isSignedIn) return null

  return {
    get: <T>(
      path: string,
      params?: Record<string, string | number | undefined>
    ) => authRequest<T>(getToken, path, { method: 'GET', params }),

    post: <T>(path: string, body?: unknown) =>
      authRequest<T>(getToken, path, { method: 'POST', body }),

    delete: <T>(path: string, body?: unknown) =>
      authRequest<T>(getToken, path, { method: 'DELETE', body }),
  }
}

/**
 * Public API client — no auth required.
 * Used for public pages (search, movie/show detail etc.)
 */
export function usePublicApi() {
  return {
    get: <T>(
      path: string,
      params?: Record<string, string | number | undefined>
    ) => api.get<T>(path, params),
  }
}
