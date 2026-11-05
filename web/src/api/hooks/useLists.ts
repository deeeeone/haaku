import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import type { ApiClient } from '@/api/client'
import type {
  AddListItemPayload,
  Collaborator,
  CreateListPayload,
  CreateSmartListPayload,
  InviteCollaboratorPayload,
  ListComment,
  ListItem,
  ListLike,
  PopularList,
  RemoveListItemPayload,
  ReorderListItemPayload,
  SearchResult,
  SmartList,
  TraktList,
  TrendingList,
  UpdateListPayload,
  UpdateSmartListPayload,
} from '@/api/types/lists'

// The generated schema uses content?: never and params?: never for many endpoints.
// We cast the method to any to bypass strict types; runtime behavior is correct.

// ─── Keys ───────────────────────────────────────────────
export const listKeys = {
  all: ['lists'] as const,
  mine: () => [...listKeys.all, 'mine'] as const,
  collaborations: () => [...listKeys.all, 'collaborations'] as const,
  detail: (id: number | string) => [...listKeys.all, 'detail', id] as const,
  items: (id: number | string, page?: number) =>
    [...listKeys.all, 'items', id, page ?? 1] as const,
  comments: (id: number | string) => [...listKeys.all, 'comments', id] as const,
  collaborators: (id: number | string) =>
    [...listKeys.all, 'collaborators', id] as const,
  likes: (id: number | string) => [...listKeys.all, 'likes', id] as const,
  smartLists: () => [...listKeys.all, 'smart'] as const,
  smartDetail: (id: number | string) =>
    [...listKeys.all, 'smart', 'detail', id] as const,
  smartItems: (id: number | string, page?: number) =>
    [...listKeys.all, 'smart', 'items', id, page ?? 1] as const,
  trending: () => [...listKeys.all, 'trending'] as const,
  popular: (period: string) => [...listKeys.all, 'popular', period] as const,
}

const ME = 'me'

// ─── My Lists ────────────────────────────────────────────
export function useMyLists(client: ApiClient | null) {
  return useQuery({
    queryKey: listKeys.mine(),
    queryFn: async (): Promise<TraktList[]> => {
      const { data, error } = await (client!.GET as any)(
        '/v1/users/{id}/lists',
        { params: { path: { id: ME } } }
      )
      if (error) throw error
      return (data as TraktList[]) ?? []
    },
    enabled: !!client,
  })
}

// ─── Collaborations ──────────────────────────────────────
export function useCollaborations(client: ApiClient | null) {
  return useQuery({
    queryKey: listKeys.collaborations(),
    queryFn: async (): Promise<TraktList[]> => {
      const { data, error } = await (client!.GET as any)(
        '/v1/users/me/lists/collaborations'
      )
      if (error) throw error
      return (data as TraktList[]) ?? []
    },
    enabled: !!client,
  })
}

// ─── Create List ─────────────────────────────────────────
export function useCreateList(client: ApiClient | null) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (payload: CreateListPayload): Promise<TraktList> => {
      const { data, error } = await (client!.POST as any)(
        '/v1/users/me/lists',
        { body: payload }
      )
      if (error) throw error
      return data as TraktList
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: listKeys.mine() })
      qc.invalidateQueries({ queryKey: listKeys.collaborations() })
      toast.success('List created')
    },
    onError: () => {
      toast.error('Failed to create list')
    },
  })
}

// ─── Update List ─────────────────────────────────────────
export function useUpdateList(client: ApiClient | null) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async ({
      id,
      payload,
    }: {
      id: number | string
      payload: UpdateListPayload
    }): Promise<TraktList> => {
      const { data, error } = await (client!.PUT as any)('/v1/lists/{id}', {
        params: { path: { id: String(id) } },
        body: payload,
      })
      if (error) throw error
      return data as TraktList
    },
    onSuccess: (_, { id }) => {
      qc.invalidateQueries({ queryKey: listKeys.detail(id) })
      qc.invalidateQueries({ queryKey: listKeys.mine() })
      toast.success('List updated')
    },
    onError: () => {
      toast.error('Failed to update list')
    },
  })
}

