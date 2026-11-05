import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { useApi } from '@/api/client'
import type { Comment } from '@/api/types'

// ============================================================
// Follow hooks
// ============================================================

export function useFollowers(
  userId: string,
  params?: { page?: number; limit?: number }
) {
  const api = useApi()

  return useQuery({
    queryKey: ['followers', userId, params],
    queryFn: async () => {
      if (!api) return []
      const searchParams: Record<string, string> = {}
      if (params?.page) searchParams.page = String(params.page)
      if (params?.limit) searchParams.limit = String(params.limit)
      const res = await api.GET('/v1/users/{id}/followers', {
        params: {
          path: { id: userId },
          query: searchParams as Record<string, never>,
        },
      } as never)
      return (res.data as unknown as unknown[]) ?? []
    },
    enabled: !!api && !!userId,
  })
}

export function useFollowing(
  userId: string,
  params?: { page?: number; limit?: number }
) {
  const api = useApi()

  return useQuery({
    queryKey: ['following', userId, params],
    queryFn: async () => {
      if (!api) return []
      const searchParams: Record<string, string> = {}
      if (params?.page) searchParams.page = String(params.page)
      if (params?.limit) searchParams.limit = String(params.limit)
      const res = await api.GET('/v1/users/{id}/following', {
        params: {
          path: { id: userId },
          query: searchParams as Record<string, never>,
        },
      } as never)
      return (res.data as unknown as unknown[]) ?? []
    },
    enabled: !!api && !!userId,
  })
}

export function useFriends(
  userId: string,
  params?: { page?: number; limit?: number }
) {
  const api = useApi()

  return useQuery({
    queryKey: ['friends', userId, params],
    queryFn: async () => {
      if (!api) return []
      const searchParams: Record<string, string> = {}
      if (params?.page) searchParams.page = String(params.page)
      if (params?.limit) searchParams.limit = String(params.limit)
      const res = await api.GET('/v1/users/{id}/friends', {
        params: {
          path: { id: userId },
          query: searchParams as Record<string, never>,
        },
      } as never)
      return (res.data as unknown as unknown[]) ?? []
    },
    enabled: !!api && !!userId,
  })
}

export function useFollowRequests(params?: { page?: number; limit?: number }) {
  const api = useApi()

  return useQuery({
    queryKey: ['follow-requests', params],
    queryFn: async () => {
      if (!api) return []
      const searchParams: Record<string, string> = {}
      if (params?.page) searchParams.page = String(params.page)
      if (params?.limit) searchParams.limit = String(params.limit)
      const res = await api.GET('/v1/users/me/follow_requests', {
        params: { query: searchParams as Record<string, never> },
      } as never)
      return (res.data as unknown as unknown[]) ?? []
    },
    enabled: !!api,
  })
}

export function useFollow() {
  const api = useApi()
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (userId: string) => {
      if (!api) throw new Error('Not authenticated')
      await api.POST('/v1/users/{id}/follow', {
        params: { path: { id: userId } },
      } as never)
    },
    onSuccess: (_data, userId) => {
      queryClient.invalidateQueries({ queryKey: ['followers'] })
      queryClient.invalidateQueries({ queryKey: ['following'] })
      queryClient.invalidateQueries({ queryKey: ['friends'] })
      queryClient.invalidateQueries({ queryKey: ['user', userId] })
      toast.success('Followed')
    },
    onError: () => {
      toast.error('Failed to follow')
    },
  })
}

export function useUnfollow() {
  const api = useApi()
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (userId: string) => {
      if (!api) throw new Error('Not authenticated')
      await api.DELETE('/v1/users/{id}/follow', {
        params: { path: { id: userId } },
      } as never)
    },
    onSuccess: (_data, userId) => {
      queryClient.invalidateQueries({ queryKey: ['followers'] })
      queryClient.invalidateQueries({ queryKey: ['following'] })
      queryClient.invalidateQueries({ queryKey: ['friends'] })
      queryClient.invalidateQueries({ queryKey: ['user', userId] })
      toast.success('Unfollowed')
    },
    onError: () => {
      toast.error('Failed to unfollow')
    },
  })
}

export function useApproveFollowRequest() {
  const api = useApi()
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (followerId: string) => {
      if (!api) throw new Error('Not authenticated')
      await api.POST('/v1/users/me/follow_requests/{follower_id}/approve', {
        params: { path: { follower_id: followerId } },
      } as never)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['follow-requests'] })
      queryClient.invalidateQueries({ queryKey: ['followers'] })
      toast.success('Follow request approved')
    },
    onError: () => {
      toast.error('Failed to approve')
    },
  })
}

