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

export function usePlaybackMovies() {
  const api = useApi()

  return useQuery({
    queryKey: ['playback', 'movies'],
    queryFn: async () => {
      if (!api) return []
      const res = await api.GET('/v1/sync/playback/movies')
      const data = res.data as unknown as unknown[] | undefined
      return (data ?? []) as PlaybackItem[]
    },
    enabled: !!api,
  })
}

export function usePlaybackEpisodes() {
  const api = useApi()

  return useQuery({
    queryKey: ['playback', 'episodes'],
    queryFn: async () => {
      if (!api) return []
      const res = await api.GET('/v1/sync/playback/episodes')
      const data = res.data as unknown as unknown[] | undefined
      return (data ?? []) as PlaybackItem[]
    },
    enabled: !!api,
  })
}

// ============================================================
// Activity feed hooks
// ============================================================

export function useActivityFeed(params?: {
  page?: number
  limit?: number
  type?: string
}) {
  const api = useApi()

  return useQuery({
    queryKey: ['activity', params],
    queryFn: async () => {
      if (!api) return []
      const searchParams: Record<string, string> = {}
      if (params?.page) searchParams.page = String(params.page)
      if (params?.limit) searchParams.limit = String(params.limit)
      if (params?.type) searchParams.type = params.type

      const res = await api.GET('/v1/users/me/activity', {
        params: { query: searchParams as Record<string, never> },
      } as never)
      const data = res.data as unknown as unknown[] | undefined
      return (data ?? []) as ActivityItem[]
    },
    enabled: !!api,
  })
}

// ============================================================
// Recommendations hooks
// ============================================================

export function useRecommendationsMovies() {
  const api = useApi()

  return useQuery({
    queryKey: ['recommendations', 'movies'],
    queryFn: async () => {
      if (!api) return []
      const res = await api.GET('/v1/recommendations/movies')
      const data = res.data as unknown as unknown[] | undefined
      return (data ?? []) as RecommendationItem[]
    },
    enabled: !!api,
  })
}

export function useRecommendationsShows() {
  const api = useApi()

  return useQuery({
    queryKey: ['recommendations', 'shows'],
    queryFn: async () => {
      if (!api) return []
      const res = await api.GET('/v1/recommendations/shows')
      const data = res.data as unknown as unknown[] | undefined
      return (data ?? []) as RecommendationItem[]
    },
    enabled: !!api,
  })
}

export function useSocialRecommendationsMovies() {
  const api = useApi()

  return useQuery({
    queryKey: ['social-recommendations', 'movies'],
    queryFn: async () => {
      if (!api) return []
      const res = await api.GET('/v1/social_recommendations/movies')
      const data = res.data as unknown as unknown[] | undefined
      return (data ?? []) as RecommendationItem[]
    },
    enabled: !!api,
  })
}

export function useSocialRecommendationsShows() {
  const api = useApi()

  return useQuery({
    queryKey: ['social-recommendations', 'shows'],
    queryFn: async () => {
      if (!api) return []
      const res = await api.GET('/v1/social_recommendations/shows')
      const data = res.data as unknown as unknown[] | undefined
      return (data ?? []) as RecommendationItem[]
    },
    enabled: !!api,
  })
}
