/**
 * API data type definitions for the trakt-killer API.
 * These mirror the backend response shapes (Trakt-compatible format).
 */

// ─── Common / Utility ────────────────────────────────────────────────

export interface PaginatedResponse<T> {
  data: T[]
  page: number
  limit: number
  total: number
  total_pages: number
}

export interface Ids {
  trakt: number
  slug: string
  imdb?: string
  tmdb?: number
}

export interface ImageSet {
  poster?: string
  backdrop?: string
  headshot?: string
  logo?: string
  thumb?: string
}

// ─── Media ────────────────────────────────────────────────────────────

export interface Movie {
  title: string
  year: number
  ids: Ids
  overview?: string
  released?: string
  runtime?: number
  country?: string
  trailer?: string
  homepage?: string
  status?: string
  rating?: number
  votes?: number
  comment_count?: number
  language?: string
  available_translations?: string[]
  genres?: string[]
  certification?: string
  images?: ImageSet
}

export interface Show {
  title: string
  year: number
  ids: Ids
  overview?: string
  first_aired?: string
  airs?: {
    day?: string
    time?: string
    timezone?: string
  }
  runtime?: number
  certification?: string
  network?: string
  country?: string
  trailer?: string
  homepage?: string
  status?: string
  rating?: number
  votes?: number
  comment_count?: number
  language?: string
  available_translations?: string[]
  genres?: string[]
  aired_episodes?: number
  images?: ImageSet
}

export interface Episode {
  season: number
  number: number
  title: string
  ids: Ids
  overview?: string
  first_aired?: string
  runtime?: number
  rating?: number
  votes?: number
  comment_count?: number
  images?: ImageSet
}

export interface Season {
  number: number
  ids: Ids
  rating?: number
  votes?: number
  episode_count?: number
  aired_episodes?: number
  title?: string
  overview?: string
  first_aired?: string
  images?: ImageSet
  episodes?: Episode[]
}

export interface Person {
  name: string
  ids: Ids
  biography?: string
  birthday?: string
  death?: string
  birthplace?: string
  homepage?: string
  images?: ImageSet
}

export interface CastMember {
  character: string
  person: Person
  movie?: Movie
  show?: Show
}

export interface CrewMember {
  job: string
  person: Person
  movie?: Movie
  show?: Show
}

export interface CreditResponse {
  cast?: CastMember[]
  crew?: CrewMember[]
}

// ─── Search ───────────────────────────────────────────────────────────

export type SearchType =
  | 'movie'
  | 'show'
  | 'episode'
  | 'person'
  | 'list'
  | 'season'

export interface SearchResult {
  type: SearchType
  score: number
  movie?: Movie
  show?: Show
  episode?: Episode
  person?: Person
  list?: TraktList
}

export interface SearchResponse extends PaginatedResponse<SearchResult> {}

// ─── Stats ────────────────────────────────────────────────────────────

export interface MediaStats {
  watchers?: number
  plays?: number
  collectors?: number
  collected_episodes?: number
  comments?: number
  lists?: number
  votes?: number
  recommended?: number
}

export interface RatingDistribution {
  rating: number
  votes: number
  percentage: number
}

// ─── Lists ────────────────────────────────────────────────────────────

export interface TraktList {
  name: string
  description?: string
  privacy?: string
  display_numbers?: boolean
  allow_comments?: boolean
  sort_by?: string
  sort_how?: string
  created_at?: string
  updated_at?: string
  item_count?: number
  comment_count?: number
  likes?: number
  ids: Ids
  user?: UserRef
}

export interface ListItem {
  rank: number
  id: number
  listed_at: string
  type: SearchType
  movie?: Movie
  show?: Show
  episode?: Episode
  person?: Person
}

// ─── Users ────────────────────────────────────────────────────────────

export interface UserRef {
  username: string
  private?: boolean
  name?: string
  vip?: boolean
  vip_ep?: boolean
  ids: {
    slug: string
    uuid?: string
  }
}

// ─── Sync ─────────────────────────────────────────────────────────────

export interface SyncItem {
  ids: Ids
  collected_at?: string
  watched_at?: string
  rated_at?: string
  rating?: number
  listed_at?: string
}

