import createClient from 'openapi-fetch'
import { useAuth } from '@clerk/clerk-react'
import { toast } from 'sonner'
import type { paths } from '@/api/generated/schema'

// Create a typed fetch client from the generated OpenAPI schema.
// Injected into TanStack Query hooks via context — never called directly.

export type ApiClient = ReturnType<typeof createApiClient>

export function createApiClient(getToken: () => Promise<string | null>) {
  const client = createClient<paths>({
    baseUrl: import.meta.env.VITE_API_URL,
  })

  // Inject Clerk auth token on every request
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

// React hook to get a typed client (re-creates when token changes)
export function useApi(): ApiClient | null {
  const { getToken, isSignedIn } = useAuth()
  if (!isSignedIn) return null
  return createApiClient(() => getToken())
}