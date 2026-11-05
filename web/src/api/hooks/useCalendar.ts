import { useQuery } from '@tanstack/react-query'
import { useApi } from '@/api/client'

export interface CalendarEpisode {
  episode: {
    season: number
    number: number
    title: string
    ids: { trakt: number; slug: string; imdb?: string; tmdb?: number }
    overview?: string
    first_aired?: string
    runtime?: number
    rating?: number
    images?: { screenshot?: string }
  }
  show: {
    title: string
    year: number
    ids: { trakt: number; slug: string; imdb?: string; tmdb?: number }
    images?: { poster?: string; thumb?: string }
  }
  season?: {
    number: number
    ids: { trakt: number }
  }
  type?: string // 'new' | 'premiere' | 'finale'
}

export type CalendarTarget = 'my' | 'all' | 'friends' | 'favorites' | 'watched'

export function useCalendar(params: {
  target: CalendarTarget
  startDate: string
  days: number
  type?: string // 'new' | 'premieres' | 'finale'
}) {
  const api = useApi()

  return useQuery({
    queryKey: ['calendar', params],
    queryFn: async () => {
      if (!api) return [] as CalendarEpisode[]
      const searchParams: Record<string, string> = {}
      if (params.type) searchParams.type = params.type

      const path =
        `/v1/calendars/${params.target}/shows/${params.startDate}/${params.days}` as const
      const res = await api.GET(
        path as '/v1/calendars/my/shows/{start_date}/{days}',
        {
          params: {
            path: { start_date: params.startDate, days: params.days },
            query: searchParams as Record<string, never>,
          },
        } as never
      )
      return (res.data as unknown as CalendarEpisode[]) ?? []
    },
    enabled: !!api && !!params.startDate,
  })
}
