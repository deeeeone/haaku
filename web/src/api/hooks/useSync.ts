import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { useApi } from '@/api/client'
import type {
  ActivityItem,
  CollectionItem,
  CollectionResponse,
  FavoriteItem,
  FavoriteResponse,
  HistoryItem,
  HistoryResponse,
  Ids,
  Movie,
  PlaybackItem,
  RatingItem,
  RatingResponse,
  RecommendationItem,
  SyncActionPayload,
  WatchlistItem,
  WatchlistReorderBody,
  WatchlistResponse,
} from '@/api/types'

// ============================================================
// buildSyncPayload helper (used by detail pages)
// ============================================================

export function buildSyncPayload(
  type: string,
  ids: Ids,
  _rating?: number
): SyncActionPayload {
  switch (type) {
    case 'movie':
      return { movies: [{ ids }] }
    case 'show':
      return { shows: [{ ids }] }
    case 'episode':
      return { episodes: [{ ids }] }
    case 'season':
      return { seasons: [{ ids }] }
    default:
      return { movies: [{ ids }] }
  }
}

// ============================================================
// Watchlist hooks
// ============================================================

export function useWatchlist(params?: {
  page?: number
  limit?: number
  type?: string
  sort?: string
}) {
  const api = useApi()

  return useQuery({
    queryKey: ['watchlist', params],
    queryFn: async () => {
      if (!api)
        return {
          data: [] as WatchlistItem[],
          page: 1,
          limit: 20,
          total: 0,
          total_pages: 0,
        }
      const searchParams: Record<string, string> = {}
      if (params?.page) searchParams.page = String(params.page)
      if (params?.limit) searchParams.limit = String(params.limit)
      if (params?.type) searchParams.type = params.type
      if (params?.sort) searchParams.sort = params.sort

      const res = await api.GET('/v1/sync/watchlist', {
        params: { query: searchParams as Record<string, never> },
      } as never)
      const data = res.data as unknown as unknown[] | undefined
      if (!data || !Array.isArray(data))
        return {
          data: [] as WatchlistItem[],
          page: 1,
          limit: 20,
          total: 0,
          total_pages: 0,
        }

      const headers = res.response?.headers
      const total = Number(headers?.get('x-pagination-total') ?? data.length)
      const page = Number(headers?.get('x-pagination-page') ?? 1)
      const limit = Number(headers?.get('x-pagination-limit') ?? 20)

      return {
        data: data as WatchlistItem[],
        page,
        limit,
        total,
        total_pages: Math.ceil(total / limit),
      } satisfies WatchlistResponse
    },
    enabled: !!api,
  })
}

export function useAddToWatchlist() {
  const api = useApi()
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (body: SyncActionPayload) => {
      if (!api) throw new Error('Not authenticated')
      const res = await api.POST('/v1/sync/watchlist', { body } as never)
      return res.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['watchlist'] })
      toast.success('Added to watchlist')
    },
    onError: () => {
      toast.error('Failed to add to watchlist')
    },
  })
}

export function useRemoveFromWatchlist() {
  const api = useApi()
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (body: SyncActionPayload) => {
      if (!api) throw new Error('Not authenticated')
      const res = await api.POST('/v1/sync/watchlist/remove', { body } as never)
      return res.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['watchlist'] })
      toast.success('Removed from watchlist')
    },
    onError: () => {
      toast.error('Failed to remove from watchlist')
    },
  })
}

export function useReorderWatchlist() {
  const api = useApi()
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (body: WatchlistReorderBody) => {
      if (!api) throw new Error('Not authenticated')
      const res = await api.PUT('/v1/sync/watchlist/reorder', { body } as never)
      return res.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['watchlist'] })
    },
    onError: () => {
      toast.error('Failed to reorder watchlist')
    },
  })
}

// ============================================================
// History hooks
// ============================================================

export function useHistory(params?: {
  page?: number
  limit?: number
  type?: string
  start_at?: string
  end_at?: string
}) {
  const api = useApi()

  return useQuery({
    queryKey: ['history', params],
    queryFn: async () => {
      if (!api)
        return {
          data: [] as HistoryItem[],
          page: 1,
          limit: 20,
          total: 0,
          total_pages: 0,
        }
      const searchParams: Record<string, string> = {}
      if (params?.page) searchParams.page = String(params.page)
      if (params?.limit) searchParams.limit = String(params.limit)
      if (params?.type) searchParams.type = params.type
      if (params?.start_at) searchParams.start_at = params.start_at
      if (params?.end_at) searchParams.end_at = params.end_at

      const res = await api.GET('/v1/sync/history', {
        params: { query: searchParams as Record<string, never> },
      } as never)
      const data = res.data as unknown as unknown[] | undefined
      if (!data || !Array.isArray(data))
        return {
          data: [] as HistoryItem[],
          page: 1,
          limit: 20,
          total: 0,
          total_pages: 0,
        }

      const headers = res.response?.headers
      const total = Number(headers?.get('x-pagination-total') ?? data.length)
      const page = Number(headers?.get('x-pagination-page') ?? 1)
      const limit = Number(headers?.get('x-pagination-limit') ?? 20)

      return {
        data: data as HistoryItem[],
        page,
        limit,
        total,
        total_pages: Math.ceil(total / limit),
      } satisfies HistoryResponse
    },
    enabled: !!api,
  })
}