export interface CollectionItem {
  rank?: number
  id: number
  listed_at?: string
  collected_at?: string
  type: SearchType
  movie?: Movie
  show?: Show
  episode?: Episode
  season?: Season
  media_type?: string
  resolution?: string
  hdr?: string
  audio?: string
  audio_channels?: string
  '3d'?: boolean
}

export interface CollectionResponse extends PaginatedResponse<CollectionItem> {}

export interface HistoryItem {
  id: number
  watched_at: string
  action?: string
  type: SearchType
  movie?: Movie
  show?: Show
  season?: Season
  episode?: Episode
}

export interface HistoryResponse extends PaginatedResponse<HistoryItem> {}

export interface PlaybackItem {
  id: number
  progress: number
  paused_at: string
  type: SearchType
  movie?: Movie
  episode?: Episode
  show?: Show
}

export interface ActivityItem {
  id: number
  timestamps: { start: string }
  action: string
  type: string
  user: {
    username: string
    private: boolean
    name: string
    vip?: boolean
    vip_ep?: boolean
    ids: { slug: string }
    avatar?: string
  }
  movie?: Movie
  show?: Show
  season?: Season
  episode?: Episode
  rating?: number
  list?: {
    name: string
    ids: { trakt: number; slug: string }
  }
}

export interface RecommendationItem {
  rank: number
  listed_at: string
  type: string
  score: number
  movie?: Movie
  show?: Show
  total_user_count?: number
  total_friend_count?: number
}

export interface WatchlistReorderBody {
  rank: Array<{
    id: number
    rank: number
  }>
}

export interface WatchlistItem {
  rank: number
  id: number
  listed_at: string
  type: SearchType
  movie?: Movie
  show?: Show
  episode?: Episode
}

export interface WatchlistResponse extends PaginatedResponse<WatchlistItem> {}

export interface FavoriteItem {
  rank: number
  id: number
  listed_at: string
  type: SearchType
  movie?: Movie
  show?: Show
}

export interface FavoriteResponse extends PaginatedResponse<FavoriteItem> {}

export interface RatingItem {
  rated_at: string
  rating: number
  type: SearchType
  movie?: Movie
  show?: Show
  episode?: Episode
  season?: Season
}

export interface RatingResponse extends PaginatedResponse<RatingItem> {}

export interface SyncActionPayload {
  movies?: { ids: Ids }[]
  shows?: { ids: Ids }[]
  episodes?: { ids: Ids }[]
  seasons?: { ids: Ids }[]
  people?: { ids: Ids }[]
}

export interface SyncAddResponse {
  added: {
    movies: number
    shows: number
    episodes: number
    seasons: number
    people: number
  }
  existing: {
    movies: number
    shows: number
    episodes: number
    seasons: number
    people: number
  }
  not_found: {
    movies: Ids[]
    shows: Ids[]
    episodes: Ids[]
    seasons: Ids[]
    people: Ids[]
  }
}

export interface SyncRemoveResponse {
  deleted: {
    movies: number
    shows: number
    episodes: number
    seasons: number
    people: number
  }
  not_found: {
    movies: Ids[]
    shows: Ids[]
    episodes: Ids[]
    seasons: Ids[]
    people: Ids[]
  }
}

// ─── Trending / Popular ───────────────────────────────────────────────

export interface TrendingItem<T = Movie | Show> {
  watchers: number
  movie?: T extends Movie ? Movie : never
  show?: T extends Show ? Show : never
}

// ─── Watch Providers ──────────────────────────────────────────────────

export interface WatchProvider {
  name: string
  logo?: string
  link?: string
  types?: string[]
}

export interface WatchProviderCountry {
  country: string
  providers: WatchProvider[]
}

// ─── Comments ─────────────────────────────────────────────────────────

export interface Comment {
  id: number
  parent_id?: number
  created_at: string
  updated_at?: string
  comment: string
  spoiler: boolean
  review: boolean
  replies: number
  likes: number
  user_rating?: number
  user: UserRef
}

export interface CommentResponse extends PaginatedResponse<Comment> {}

// ─── Aliases ──────────────────────────────────────────────────────────

export interface Alias {
  title: string
  country: string
}

// ─── Genre ────────────────────────────────────────────────────────────

export interface Genre {
  name: string
  slug: string
}