// ─── Delete List ─────────────────────────────────────────
export function useDeleteList(client: ApiClient | null) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (id: number | string) => {
      const { error } = await (client!.DELETE as any)('/v1/lists/{id}', {
        params: { path: { id: String(id) } },
      })
      if (error) throw error
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: listKeys.mine() })
      qc.invalidateQueries({ queryKey: listKeys.collaborations() })
      toast.success('List deleted')
    },
    onError: () => {
      toast.error('Failed to delete list')
    },
  })
}

// ─── List Detail ─────────────────────────────────────────
export function useListDetail(client: ApiClient | null, id: number | string) {
  return useQuery({
    queryKey: listKeys.detail(id),
    queryFn: async (): Promise<TraktList> => {
      const { data, error } = await (client!.GET as any)('/v1/lists/{id}', {
        params: { path: { id: String(id) } },
      })
      if (error) throw error
      return data as TraktList
    },
    enabled: !!client && !!id,
  })
}

// ─── List Items ──────────────────────────────────────────
export function useListItems(
  client: ApiClient | null,
  id: number | string,
  page: number = 1,
  limit: number = 20
) {
  return useQuery({
    queryKey: listKeys.items(id, page),
    queryFn: async (): Promise<ListItem[]> => {
      const { data, error } = await (client!.GET as any)(
        '/v1/lists/{id}/items',
        { params: { path: { id: String(id) }, query: { page, limit } } }
      )
      if (error) throw error
      return (data as ListItem[]) ?? []
    },
    enabled: !!client && !!id,
  })
}

// ─── Add Items ───────────────────────────────────────────
export function useAddToList(client: ApiClient | null) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async ({
      id,
      payload,
    }: {
      id: number | string
      payload: AddListItemPayload
    }) => {
      const { data, error } = await (client!.POST as any)(
        '/v1/lists/{id}/items',
        { params: { path: { id: String(id) } }, body: payload }
      )
      if (error) throw error
      return data
    },
    onSuccess: (_, { id }) => {
      qc.invalidateQueries({ queryKey: listKeys.items(id) })
      qc.invalidateQueries({ queryKey: listKeys.detail(id) })
      toast.success('Items added to list')
    },
    onError: () => {
      toast.error('Failed to add items')
    },
  })
}

// ─── Remove Items ────────────────────────────────────────
export function useRemoveFromList(client: ApiClient | null) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async ({
      id,
      payload,
    }: {
      id: number | string
      payload: RemoveListItemPayload
    }) => {
      const { error } = await (client!.POST as any)(
        '/v1/lists/{id}/items/remove',
        { params: { path: { id: String(id) } }, body: payload }
      )
      if (error) throw error
    },
    onSuccess: (_, { id }) => {
      qc.invalidateQueries({ queryKey: listKeys.items(id) })
      qc.invalidateQueries({ queryKey: listKeys.detail(id) })
      toast.success('Items removed from list')
    },
    onError: () => {
      toast.error('Failed to remove items')
    },
  })
}

// ─── Reorder Items ───────────────────────────────────────
export function useReorderList(client: ApiClient | null) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async ({
      id,
      payload,
    }: {
      id: number | string
      payload: ReorderListItemPayload
    }) => {
      const { error } = await (client!.PUT as any)(
        '/v1/lists/{id}/items/reorder',
        { params: { path: { id: String(id) } }, body: payload }
      )
      if (error) throw error
    },
    onSuccess: (_, { id }) => {
      qc.invalidateQueries({ queryKey: listKeys.items(id) })
    },
    onError: () => {
      toast.error('Failed to reorder items')
    },
  })
}

// ─── Like / Unlike ───────────────────────────────────────
export function useLikeList(client: ApiClient | null) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (id: number | string) => {
      const { error } = await (client!.POST as any)('/v1/lists/{id}/like', {
        params: { path: { id: String(id) } },
      })
      if (error) throw error
    },
    onSuccess: (_, id) => {
      qc.invalidateQueries({ queryKey: listKeys.detail(id) })
      qc.invalidateQueries({ queryKey: listKeys.likes(id) })
      qc.invalidateQueries({ queryKey: listKeys.mine() })
    },
    onError: () => {
      toast.error('Failed to like list')
    },
  })
}

