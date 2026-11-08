/**
 * Search hooks — powered by TanStack Query
 *
 * Search answers from two places: the local catalogue and, for anything it does
 * not hold, TVDB's live name search. Both are rendered into the same shape; the
 * difference is `source`/`in_catalog`, and a live hit has to be resolved
 * through /v1/media/resolve before it can be navigated to or watchlisted.
 */

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useApi } from '@/api/client'
import { api } from '@/api/fetch'
import type {
  Ids,
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

// ─── Normalisation ─────────────────────────────────────────────────────

// The media endpoints serialise ids as `{id, slug, tmdb, imdb, tvdb}`; the
// frontend types use Trakt's `{trakt, slug, ...}`. Normalise here so route
// links and watchlist payloads work without changing every consumer.
interface ApiIDs {
  id?: number
  trakt?: number
  slug?: string
  tmdb?: number
  imdb?: string
  tvdb?: string
}

function normalizeIDs(ids: ApiIDs | undefined): Ids {
  const local = ids?.trakt ?? ids?.id ?? 0
  return {
    trakt: local,
    slug: ids?.slug ?? (local > 0 ? String(local) : ''),
    tmdb: ids?.tmdb,
    imdb: ids?.imdb,
    tvdb: ids?.tvdb,
  }
}

/** rewriteIDs normalises whatever an endpoint returned into frontend Ids. */
export function normalizeSearchResult(result: SearchResult): SearchResult {
  if (result.movie) {
    const movie = result.movie as Movie & { ids?: ApiIDs }
    return {
      ...result,
      movie: { ...movie, ids: normalizeIDs(movie.ids) } as Movie,
    }
  }
  if (result.show) {
    const show = result.show as Show & { ids?: ApiIDs }
    return {
      ...result,
      show: { ...show, ids: normalizeIDs(show.ids) } as Show,
    }
  }
  return result
}

// ─── Hooks ─────────────────────────────────────────────────────────────

/**
 * Full-text search with filters: catalogue first, live TVDB results to fill.
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
        const data = result.map(normalizeSearchResult)
        return {
          data,
          page: params.page ?? 1,
          limit: params.limit ?? 20,
          total: data.length,
          total_pages: 1,
        } as SearchResponse
      }
      const wrapped = result as unknown as SearchResponse
      return {
        ...wrapped,
        data: (wrapped.data ?? []).map(normalizeSearchResult),
      } as SearchResponse
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

// ─── Resolve (ingest on demand) ────────────────────────────────────────

export interface ResolveParams {
  type: 'movie' | 'show'
  /** The external id the search result carried (TVDB by default). */
  id: string
  idType?: 'tvdb' | 'tmdb' | 'imdb'
}

/**
 * useResolveMedia turns a live search hit into a canonical local record.
 *
 * Every other endpoint — watchlisting, watching, rating — addresses media by
 * its local row, and a live TVDB result has no row yet. This ingests it (the
 * server call is idempotent, so calling it again for a title already resolved
 * costs nothing but a lookup) and returns the media in the same shape the
 * media endpoints do, so the caller can navigate straight there.
 */
export interface ResolvedMedia {
  mediaType: 'movie' | 'show'
  title: string
  ids: Ids
}

export function useResolveMedia() {
  const apiClient = useApi()
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async ({
      type,
      id,
      idType = 'tvdb',
    }: ResolveParams): Promise<ResolvedMedia> => {
      if (!apiClient) throw new Error('Not authenticated')
      const res = await apiClient.GET('/v1/media/resolve', {
        params: {
          query: { type, id, id_type: idType },
        },
      } as never)
      const resolved = res.data as unknown as
        | (Movie & { ids?: ApiIDs })
        | (Show & { ids?: ApiIDs })
      return {
        mediaType: type,
        title: resolved?.title ?? '',
        ids: normalizeIDs(resolved?.ids),
      }
    },
    onSuccess: () => {
      // The title is in the catalogue now; a stale search page would still
      // show it as a live hit.
      queryClient.invalidateQueries({ queryKey: searchKeys.all })
    },
  })
}