export function useDenyFollowRequest() {
  const api = useApi()
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (followerId: string) => {
      if (!api) throw new Error('Not authenticated')
      await api.DELETE('/v1/users/me/follow_requests/{follower_id}', {
        params: { path: { follower_id: followerId } },
      } as never)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['follow-requests'] })
      toast.success('Follow request denied')
    },
    onError: () => {
      toast.error('Failed to deny')
    },
  })
}

export function useBlock() {
  const api = useApi()
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (userId: string) => {
      if (!api) throw new Error('Not authenticated')
      await api.POST('/v1/users/{id}/block', {
        params: { path: { id: userId } },
      } as never)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['user'] })
      toast.success('User blocked')
    },
    onError: () => {
      toast.error('Failed to block user')
    },
  })
}

export function useUnblock() {
  const api = useApi()
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (userId: string) => {
      if (!api) throw new Error('Not authenticated')
      await api.DELETE('/v1/users/{id}/block', {
        params: { path: { id: userId } },
      } as never)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['user'] })
      toast.success('User unblocked')
    },
    onError: () => {
      toast.error('Failed to unblock')
    },
  })
}

// ============================================================
// Comments hooks
// ============================================================

export function useComments(params: {
  mediaType: string
  mediaId: string
  page?: number
  limit?: number
}) {
  const api = useApi()

  return useQuery({
    queryKey: ['comments', params],
    queryFn: async () => {
      if (!api)
        return {
          data: [] as Comment[],
          page: 1,
          limit: 20,
          total: 0,
          total_pages: 0,
        }
      const searchParams: Record<string, string> = {}
      if (params?.page) searchParams.page = String(params.page)
      if (params?.limit) searchParams.limit = String(params.limit)

      const path =
        `/v1/${params.mediaType}s/${params.mediaId}/comments` as const
      const res = await api.GET(
        path as never,
        {
          params: {
            path: { id: params.mediaId },
            query: searchParams as Record<string, never>,
          },
        } as never
      )
      const data = res.data as unknown as unknown[] | undefined
      if (!data || !Array.isArray(data))
        return {
          data: [] as Comment[],
          page: 1,
          limit: 20,
          total: 0,
          total_pages: 0,
        }

      const headers = res.response?.headers
      const total = Number(headers?.get('x-pagination-total') ?? data.length)
      const pageNum = Number(headers?.get('x-pagination-page') ?? 1)
      const limitNum = Number(headers?.get('x-pagination-limit') ?? 20)

      return {
        data: data as Comment[],
        page: pageNum,
        limit: limitNum,
        total,
        total_pages: Math.ceil(total / limitNum),
      }
    },
    enabled: !!api && !!params.mediaId,
  })
}

export function useCommentReplies(
  commentId: number,
  params?: { page?: number; limit?: number }
) {
  const api = useApi()

  return useQuery({
    queryKey: ['comment-replies', commentId, params],
    queryFn: async () => {
      if (!api)
        return {
          data: [] as Comment[],
          page: 1,
          limit: 20,
          total: 0,
          total_pages: 0,
        }
      const searchParams: Record<string, string> = {}
      if (params?.page) searchParams.page = String(params.page)
      if (params?.limit) searchParams.limit = String(params.limit)

      const res = await api.GET('/v1/comments/{id}/replies', {
        params: {
          path: { id: commentId },
          query: searchParams as Record<string, never>,
        },
      } as never)
      const data = res.data as unknown as unknown[] | undefined
      if (!data || !Array.isArray(data))
        return {
          data: [] as Comment[],
          page: 1,
          limit: 20,
          total: 0,
          total_pages: 0,
        }

      const headers = res.response?.headers
      const total = Number(headers?.get('x-pagination-total') ?? data.length)
      const pageNum = Number(headers?.get('x-pagination-page') ?? 1)
      const limitNum = Number(headers?.get('x-pagination-limit') ?? 20)

      return {
        data: data as Comment[],
        page: pageNum,
        limit: limitNum,
        total,
        total_pages: Math.ceil(total / limitNum),
      }
    },
    enabled: !!api && !!commentId,
  })
}

export function useCreateComment() {
  const api = useApi()
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (body: {
      comment: string
      spoiler: boolean
      review: boolean
      item: { type: string; id: number }
    }) => {
      if (!api) throw new Error('Not authenticated')
      const res = await api.POST('/v1/comments', { body } as never)
      return res.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['comments'] })
      toast.success('Comment posted')
    },
    onError: () => {
      toast.error('Failed to post comment')
    },
  })
}

