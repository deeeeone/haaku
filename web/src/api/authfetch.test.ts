// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { clearSession, readSession, writeSession } from '@/auth/session-store'
import { AuthRequestError, authFetch } from './authfetch'

function json(
  body: unknown,
  status: number,
  headers: Record<string, string> = {}
) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json', ...headers },
  })
}

describe('authFetch', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.stubEnv('VITE_API_URL', 'http://api.test/')
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.unstubAllEnvs()
  })

  it('sends the bearer token explicitly, and nothing when there is none', async () => {
    const fetchMock = vi.fn(
      async (_url: RequestInfo | URL, _init?: RequestInit) =>
        new Response('{}', { status: 200 })
    )
    vi.stubGlobal('fetch', fetchMock)

    await authFetch('v1/admin/users', { token: 'hs_tok' })
    await authFetch('v1/auth/config')

    const first = fetchMock.mock.calls[0]?.[1] as RequestInit
    const second = fetchMock.mock.calls[1]?.[1] as RequestInit
    expect((first.headers as Record<string, string>).authorization).toBe(
      'Bearer hs_tok'
    )
    expect(
      (second.headers as Record<string, string>).authorization
    ).toBeUndefined()
  })

  it('surfaces the machine code and Retry-After on failure', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () =>
        json({ error: { code: 'rate_limited', message: 'slow down' } }, 429, {
          'retry-after': '60',
        })
      )
    )

    const error = await authFetch('v1/auth/local/login', {
      method: 'POST',
      body: {},
    }).catch((e: unknown) => e)

    expect(error).toBeInstanceOf(AuthRequestError)
    expect(error).toMatchObject({
      status: 429,
      code: 'rate_limited',
      retryAfterSeconds: 60,
    })
    expect((error as AuthRequestError).message).toBe('slow down')
  })

  it('drops a stored session when the API answers 401', async () => {
    writeSession({ token: 'hs_stale', user: { id: 'local_1' } as never })
    expect(readSession()).not.toBeNull()

    vi.stubGlobal(
      'fetch',
      vi.fn(async () => json({ error: { code: 'unauthorized' } }, 401))
    )

    await authFetch('v1/admin/users', { token: 'hs_stale' }).catch(() => {})
    expect(readSession()).toBeNull()
  })

  it('leaves an unrelated stored session alone on a non-401 error', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => json({ error: { code: 'not_found' } }, 404))
    )
    writeSession({ token: 'hs_ok', user: { id: 'local_1' } as never })
    await authFetch('v1/admin/users/x/ban', { method: 'POST' }).catch(() => {})
    expect(readSession()).not.toBeNull()
    clearSession()
  })

  it('resolves undefined for an empty 204 body', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response(null, { status: 204 }))
    )
    await expect(
      authFetch('v1/auth/local/logout', { method: 'POST', token: 'x' })
    ).resolves.toBeUndefined()
  })
})
