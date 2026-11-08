// @vitest-environment happy-dom

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook } from '@testing-library/react'
import type { ReactNode } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const { GET, toastError } = vi.hoisted(() => ({
  GET: vi.fn(),
  toastError: vi.fn(),
}))

vi.mock('@/api/client', () => ({ useApi: () => ({ GET }) }))
vi.mock('sonner', () => ({ toast: { error: toastError, success: vi.fn() } }))

import { useResolveMedia } from './useSearch'

function wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>
}

describe('useResolveMedia', () => {
  beforeEach(() => {
    GET.mockReset()
    toastError.mockReset()
  })

  // openapi-fetch resolves on 4xx/5xx; it does not throw. A resolve that
  // "succeeds" with no local id used to navigate to /movies/ and watchlist
  // media_id 0, so the error has to be raised here.
  it('rejects when the API returns an error', async () => {
    GET.mockResolvedValue({
      data: undefined,
      error: { error: { code: 'not_found', message: 'no such title' } },
    })

    const { result } = renderHook(() => useResolveMedia(), { wrapper })

    await expect(
      result.current.mutateAsync({ type: 'show', id: '999999999' })
    ).rejects.toThrow()
    expect(toastError).toHaveBeenCalled()
  })

  it('rejects when the response carries no local id', async () => {
    GET.mockResolvedValue({
      data: { title: 'Nothing', ids: { trakt: 0, slug: '' } },
    })

    const { result } = renderHook(() => useResolveMedia(), { wrapper })

    await expect(
      result.current.mutateAsync({ type: 'show', id: '1396' })
    ).rejects.toThrow()
  })

  it('returns the local ids on success', async () => {
    GET.mockResolvedValue({
      data: {
        title: 'Severance',
        ids: {
          id: 1,
          trakt: 1,
          slug: 'severance',
          tmdb: 95396,
          tvdb: '371980',
        },
      },
    })

    const { result } = renderHook(() => useResolveMedia(), { wrapper })

    const resolved = await result.current.mutateAsync({
      type: 'show',
      id: '95396',
    })
    expect(resolved.ids.trakt).toBe(1)
    expect(resolved.title).toBe('Severance')
  })

  // The default id_type follows the provider that is primary. A live search hit
  // carries a TMDB id, so a caller that does not pass idType must resolve by
  // TMDB — sending the id under the wrong id_type would ingest a DIFFERENT
  // title (the two providers number their records in unrelated spaces).
  it('defaults id_type to tmdb', async () => {
    GET.mockResolvedValue({
      data: {
        title: 'Severance',
        ids: { id: 1, trakt: 1, slug: 'severance' },
      },
    })

    const { result } = renderHook(() => useResolveMedia(), { wrapper })
    await result.current.mutateAsync({ type: 'show', id: '95396' })

    expect(GET).toHaveBeenCalledWith(
      '/v1/media/resolve',
      expect.objectContaining({
        params: { query: { type: 'show', id: '95396', id_type: 'tmdb' } },
      })
    )
  })

  // A TVDB-keyed caller (a media server, or an older client) can still say so.
  it('still accepts an explicit idType', async () => {
    GET.mockResolvedValue({
      data: { title: 'Severance', ids: { id: 1, trakt: 1, slug: 'severance' } },
    })

    const { result } = renderHook(() => useResolveMedia(), { wrapper })
    await result.current.mutateAsync({
      type: 'show',
      id: '371980',
      idType: 'tvdb',
    })

    expect(GET).toHaveBeenCalledWith(
      '/v1/media/resolve',
      expect.objectContaining({
        params: { query: { type: 'show', id: '371980', id_type: 'tvdb' } },
      })
    )
  })
})
