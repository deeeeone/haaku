/**
 * Episode hooks — powered by TanStack Query
 */

import { useQuery } from '@tanstack/react-query'
import { api } from '@/api/fetch'
import type {
  CommentResponse,
  Episode,
  MediaStats,
  RatingDistribution,
  TraktList,
} from '@/api/types'

// ─── Keys ──────────────────────────────────────────────────────────────

export const episodeKeys = {
  all: ['episodes'] as const,
  detail: (id: string | number) => ['episodes', 'detail', String(id)] as const,
  stats: (id: string | number) => ['episodes', 'stats', String(id)] as const,
  ratings: (id: string | number) =>
    ['episodes', 'ratings', String(id)] as const,
  comments: (id: string | number) =>
    ['episodes', 'comments', String(id)] as const,
  lists: (id: string | number) => ['episodes', 'lists', String(id)] as const,
}

// ─── Hooks ─────────────────────────────────────────────────────────────

export function useEpisode(id: string | number | undefined) {
  return useQuery({
    queryKey: episodeKeys.detail(id ?? ''),
    queryFn: () => api.get<Episode>(`/v1/episodes/${id}`),
    enabled: !!id,
    staleTime: 5 * 60_000,
  })
}

export function useEpisodeStats(id: string | number | undefined) {
  return useQuery({
    queryKey: episodeKeys.stats(id ?? ''),
    queryFn: () => api.get<MediaStats>(`/v1/episodes/${id}/stats`),
    enabled: !!id,
    staleTime: 2 * 60_000,
  })
}

export function useEpisodeRatings(id: string | number | undefined) {
  return useQuery({
    queryKey: episodeKeys.ratings(id ?? ''),
    queryFn: () => api.get<RatingDistribution>(`/v1/episodes/${id}/ratings`),
    enabled: !!id,
    staleTime: 2 * 60_000,
  })
}

export function useEpisodeComments(id: string | number | undefined) {
  return useQuery({
    queryKey: episodeKeys.comments(id ?? ''),
    queryFn: () => api.get<CommentResponse>(`/v1/episodes/${id}/comments`),
    enabled: !!id,
    staleTime: 60_000,
  })
}

export function useEpisodeLists(id: string | number | undefined) {
  return useQuery({
    queryKey: episodeKeys.lists(id ?? ''),
    queryFn: () => api.get<TraktList[]>(`/v1/episodes/${id}/lists`),
    enabled: !!id,
    staleTime: 2 * 60_000,
  })
}