export function useAddToHistory() {
  const api = useApi()
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (body: SyncActionPayload) => {
      if (!api) throw new Error('Not authenticated')
      const res = await api.POST('/v1/sync/history', { body } as never)
      return res.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['history'] })
      toast.success('Added to history')
    },
    onError: () => {
      toast.error('Failed to add to history')
    },
  })
}

export function useRemoveFromHistory() {
  const api = useApi()
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (body: SyncActionPayload) => {
      if (!api) throw new Error('Not authenticated')
      const res = await api.POST('/v1/sync/history/remove', { body } as never)
      return res.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['history'] })
      toast.success('Removed from history')
    },
    onError: () => {
      toast.error('Failed to remove from history')
    },
  })
}

// ============================================================
// Ratings hooks
// ============================================================

export function useRatings(params?: {
  page?: number
  limit?: number
  type?: string
}) {
  const api = useApi()

  return useQuery({
    queryKey: ['ratings', params],
    queryFn: async () => {
      if (!api)
        return {
          data: [] as RatingItem[],
          page: 1,
          limit: 20,
          total: 0,
          total_pages: 0,
        }
      const searchParams: Record<string, string> = {}
      if (params?.page) searchParams.page = String(params.page)
      if (params?.limit) searchParams.limit = String(params.limit)
      if (params?.type) searchParams.type = params.type

      const res = await api.GET('/v1/sync/ratings', {
        params: { query: searchParams as Record<string, never> },
      } as never)
      const data = res.data as unknown as unknown[] | undefined
      if (!data || !Array.isArray(data))
        return {
          data: [] as RatingItem[],
          page: 1,
          limit: 20,
          total: 0,
          total_pages: 0,
        }

      const headers = res.response?.headers
      const total = Number(headers?.get('x-pagination-total') ?? data.length)
      const page = Number(headers?.get('x-pagination-page') ?? 1)
      const limit = Number(headers?.get('x-pagination-limit') ?? 20)

      return {
        data: data as RatingItem[],
        page,
        limit,
        total,
        total_pages: Math.ceil(total / limit),
      } satisfies RatingResponse
    },
    enabled: !!api,
  })
}

export function useAddRating() {
  const api = useApi()
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (body: Record<string, unknown>) => {
      if (!api) throw new Error('Not authenticated')
      const res = await api.POST('/v1/sync/ratings', { body } as never)
      return res.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['ratings'] })
      toast.success('Rating saved')
    },
    onError: () => {
      toast.error('Failed to save rating')
    },
  })
}

export function useRemoveRating() {
  const api = useApi()
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (body: SyncActionPayload) => {
      if (!api) throw new Error('Not authenticated')
      const res = await api.POST('/v1/sync/ratings/remove', { body } as never)
      return res.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['ratings'] })
      toast.success('Rating removed')
    },
    onError: () => {
      toast.error('Failed to remove rating')
    },
  })
}

// ============================================================
// Collection hooks
// ============================================================

export function useCollection(params?: {
  page?: number
  limit?: number
  type?: string
}) {
  const api = useApi()

  return useQuery({
    queryKey: ['collection', params],
    queryFn: async () => {
      if (!api)
        return {
          data: [] as CollectionItem[],
          page: 1,
          limit: 20,
          total: 0,
          total_pages: 0,
        }
      const searchParams: Record<string, string> = {}
      if (params?.page) searchParams.page = String(params.page)
      if (params?.limit) searchParams.limit = String(params.limit)
      if (params?.type) searchParams.type = params.type

      const res = await api.GET('/v1/sync/collection', {
        params: { query: searchParams as Record<string, never> },
      } as never)
      const data = res.data as unknown as unknown[] | undefined
      if (!data || !Array.isArray(data))
        return {
          data: [] as CollectionItem[],
          page: 1,
          limit: 20,
          total: 0,
          total_pages: 0,
        }

      const headers = res.response?.headers
      const total = Number(headers?.get('x-pagination-total') ?? data.length)
      const page = Number(headers?.get('x-pagination-page') ?? 1)
      const limit = Number(headers?.get('x-pagination-limit') ?? 20)

      return {
        data: data as CollectionItem[],
        page,
        limit,
        total,
        total_pages: Math.ceil(total / limit),
      } satisfies CollectionResponse
    },
    enabled: !!api,
  })
}

