/**
 * Person hooks — powered by TanStack Query
 */

import { useQuery } from '@tanstack/react-query'
import { api } from '@/api/fetch'
import type { Person, CastMember } from '@/api/types'

// ─── Keys ──────────────────────────────────────────────────────────────

export const personKeys = {
  all: ['people'] as const,
  detail: (id: string | number) => ['people', 'detail', String(id)] as const,
  movies: (id: string | number) => ['people', 'movies', String(id)] as const,
  shows: (id: string | number) => ['people', 'shows', String(id)] as const,
}

// ─── Hooks ─────────────────────────────────────────────────────────────

export function usePerson(id: string | number | undefined) {
  return useQuery({
    queryKey: personKeys.detail(id ?? ''),
    queryFn: () => api.get<Person>(`/v1/people/${id}`),
    enabled: !!id,
    staleTime: 5 * 60_000,
  })
}

export function usePersonMovies(id: string | number | undefined) {
  return useQuery({
    queryKey: personKeys.movies(id ?? ''),
    queryFn: () => api.get<CastMember[]>(`/v1/people/${id}/movies`),
    enabled: !!id,
    staleTime: 5 * 60_000,
  })
}

export function usePersonShows(id: string | number | undefined) {
  return useQuery({
    queryKey: personKeys.shows(id ?? ''),
    queryFn: () => api.get<CastMember[]>(`/v1/people/${id}/shows`),
    enabled: !!id,
    staleTime: 5 * 60_000,
  })
}
