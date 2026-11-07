import createClient from 'openapi-fetch'
import { toast } from 'sonner'
import type { paths } from '@/api/generated/schema'
import { useAuth } from '@/auth/context'
import { clearSessionOnUnauthorized } from '@/auth/session-store'

// Create a typed fetch client from the generated OpenAPI schema.
// Injected into TanStack Query hooks via context — never called directly.

export type ApiClient = ReturnType<typeof createApiClient>

export function createApiClient(getToken: () => Promise<string | null>) {
  const client = createClient<paths>({
    baseUrl: import.meta.env.VITE_API_URL || '/',
  })

  // Supply the bearer token on every request. The token source is abstracted:
  // in clerk mode it is Clerk's getToken, in local mode the stored `hs_` token.
  // Nothing is sent ambiently — no cookie, no implicit credential.
  client.use({
    async onRequest({ request }) {
      const token = await getToken()
      if (token) {
        request.headers.set('Authorization', `Bearer ${token}`)
      }
      return request
    },

    async onResponse({ response }) {
      // Normalize error responses
      if (!response.ok) {
        const body = await response.json().catch(() => null)
        const message =
          body?.error?.message ||
          body?.message ||
          `Request failed (${response.status})`

        // Don't toast 401 — let the auth guard handle redirects
        if (response.status !== 401) {
          toast.error(message)
        }

        // A 401 while holding a local token means the session was revoked
        // (banned, logged out elsewhere, expired). Drop it so the guard sends
        // the user back to /login instead of retrying a dead token forever.
        clearSessionOnUnauthorized(response)

        throw new ApiError(response.status, message, body)
      }

      return response
    },
  })

  return client
}

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

// React hook to get a typed client. The token source comes from the shared auth
// context, so the same hook serves both clerk and local modes.
export function useApi(): ApiClient | null {
  const { getToken, isSignedIn } = useAuth()
  if (!isSignedIn) return null
  return createApiClient(getToken)
}