export function useAddToCollection() {
  const api = useApi()
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (body: SyncActionPayload) => {
      if (!api) throw new Error('Not authenticated')
      const res = await api.POST('/v1/sync/collection', { body } as never)
      return res.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['collection'] })
      toast.success('Added to collection')
    },
    onError: () => {
      toast.error('Failed to add to collection')
    },
  })
}

export function useRemoveFromCollection() {
  const api = useApi()
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (body: SyncActionPayload) => {
      if (!api) throw new Error('Not authenticated')
      const res = await api.POST('/v1/sync/collection/remove', {
        body,
      } as never)
      return res.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['collection'] })
      toast.success('Removed from collection')
    },
    onError: () => {
      toast.error('Failed to remove from collection')
    },
  })
}

// ============================================================
// Favorites hooks
// ============================================================

export function useFavorites(params?: {
  page?: number
  limit?: number
  type?: string
}) {
  const api = useApi()

  return useQuery({
    queryKey: ['favorites', params],
    queryFn: async () => {
      if (!api)
        return {
          data: [] as FavoriteItem[],
          page: 1,
          limit: 20,
          total: 0,
          total_pages: 0,
        }
      const searchParams: Record<string, string> = {}
      if (params?.page) searchParams.page = String(params.page)
      if (params?.limit) searchParams.limit = String(params.limit)
      if (params?.type) searchParams.type = params.type

      const res = await api.GET('/v1/sync/favorites', {
        params: { query: searchParams as Record<string, never> },
      } as never)
      const data = res.data as unknown as unknown[] | undefined
      if (!data || !Array.isArray(data))
        return {
          data: [] as FavoriteItem[],
          page: 1,
          limit: 20,
          total: 0,
          total_pages: 0,
        }

      const headers = res.response?.headers
      const total = Number(headers?.get('x-pagination-total') ?? data.length)
      const page = Number(headers?.get('x-pagination-page') ?? 1)
      const limit = Number(headers?.get('x-pagination-limit') ?? 20)

      return {
        data: data as FavoriteItem[],
        page,
        limit,
        total,
        total_pages: Math.ceil(total / limit),
      } satisfies FavoriteResponse
    },
    enabled: !!api,
  })
}

export function useAddToFavorites() {
  const api = useApi()
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (body: SyncActionPayload) => {
      if (!api) throw new Error('Not authenticated')
      const res = await api.POST('/v1/sync/favorites', { body } as never)
      return res.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['favorites'] })
      toast.success('Added to favorites')
    },
    onError: () => {
      toast.error('Failed to add to favorites')
    },
  })
}

export function useRemoveFromFavorites() {
  const api = useApi()
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (body: SyncActionPayload) => {
      if (!api) throw new Error('Not authenticated')
      const res = await api.POST('/v1/sync/favorites/remove', { body } as never)
      return res.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['favorites'] })
      toast.success('Removed from favorites')
    },
    onError: () => {
      toast.error('Failed to remove from favorites')
    },
  })
}

// ============================================================
// Playback hooks (Continue Watching)
// ============================================================

// The media endpoints serialise ids as `{id, slug, tmdb, imdb}`; the frontend
// types use Trakt's `{trakt, slug, ...}`. Normalise here so route links keep
// working without changing every consumer.
interface ApiIDs {
  id?: number
  slug?: string
  tmdb?: number
  imdb?: string
}

function normalizeIDs(ids: ApiIDs | undefined): Ids {
  return {
    trakt: ids?.id ?? 0,
    slug: ids?.slug ?? (ids?.id != null ? String(ids.id) : ''),
    tmdb: ids?.tmdb,
    imdb: ids?.imdb,
  }
}

interface ApiMoviePlayback {
  progress: number
  paused_at: string
  movie?: { ids?: ApiIDs; title: string; year: number } & Record<
    string,
    unknown
  >
}

interface ApiEpisodePlayback {
  progress: number
  paused_at: string
  episode?: { title: string; season: number; number: number }
  show?: { title: string; ids?: ApiIDs }
}

function normalizeMovie(raw: ApiMoviePlayback['movie']): Movie | undefined {
  if (!raw) return undefined
  return { ...raw, ids: normalizeIDs(raw.ids) } as Movie
}

export function usePlaybackMovies() {
  const api = useApi()

  return useQuery({
    queryKey: ['playback', 'movies'],
    queryFn: async (): Promise<PlaybackItem[]> => {
      if (!api) return []
      const res = await api.GET('/v1/sync/playback/movies')
      const data = res.data as unknown as ApiMoviePlayback[] | undefined
      return (data ?? []).map((item) => {
        const movie = normalizeMovie(item.movie)
        return {
          id: movie?.ids.trakt ?? 0,
          progress: item.progress,
          paused_at: item.paused_at,
          type: 'movie' as const,
          movie,
        }
      })
    },
    enabled: !!api,
  })
}

