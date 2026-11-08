// @vitest-environment happy-dom

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'

// `useSearch` pulls in `useApi` (Clerk + sonner) through the resolve mutation.
// Stub both so the hook can be exercised headlessly, like the client tests do.
vi.mock('@clerk/react', () => ({
  useAuth: () => ({ getToken: async () => null, isSignedIn: false }),
}))
vi.mock('sonner', () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}))

import { useSearch } from './useSearch'

function wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>
}

describe('useSearch — request contract', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.unstubAllEnvs()
  })

  // /v1/search reads `?query=`; the handler answers 400 "query parameter is
  // required" for anything else. The hook used to send `?q=`, which is what
  // made the search page look broken with a valid TVDB token configured.
  it('sends the term as `query`, not `q`', async () => {
    vi.stubEnv('VITE_API_URL', 'http://api.test')
    const fetchMock = vi.fn(
      async () =>
        new Response('[]', {
          status: 200,
          headers: { 'content-type': 'application/json' },
        })
    )
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useSearch({ q: 'severance' }), {
      wrapper,
    })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    const url = String(fetchMock.mock.calls[0]?.[0] ?? '')
    expect(url).toContain('query=severance')
    expect(url).not.toMatch(/[?&]q=/)
  })
})
