/**
 * Thin fetch wrapper for the trakt-killer API.
 * Used when the generated openapi-fetch client doesn't have typed response schemas.
 * Handles base URL, JSON parsing, and error normalization.
 */

const BASE_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080'

export class ApiError extends Error {
  status: number
  body: unknown

  constructor(status: number, message: string, body: unknown) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.body = body
  }
}

interface RequestOptions extends Omit<RequestInit, 'body'> {
  body?: unknown
  params?: Record<string, string | number | undefined>
}

async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const { params, body, headers: extraHeaders, ...init } = options

  // Build URL with query params
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

  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(extraHeaders as Record<string, string>),
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

  // Handle empty responses (204, etc.)
  const text = await response.text()
  if (!text) return undefined as T
  return JSON.parse(text) as T
}

// Convenience methods
export const api = {
  get<T>(path: string, params?: Record<string, string | number | undefined>) {
    return request<T>(path, { method: 'GET', params })
  },
  post<T>(path: string, body?: unknown) {
    return request<T>(path, { method: 'POST', body })
  },
  put<T>(path: string, body?: unknown) {
    return request<T>(path, { method: 'PUT', body })
  },
  delete<T>(path: string, body?: unknown) {
    return request<T>(path, { method: 'DELETE', body })
  },
}
