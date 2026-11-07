// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

// `client.ts` pulls in Clerk (for the `useApi` hook) and sonner (toasts); stub
// both so the module can be exercised headlessly. The toast spy is asserted on,
// so it has to be created before the module under test is imported.
vi.mock('@clerk/react', () => ({
  useAuth: () => ({ getToken: async () => null, isSignedIn: false }),
}))
vi.mock('sonner', () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}))

import { toast } from 'sonner'
import { clearSession, readSession, writeSession } from '@/auth/session-store'
import { ApiError, createApiClient } from './client'

function jsonResponse(body: unknown, status: number) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json' },
  })
}

describe('createApiClient — error normalisation', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    // Node's fetch/Request require an absolute URL; a relative baseUrl (the
    // production default) cannot be constructed outside a browser.
    vi.stubEnv('VITE_API_URL', 'http://api.test')
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.unstubAllEnvs()
  })

  it('normalises an error-envelope response into a typed ApiError', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () =>
        jsonResponse(
          { error: { code: 'not_found', message: 'Movie not found' } },
          404
        )
      )
    )

    const client = createApiClient(async () => null)

    const error = await client
      .GET('/healthz')
      .then(() => null)
      .catch((e: unknown) => e)

    expect(error).toBeInstanceOf(ApiError)
    expect(error).toMatchObject({
      name: 'ApiError',
      status: 404,
      message: 'Movie not found',
    })
    // The raw envelope is preserved for callers that need the error code.
    expect((error as ApiError).body).toEqual({
      error: { code: 'not_found', message: 'Movie not found' },
    })
    expect(toast.error).toHaveBeenCalledWith('Movie not found')
  })

  it('does not toast a 401 — the auth guard owns the redirect', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () =>
        jsonResponse(
          { error: { code: 'unauthorized', message: 'Unauthorized' } },
          401
        )
      )
    )

    const client = createApiClient(async () => null)

    const error = await client
      .GET('/healthz')
      .then(() => null)
      .catch((e: unknown) => e)

    expect(error).toBeInstanceOf(ApiError)
    expect((error as ApiError).status).toBe(401)
    expect(toast.error).not.toHaveBeenCalled()
  })

  it('falls back to a status message when the body carries no message', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response('not json', { status: 500 }))
    )

    const client = createApiClient(async () => null)

    const error = await client
      .GET('/healthz')
      .then(() => null)
      .catch((e: unknown) => e)

    expect(error).toBeInstanceOf(ApiError)
    expect((error as ApiError).message).toBe('Request failed (500)')
    expect(toast.error).toHaveBeenCalledWith('Request failed (500)')
  })

  it('resolves successful responses untouched', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => jsonResponse({ status: 'ok' }, 200))
    )

    const client = createApiClient(async () => null)
    const { data, error } = await client.GET('/healthz')

    expect(error).toBeUndefined()
    expect(data).toEqual({ status: 'ok' })
    expect(toast.error).not.toHaveBeenCalled()
  })

  it('clears a stored local session when a request is answered 401', async () => {
    writeSession({ token: 'hs_stale', user: { id: 'local_1' } as never })
    expect(readSession()).not.toBeNull()

    vi.stubGlobal(
      'fetch',
      vi.fn(async () => jsonResponse({ error: { code: 'unauthorized' } }, 401))
    )

    const client = createApiClient(async () => 'hs_stale')
    await client.GET('/healthz').catch(() => {})

    expect(readSession()).toBeNull()
  })

  it('leaves an unrelated stored session alone on a non-401 error', async () => {
    writeSession({ token: 'hs_ok', user: { id: 'local_1' } as never })

    vi.stubGlobal(
      'fetch',
      vi.fn(async () => jsonResponse({ error: { code: 'not_found' } }, 404))
    )

    const client = createApiClient(async () => 'hs_ok')
    await client.GET('/healthz').catch(() => {})

    expect(readSession()).not.toBeNull()
    clearSession()
  })

  it('injects the bearer token from getToken on every request', async () => {
    const fetchMock = vi.fn(
      async (_input: RequestInfo | URL) => new Response('{}', { status: 200 })
    )
    vi.stubGlobal('fetch', fetchMock)

    const client = createApiClient(async () => 'token-123')
    await client.GET('/healthz')

    expect(fetchMock).toHaveBeenCalledTimes(1)
    const request = fetchMock.mock.calls[0]?.[0] as Request
    expect(request.headers.get('Authorization')).toBe('Bearer token-123')
  })

  it('omits the Authorization header when there is no token', async () => {
    const fetchMock = vi.fn(
      async (_input: RequestInfo | URL) => new Response('{}', { status: 200 })
    )
    vi.stubGlobal('fetch', fetchMock)

    const client = createApiClient(async () => null)
    await client.GET('/healthz')

    const request = fetchMock.mock.calls[0]?.[0] as Request
    expect(request.headers.get('Authorization')).toBeNull()
  })
})
