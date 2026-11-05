// List types for the Lists feature.
// Since the generated schema has content?: never for most endpoints,
// we define full types here based on the trakt API spec.

// ─── List ────────────────────────────────────────────────
export interface ListIds {
  trakt: number
  slug: string
}

export interface ListUser {
  username: string
  private: boolean
  name: string
  vip: boolean
  vip_ep: boolean
  ids: { slug: string }
}

export type ListPrivacy = 'public' | 'private' | 'link_only'

export interface TraktList {
  name: string
  description: string
  privacy: ListPrivacy
  display_numbers: boolean
  allow_comments: boolean
  sort_by: string
  sort_how: string
  created_at: string
  updated_at: string
  item_count: number
  comment_count: number
  likes: number
  ids: ListIds
  user: ListUser
}

export interface CreateListPayload {
  name: string
  description?: string
  privacy?: ListPrivacy
  display_numbers?: boolean
  allow_comments?: boolean
  sort_by?: string
  sort_how?: string
}

export interface UpdateListPayload {
  name?: string
  description?: string
  privacy?: ListPrivacy
  display_numbers?: boolean
  allow_comments?: boolean
  sort_by?: string
  sort_how?: string
}

// ─── List Items ──────────────────────────────────────────
export interface ListItemIds {
  trakt: number
  slug: string
  imdb?: string
  tmdb?: number
}

export interface ListItemMovie {
  title: string
  year: number
  ids: ListItemIds
}

export interface ListItemShow {
  title: string
  year: number
  ids: ListItemIds
}

export interface ListItem {
  rank: number
  id: number
  listed_at: string
  notes?: string
  type: 'movie' | 'show' | 'season' | 'episode' | 'person'
  movie?: ListItemMovie
  show?: ListItemShow
}

export interface AddListItemPayload {
  movies?: { ids: ListItemIds; notes?: string }[]
  shows?: { ids: ListItemIds; notes?: string }[]
  seasons?: { ids: ListItemIds; notes?: string }[]
  episodes?: { ids: ListItemIds; notes?: string }[]
  people?: { ids: ListItemIds; notes?: string }[]
}

export interface RemoveListItemPayload {
  movies?: { ids: { trakt: number } }[]
  shows?: { ids: { trakt: number } }[]
  seasons?: { ids: { trakt: number } }[]
  episodes?: { ids: { trakt: number } }[]
  people?: { ids: { trakt: number } }[]
}

export interface ReorderListItemPayload {
  rank: number[]
}

// ─── Comments ────────────────────────────────────────────
export interface ListComment {
  id: number
  parent_id: number
  created_at: string
  updated_at: string
  comment: string
  spoiler: boolean
  review: boolean
  replies: number
  likes: number
  user_rating: number | null
  user: ListUser
}

// ─── Collaborators ───────────────────────────────────────
export interface Collaborator {
  user: ListUser
  role: 'owner' | 'collaborator'
}

export interface InviteCollaboratorPayload {
  username: string
}

// ─── Likes ───────────────────────────────────────────────
export interface ListLike {
  liked_at: string
  user: ListUser
}

// ─── Smart Lists ─────────────────────────────────────────
export interface SmartListFilter {
  type: 'movies' | 'shows'
  genres?: string[]
  year_min?: number
  year_max?: number
  rating_min?: number
  votes_min?: number
  status?: 'returning' | 'ended' | 'canceled'
  watched?: boolean
  collected?: boolean
}

export interface SmartList {
  name: string
  filter: SmartListFilter
  ids: ListIds
  created_at: string
  updated_at: string
  item_count: number
}

export interface CreateSmartListPayload {
  name: string
  filter: SmartListFilter
}

export interface UpdateSmartListPayload {
  name?: string
  filter?: SmartListFilter
}

// ─── Community Lists ─────────────────────────────────────
export interface TrendingList {
  list: TraktList
  watchers: number
}

export interface PopularList {
  list: TraktList
  watchers: number
}

// ─── Paginated Responses ─────────────────────────────────
export interface PaginatedResponse<T> {
  data: T[]
  page: number
  limit: number
  total: number
}

// ─── Search for Add Items ────────────────────────────────
export interface SearchResult {
  type: 'movie' | 'show'
  movie?: ListItemMovie
  show?: ListItemShow
}