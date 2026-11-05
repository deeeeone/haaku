import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { useApi } from '@/api/client'

export interface Notification {
  id: number
  type: string // 'follow' | 'comment' | 'like' | 'watchlist' | 'recommendation' etc.
  message: string
  read: boolean
  created_at: string
  user?: {
    username: string
    name?: string
    ids: { slug: string }
    avatar?: string
  }
  item?: {
    type: string
    id: number
    title?: string
  }
}

export function useNotifications(params?: { page?: number; limit?: number }) {
  const api = useApi()

  return useQuery({
    queryKey: ['notifications', params],
    queryFn: async () => {
      if (!api) return []
      const searchParams: Record<string, string> = {}
      if (params?.page) searchParams.page = String(params.page)
      if (params?.limit) searchParams.limit = String(params.limit)
      const res = await api.GET('/v1/notifications', {
        params: { query: searchParams as Record<string, never> },
      } as never)
      return (res.data as unknown as Notification[]) ?? []
    },
    enabled: !!api,
  })
}

export function useMarkNotificationsRead() {
  const api = useApi()
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (ids: number[]) => {
      if (!api) throw new Error('Not authenticated')
      await api.POST('/v1/notifications/read', {
        body: { ids },
      } as never)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['notifications'] })
      toast.success('Marked as read')
    },
    onError: () => {
      toast.error('Failed to mark as read')
    },
  })
}