export function useUnlikeList(client: ApiClient | null) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (id: number | string) => {
      const { error } = await (client!.DELETE as any)('/v1/lists/{id}/like', {
        params: { path: { id: String(id) } },
      })
      if (error) throw error
    },
    onSuccess: (_, id) => {
      qc.invalidateQueries({ queryKey: listKeys.detail(id) })
      qc.invalidateQueries({ queryKey: listKeys.likes(id) })
      qc.invalidateQueries({ queryKey: listKeys.mine() })
    },
    onError: () => {
      toast.error('Failed to unlike list')
    },
  })
}

// ─── Likes ───────────────────────────────────────────────
export function useListLikes(client: ApiClient | null, id: number | string) {
  return useQuery({
    queryKey: listKeys.likes(id),
    queryFn: async (): Promise<ListLike[]> => {
      const { data, error } = await (client!.GET as any)(
        '/v1/lists/{id}/likes',
        { params: { path: { id: String(id) } } }
      )
      if (error) throw error
      return (data as ListLike[]) ?? []
    },
    enabled: !!client && !!id,
  })
}

// ─── Comments ────────────────────────────────────────────
export function useListComments(client: ApiClient | null, id: number | string) {
  return useQuery({
    queryKey: listKeys.comments(id),
    queryFn: async (): Promise<ListComment[]> => {
      const { data, error } = await (client!.GET as any)(
        '/v1/lists/{id}/comments',
        { params: { path: { id: String(id) } } }
      )
      if (error) throw error
      return (data as ListComment[]) ?? []
    },
    enabled: !!client && !!id,
  })
}

// ─── Collaborators ──────────────────────────────────────
export function useListCollaborators(
  client: ApiClient | null,
  id: number | string
) {
  return useQuery({
    queryKey: listKeys.collaborators(id),
    queryFn: async (): Promise<Collaborator[]> => {
      const { data, error } = await (client!.GET as any)(
        '/v1/lists/{id}/collaborators',
        { params: { path: { id: String(id) } } }
      )
      if (error) throw error
      return (data as Collaborator[]) ?? []
    },
    enabled: !!client && !!id,
  })
}

export function useInviteCollaborator(client: ApiClient | null) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async ({
      id,
      payload,
    }: {
      id: number | string
      payload: InviteCollaboratorPayload
    }) => {
      const { error } = await (client!.POST as any)(
        '/v1/lists/{id}/collaborators',
        { params: { path: { id: String(id) } }, body: payload }
      )
      if (error) throw error
    },
    onSuccess: (_, { id }) => {
      qc.invalidateQueries({ queryKey: listKeys.collaborators(id) })
      toast.success('Collaborator invited')
    },
    onError: () => {
      toast.error('Failed to invite collaborator')
    },
  })
}

export function useRemoveCollaborator(client: ApiClient | null) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async ({
      id,
      userId,
    }: {
      id: number | string
      userId: number | string
    }) => {
      const { error } = await (client!.DELETE as any)(
        '/v1/lists/{id}/collaborators/{user_id}',
        { params: { path: { id: String(id), user_id: String(userId) } } }
      )
      if (error) throw error
    },
    onSuccess: (_, { id }) => {
      qc.invalidateQueries({ queryKey: listKeys.collaborators(id) })
      toast.success('Collaborator removed')
    },
    onError: () => {
      toast.error('Failed to remove collaborator')
    },
  })
}

// ─── Smart Lists ─────────────────────────────────────────
export function useMySmartLists(client: ApiClient | null) {
  return useQuery({
    queryKey: listKeys.smartLists(),
    queryFn: async (): Promise<SmartList[]> => {
      const { data, error } = await (client!.GET as any)(
        '/v1/users/me/smart-lists'
      )
      if (error) throw error
      return (data as SmartList[]) ?? []
    },
    enabled: !!client,
  })
}