export function useCreateReply(commentId: number) {
  const api = useApi()
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (body: { comment: string; spoiler: boolean }) => {
      if (!api) throw new Error('Not authenticated')
      const res = await api.POST('/v1/comments/{id}/replies', {
        params: { path: { id: commentId } },
        body,
      } as never)
      return res.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['comment-replies'] })
      queryClient.invalidateQueries({ queryKey: ['comments'] })
      toast.success('Reply posted')
    },
    onError: () => {
      toast.error('Failed to post reply')
    },
  })
}

export function useEditComment() {
  const api = useApi()
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async ({
      id,
      comment,
      spoiler,
    }: {
      id: number
      comment: string
      spoiler: boolean
    }) => {
      if (!api) throw new Error('Not authenticated')
      const res = await api.PUT('/v1/comments/{id}', {
        params: { path: { id } },
        body: { comment, spoiler },
      } as never)
      return res.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['comments'] })
      queryClient.invalidateQueries({ queryKey: ['comment-replies'] })
      toast.success('Comment updated')
    },
    onError: () => {
      toast.error('Failed to update comment')
    },
  })
}

export function useDeleteComment() {
  const api = useApi()
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (commentId: number) => {
      if (!api) throw new Error('Not authenticated')
      await api.DELETE('/v1/comments/{id}', {
        params: { path: { id: commentId } },
      } as never)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['comments'] })
      queryClient.invalidateQueries({ queryKey: ['comment-replies'] })
      toast.success('Comment deleted')
    },
    onError: () => {
      toast.error('Failed to delete comment')
    },
  })
}

export function useLikeComment() {
  const api = useApi()
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (commentId: number) => {
      if (!api) throw new Error('Not authenticated')
      await api.POST('/v1/comments/{id}/like', {
        params: { path: { id: commentId } },
      } as never)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['comments'] })
      queryClient.invalidateQueries({ queryKey: ['comment-replies'] })
    },
  })
}

export function useUnlikeComment() {
  const api = useApi()
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (commentId: number) => {
      if (!api) throw new Error('Not authenticated')
      await api.DELETE('/v1/comments/{id}/like', {
        params: { path: { id: commentId } },
      } as never)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['comments'] })
      queryClient.invalidateQueries({ queryKey: ['comment-replies'] })
    },
  })
}

export function useAddReaction() {
  const api = useApi()
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async ({
      commentId,
      reaction,
    }: {
      commentId: number
      reaction: string
    }) => {
      if (!api) throw new Error('Not authenticated')
      await api.POST('/v1/comments/{id}/reactions', {
        params: { path: { id: commentId } },
        body: { reaction },
      } as never)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['comments'] })
      queryClient.invalidateQueries({ queryKey: ['comment-replies'] })
    },
  })
}

export function useRemoveReaction() {
  const api = useApi()
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async ({
      commentId,
      reaction,
    }: {
      commentId: number
      reaction: string
    }) => {
      if (!api) throw new Error('Not authenticated')
      await api.DELETE('/v1/comments/{id}/reactions', {
        params: { path: { id: commentId } },
        body: { reaction },
      } as never)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['comments'] })
      queryClient.invalidateQueries({ queryKey: ['comment-replies'] })
    },
  })
}

export function useReportComment() {
  const api = useApi()

  return useMutation({
    mutationFn: async (commentId: number) => {
      if (!api) throw new Error('Not authenticated')
      await api.POST('/v1/comments/{id}/report', {
        params: { path: { id: commentId } },
      } as never)
    },
    onSuccess: () => {
      toast.success('Comment reported')
    },
    onError: () => {
      toast.error('Failed to report comment')
    },
  })
}

export function useTrendingComments(params?: {
  type?: string
  page?: number
  limit?: number
}) {
  const api = useApi()

  return useQuery({
    queryKey: ['trending-comments', params],
    queryFn: async () => {
      if (!api) return []
      const searchParams: Record<string, string> = {}
      if (params?.type) searchParams.type = params.type
      if (params?.page) searchParams.page = String(params.page)
      if (params?.limit) searchParams.limit = String(params.limit)
      const res = await api.GET('/v1/comments/trending', {
        params: { query: searchParams as Record<string, never> },
      } as never)
      return (res.data as unknown as unknown[]) ?? []
    },
    enabled: !!api,
  })
}

export function useRecentComments(params?: {
  type?: string
  page?: number
  limit?: number
}) {
  const api = useApi()

  return useQuery({
    queryKey: ['recent-comments', params],
    queryFn: async () => {
      if (!api) return []
      const searchParams: Record<string, string> = {}
      if (params?.type) searchParams.type = params.type
      if (params?.page) searchParams.page = String(params.page)
      if (params?.limit) searchParams.limit = String(params.limit)
      const res = await api.GET('/v1/comments/recent', {
        params: { query: searchParams as Record<string, never> },
      } as never)
      return (res.data as unknown as unknown[]) ?? []
    },
    enabled: !!api,
  })
}
