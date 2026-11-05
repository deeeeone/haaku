/**
 * Season hooks — powered by TanStack Query
 */

import { useQuery } from '@tanstack/react-query'
import { api } from '@/api/fetch'
import type { Season, MediaStats, RatingDistribution } from '@/api/types'

// ─── Keys ──────────────────────────────────────────────────────────────

export const seasonKeys = {
  all: ['seasons'] as const,
  detail: (id: string | number) => ['seasons', 'detail', String(id)] as const,
  stats: (id: string | number) => ['seasons', 'stats', String(id)] as const,
  ratings: (id: string | number) => ['seasons', 'ratings', String(id)] as const,
}

// ─── Hooks ─────────────────────────────────────────────────────────────

export function useSeason(id: string | number | undefined) {
  return useQuery({
    queryKey: seasonKeys.detail(id ?? ''),
    queryFn: () => api.get<Season>(`/v1/seasons/${id}`),
    enabled: !!id,
    staleTime: 5 * 60_000,
  })
}

export function useSeasonStats(id: string | number | undefined) {
  return useQuery({
    queryKey: seasonKeys.stats(id ?? ''),
    queryFn: () => api.get<MediaStats>(`/v1/seasons/${id}/stats`),
    enabled: !!id,
    staleTime: 2 * 60_000,
  })
}

export function useSeasonRatings(id: string | number | undefined) {
  return useQuery({
    queryKey: seasonKeys.ratings(id ?? ''),
    queryFn: () => api.get<RatingDistribution>(`/v1/seasons/${id}/ratings`),
    enabled: !!id,
    staleTime: 2 * 60_000,
  })
}