export function useCreateSmartList(client: ApiClient | null) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (payload: CreateSmartListPayload): Promise<SmartList> => {
      const { data, error } = await (client!.POST as any)(
        '/v1/users/me/smart-lists',
        { body: payload }
      )
      if (error) throw error
      return data as SmartList
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: listKeys.smartLists() })
      toast.success('Smart list created')
    },
    onError: () => {
      toast.error('Failed to create smart list')
    },
  })
}

export function useUpdateSmartList(client: ApiClient | null) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async ({
      id,
      payload,
    }: {
      id: number | string
      payload: UpdateSmartListPayload
    }) => {
      const { error } = await (client!.PUT as any)('/v1/smart-lists/{id}', {
        params: { path: { id: String(id) } },
        body: payload,
      })
      if (error) throw error
    },
    onSuccess: (_, { id }) => {
      qc.invalidateQueries({ queryKey: listKeys.smartDetail(id) })
      qc.invalidateQueries({ queryKey: listKeys.smartLists() })
      toast.success('Smart list updated')
    },
    onError: () => {
      toast.error('Failed to update smart list')
    },
  })
}

export function useDeleteSmartList(client: ApiClient | null) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (id: number | string) => {
      const { error } = await (client!.DELETE as any)('/v1/smart-lists/{id}', {
        params: { path: { id: String(id) } },
      })
      if (error) throw error
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: listKeys.smartLists() })
      toast.success('Smart list deleted')
    },
    onError: () => {
      toast.error('Failed to delete smart list')
    },
  })
}

export function useSmartListDetail(
  client: ApiClient | null,
  id: number | string
) {
  return useQuery({
    queryKey: listKeys.smartDetail(id),
    queryFn: async (): Promise<SmartList> => {
      const { data, error } = await (client!.GET as any)(
        '/v1/smart-lists/{id}',
        { params: { path: { id: String(id) } } }
      )
      if (error) throw error
      return data as SmartList
    },
    enabled: !!client && !!id,
  })
}

export function useSmartListItems(
  client: ApiClient | null,
  id: number | string,
  page: number = 1,
  limit: number = 20
) {
  return useQuery({
    queryKey: listKeys.smartItems(id, page),
    queryFn: async (): Promise<ListItem[]> => {
      const { data, error } = await (client!.GET as any)(
        '/v1/smart-lists/{id}/items',
        { params: { path: { id: String(id) }, query: { page, limit } } }
      )
      if (error) throw error
      return (data as ListItem[]) ?? []
    },
    enabled: !!client && !!id,
  })
}

// ─── Community Lists ─────────────────────────────────────
export function useTrendingLists(client: ApiClient | null) {
  return useQuery({
    queryKey: listKeys.trending(),
    queryFn: async (): Promise<TrendingList[]> => {
      const { data, error } = await (client!.GET as any)('/v1/lists/trending')
      if (error) throw error
      return (data as TrendingList[]) ?? []
    },
    enabled: !!client,
  })
}

export function usePopularLists(
  client: ApiClient | null,
  period: 'weekly' | 'monthly' | 'all' = 'weekly'
) {
  return useQuery({
    queryKey: listKeys.popular(period),
    queryFn: async (): Promise<PopularList[]> => {
      const { data, error } = await (client!.GET as any)('/v1/lists/popular', {
        params: { query: { period } },
      })
      if (error) throw error
      return (data as PopularList[]) ?? []
    },
    enabled: !!client,
  })
}

// ─── Search (for adding items) ───────────────────────────
export function useSearchForList(
  client: ApiClient | null,
  query: string,
  enabled: boolean = false
) {
  return useQuery({
    queryKey: ['search', 'listAdd', query],
    queryFn: async (): Promise<SearchResult[]> => {
      const { data, error } = await (client!.GET as any)('/v1/search', {
        params: {
          query: { query, type: 'movie,show', limit: 20 },
        },
      })
      if (error) throw error
      const results: SearchResult[] = []
      const raw = data as any
      if (Array.isArray(raw)) {
        for (const item of raw) {
          if (item.type === 'movie' && item.movie) {
            results.push({ type: 'movie', movie: item.movie })
          } else if (item.type === 'show' && item.show) {
            results.push({ type: 'show', show: item.show })
          }
        }
      }
      return results
    },
    enabled: enabled && query.length >= 2,
  })
}
