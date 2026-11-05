/**
 * Show hooks — powered by TanStack Query
 */

import { useQuery } from '@tanstack/react-query'
import { api } from '@/api/fetch'
import type {
  Alias,
  CommentResponse,
  CreditResponse,
  Episode,
  MediaStats,
  RatingDistribution,
  Season,
  Show,
  TraktList,
  TrendingItem,
  WatchProviderCountry,
} from '@/api/types'

// ─── Keys ──────────────────────────────────────────────────────────────

export const showKeys = {
  all: ['shows'] as const,
  detail: (id: string | number) => ['shows', 'detail', String(id)] as const,
  people: (id: string | number) => ['shows', 'people', String(id)] as const,
  stats: (id: string | number) => ['shows', 'stats', String(id)] as const,
  ratings: (id: string | number) => ['shows', 'ratings', String(id)] as const,
  watch: (id: string | number) => ['shows', 'watch', String(id)] as const,
  lists: (id: string | number) => ['shows', 'lists', String(id)] as const,
  watching: (id: string | number) => ['shows', 'watching', String(id)] as const,
  aliases: (id: string | number) => ['shows', 'aliases', String(id)] as const,
  recommendations: (id: string | number) =>
    ['shows', 'recommendations', String(id)] as const,
  comments: (id: string | number) => ['shows', 'comments', String(id)] as const,
  seasons: (id: string | number) => ['shows', 'seasons', String(id)] as const,
  seasonDetail: (id: string | number, season: number) =>
    ['shows', 'season', String(id), season] as const,
  seasonEpisodes: (id: string | number, season: number) =>
    ['shows', 'seasonEpisodes', String(id), season] as const,
  episodeDetail: (id: string | number, season: number, episode: number) =>
    ['shows', 'episodeDetail', String(id), season, episode] as const,
  trending: (page?: number) => ['shows', 'trending', page ?? 1] as const,
  popular: (page?: number) => ['shows', 'popular', page ?? 1] as const,
}

// ─── Hooks ─────────────────────────────────────────────────────────────

export function useShow(id: string | number | undefined) {
  return useQuery({
    queryKey: showKeys.detail(id ?? ''),
    queryFn: () => api.get<Show>(`/v1/shows/${id}`),
    enabled: !!id,
    staleTime: 5 * 60_000,
  })
}

export function useShowPeople(id: string | number | undefined) {
  return useQuery({
    queryKey: showKeys.people(id ?? ''),
    queryFn: () => api.get<CreditResponse>(`/v1/shows/${id}/people`),
    enabled: !!id,
    staleTime: 5 * 60_000,
  })
}

export function useShowStats(id: string | number | undefined) {
  return useQuery({
    queryKey: showKeys.stats(id ?? ''),
    queryFn: () => api.get<MediaStats>(`/v1/shows/${id}/stats`),
    enabled: !!id,
    staleTime: 2 * 60_000,
  })
}

export function useShowRatings(id: string | number | undefined) {
  return useQuery({
    queryKey: showKeys.ratings(id ?? ''),
    queryFn: () => api.get<RatingDistribution>(`/v1/shows/${id}/ratings`),
    enabled: !!id,
    staleTime: 2 * 60_000,
  })
}

export function useShowWatchProviders(id: string | number | undefined) {
  return useQuery({
    queryKey: showKeys.watch(id ?? ''),
    queryFn: () => api.get<WatchProviderCountry[]>(`/v1/shows/${id}/watch`),
    enabled: !!id,
    staleTime: 10 * 60_000,
  })
}

export function useShowLists(id: string | number | undefined) {
  return useQuery({
    queryKey: showKeys.lists(id ?? ''),
    queryFn: () => api.get<TraktList[]>(`/v1/shows/${id}/lists`),
    enabled: !!id,
    staleTime: 2 * 60_000,
  })
}

export function useShowWatching(id: string | number | undefined) {
  return useQuery({
    queryKey: showKeys.watching(id ?? ''),
    queryFn: () => api.get<unknown[]>(`/v1/shows/${id}/watching`),
    enabled: !!id,
    staleTime: 30_000,
  })
}

export function useShowAliases(id: string | number | undefined) {
  return useQuery({
    queryKey: showKeys.aliases(id ?? ''),
    queryFn: () => api.get<Alias[]>(`/v1/shows/${id}/aliases`),
    enabled: !!id,
    staleTime: 10 * 60_000,
  })
}

export function useShowRecommendations(id: string | number | undefined) {
  return useQuery({
    queryKey: showKeys.recommendations(id ?? ''),
    queryFn: () => api.get<Show[]>(`/v1/shows/recommendations/${id}`),
    enabled: !!id,
    staleTime: 5 * 60_000,
  })
}

export function useShowComments(id: string | number | undefined) {
  return useQuery({
    queryKey: showKeys.comments(id ?? ''),
    queryFn: () => api.get<CommentResponse>(`/v1/shows/${id}/comments`),
    enabled: !!id,
    staleTime: 60_000,
  })
}

export function useShowSeasons(id: string | number | undefined) {
  return useQuery({
    queryKey: showKeys.seasons(id ?? ''),
    queryFn: () => api.get<Season[]>(`/v1/shows/${id}/seasons`),
    enabled: !!id,
    staleTime: 5 * 60_000,
  })
}

export function useShowSeason(
  id: string | number | undefined,
  season: number | undefined
) {
  return useQuery({
    queryKey: showKeys.seasonDetail(id ?? '', season ?? 0),
    queryFn: () => api.get<Season>(`/v1/shows/${id}/seasons/${season}`),
    enabled: !!id && season !== undefined,
    staleTime: 5 * 60_000,
  })
}

export function useShowSeasonEpisodes(
  id: string | number | undefined,
  season: number | undefined
) {
  return useQuery({
    queryKey: showKeys.seasonEpisodes(id ?? '', season ?? 0),
    queryFn: () =>
      api.get<Episode[]>(`/v1/shows/${id}/seasons/${season}/episodes`),
    enabled: !!id && season !== undefined,
    staleTime: 5 * 60_000,
  })
}

export function useShowEpisode(
  id: string | number | undefined,
  season: number | undefined,
  episode: number | undefined
) {
  return useQuery({
    queryKey: showKeys.episodeDetail(id ?? '', season ?? 0, episode ?? 0),
    queryFn: () =>
      api.get<Episode>(`/v1/shows/${id}/seasons/${season}/episodes/${episode}`),
    enabled: !!id && season !== undefined && episode !== undefined,
    staleTime: 5 * 60_000,
  })
}

export function useTrendingShows(page = 1, limit = 20) {
  return useQuery({
    queryKey: showKeys.trending(page),
    queryFn: () =>
      api.get<TrendingItem[]>(`/v1/shows/trending`, { page, limit }),
    staleTime: 60_000,
  })
}

export function usePopularShows(page = 1, limit = 20) {
  return useQuery({
    queryKey: showKeys.popular(page),
    queryFn: () => api.get<Show[]>(`/v1/shows/popular`, { page, limit }),
    staleTime: 60_000,
  })
}
