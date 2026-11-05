/**
 * Search hooks — powered by TanStack Query
 */

import { useQuery } from '@tanstack/react-query'
import { api } from '@/api/fetch'
import type {
  Movie,
  SearchResponse,
  SearchResult,
  SearchType,
  Show,
} from '@/api/types'

// ─── Keys ──────────────────────────────────────────────────────────────

export const searchKeys = {
  all: ['search'] as const,
  search: (params: SearchParams) => ['search', 'results', params] as const,
  exact: (q: string) => ['search', 'exact', q] as const,
  lookup: (idType: string, id: string) =>
    ['search', 'lookup', idType, id] as const,
  autocomplete: (q: string) => ['search', 'autocomplete', q] as const,
}

// ─── Params ────────────────────────────────────────────────────────────

export interface SearchParams {
  q: string
  type?: SearchType
  page?: number
  limit?: number
  genres?: string
  years?: string
}

// ─── Hooks ─────────────────────────────────────────────────────────────

/**
 * Full-text search with filters.
 * Debounced at the call site (useSearchBar handles 300ms debounce).
 */
export function useSearch(params: SearchParams | null) {
  return useQuery({
    queryKey: searchKeys.search(params ?? { q: '' }),
    queryFn: async () => {
      if (!params?.q)
        return {
          data: [],
          page: 1,
          limit: 20,
          total: 0,
          total_pages: 0,
        } as SearchResponse
      const result = await api.get<SearchResult[]>('/v1/search', {
        q: params.q,
        type: params.type,
        page: params.page ?? 1,
        limit: params.limit ?? 20,
      })
      // Normalize: API may return raw array or paginated wrapper
      if (Array.isArray(result)) {
        return {
          data: result,
          page: params.page ?? 1,
          limit: params.limit ?? 20,
          total: result.length,
          total_pages: 1,
        } as SearchResponse
      }
      return result as unknown as SearchResponse
    },
    enabled: !!params?.q && params.q.length > 0,
    staleTime: 30_000,
  })
}

/**
 * Exact title match for movies/shows.
 */
export function useExactSearch(q: string) {
  return useQuery({
    queryKey: searchKeys.exact(q),
    queryFn: () => api.get<Movie | Show | null>('/v1/search/exact', { q }),
    enabled: q.length > 0,
    staleTime: 60_000,
  })
}

/**
 * Look up media by external ID (IMDB, TMDB, etc.)
 */
export function useLookup(idType: string, id: string) {
  return useQuery({
    queryKey: searchKeys.lookup(idType, id),
    queryFn: () =>
      api.get<SearchResult[]>('/v1/search/lookup', { id_type: idType, id }),
    enabled: !!idType && !!id,
    staleTime: 60_000,
  })
}