export function usePlaybackEpisodes() {
  const api = useApi()

  return useQuery({
    queryKey: ['playback', 'episodes'],
    queryFn: async (): Promise<PlaybackItem[]> => {
      if (!api) return []
      const res = await api.GET('/v1/sync/playback/episodes')
      const data = res.data as unknown as ApiEpisodePlayback[] | undefined
      return (data ?? []).map((item) => {
        const show = item.show
          ? { title: item.show.title, ids: normalizeIDs(item.show.ids) }
          : undefined
        return {
          id: show?.ids.trakt ?? 0,
          progress: item.progress,
          paused_at: item.paused_at,
          type: 'episode' as const,
          episode: item.episode,
          show,
        }
      })
    },
    enabled: !!api,
  })
}

// ============================================================
// Activity feed hooks
// ============================================================

interface ApiActivity {
  id: number
  actor_id: string
  event_type: string
  media_type?: string
  media_id?: number
  payload?: Record<string, unknown>
  created_at: string
}

function normalizeActivity(raw: unknown): ActivityItem[] {
  const items = Array.isArray(raw)
    ? raw
    : ((raw as { data?: unknown } | undefined)?.data ?? [])
  if (!Array.isArray(items)) return []
  return items.map((item) => {
    const a = item as ApiActivity
    return {
      id: a.id,
      action: a.event_type,
      type: a.media_type ?? '',
      timestamps: { start: a.created_at },
      user: {
        username: a.actor_id,
        name: a.actor_id,
        private: false,
        ids: { slug: a.actor_id },
      },
    }
  })
}

export function useActivityFeed(params?: {
  page?: number
  limit?: number
  type?: string
}) {
  const api = useApi()

  return useQuery({
    queryKey: ['activity', params],
    queryFn: async (): Promise<ActivityItem[]> => {
      if (!api) return []
      const searchParams: Record<string, string> = {}
      if (params?.page) searchParams.page = String(params.page)
      if (params?.limit) searchParams.limit = String(params.limit)
      if (params?.type) searchParams.type = params.type

      const res = await api.GET('/v1/users/me/activity', {
        params: { query: searchParams as Record<string, never> },
      } as never)
      return normalizeActivity(res.data)
    },
    enabled: !!api,
  })
}

// ============================================================
// Recommendations hooks
// ============================================================

interface ApiRecommendation {
  id: number
  tmdb_id: number
  title: string
  overview?: string
  popularity: number
  genres?: string[]
  released?: string
  first_aired?: string
  status?: string
  episode_count?: number
  friend_count?: number
  rating_avg: number
  poster?: string
}

function normalizeRecommendations(
  raw: unknown,
  mediaType: 'movie' | 'show'
): RecommendationItem[] {
  const items = Array.isArray(raw)
    ? raw
    : ((raw as { items?: unknown } | undefined)?.items ?? [])
  if (!Array.isArray(items)) return []
  return items.map((item) => ({
    ...(item as ApiRecommendation),
    media_type: mediaType,
  }))
}

export function useRecommendationsMovies() {
  const api = useApi()

  return useQuery({
    queryKey: ['recommendations', 'movies'],
    queryFn: async (): Promise<RecommendationItem[]> => {
      if (!api) return []
      const res = await api.GET('/v1/recommendations/movies')
      return normalizeRecommendations(res.data, 'movie')
    },
    enabled: !!api,
  })
}

export function useRecommendationsShows() {
  const api = useApi()

  return useQuery({
    queryKey: ['recommendations', 'shows'],
    queryFn: async (): Promise<RecommendationItem[]> => {
      if (!api) return []
      const res = await api.GET('/v1/recommendations/shows')
      return normalizeRecommendations(res.data, 'show')
    },
    enabled: !!api,
  })
}

export function useSocialRecommendationsMovies() {
  const api = useApi()

  return useQuery({
    queryKey: ['social-recommendations', 'movies'],
    queryFn: async (): Promise<RecommendationItem[]> => {
      if (!api) return []
      const res = await api.GET('/v1/social_recommendations/movies')
      return normalizeRecommendations(res.data, 'movie')
    },
    enabled: !!api,
  })
}

export function useSocialRecommendationsShows() {
  const api = useApi()

  return useQuery({
    queryKey: ['social-recommendations', 'shows'],
    queryFn: async (): Promise<RecommendationItem[]> => {
      if (!api) return []
      const res = await api.GET('/v1/social_recommendations/shows')
      return normalizeRecommendations(res.data, 'show')
    },
    enabled: !!api,
  })
}
