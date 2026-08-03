import { useNavigate } from '@tanstack/react-router'
import { Tv, Star, Bookmark, MessageSquare, Heart, UserPlus } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { ActivityItem as ActivityItemType } from '@/api/types'

const ACTIVITY_ICONS: Record<string, React.ReactNode> = {
  'history.added': <Tv className="size-4" />,
  'rating.added': <Star className="size-4" />,
  'watchlist.added': <Bookmark className="size-4" />,
  'comment.created': <MessageSquare className="size-4" />,
  'favorite.added': <Heart className="size-4" />,
  'follow.created': <UserPlus className="size-4" />,
}

const ACTIVITY_COLORS: Record<string, string> = {
  'history.added': 'text-green-400 bg-green-400/10',
  'rating.added': 'text-yellow-400 bg-yellow-400/10',
  'watchlist.added': 'text-blue-400 bg-blue-400/10',
  'comment.created': 'text-purple-400 bg-purple-400/10',
  'favorite.added': 'text-red-400 bg-red-400/10',
  'follow.created': 'text-cyan-400 bg-cyan-400/10',
}

function timeAgo(dateStr: string): string {
  const date = new Date(dateStr)
  const now = new Date()
  const diffMs = now.getTime() - date.getTime()
  const diffSec = Math.floor(diffMs / 1000)
  const diffMin = Math.floor(diffSec / 60)
  const diffHr = Math.floor(diffMin / 60)
  const diffDay = Math.floor(diffHr / 24)

  if (diffSec < 60) return 'just now'
  if (diffMin < 60) return `${diffMin}m ago`
  if (diffHr < 24) return `${diffHr}h ago`
  if (diffDay < 7) return `${diffDay}d ago`
  return date.toLocaleDateString()
}

function getActivityText(item: ActivityItemType): string {
  const username = item.user?.name ?? item.user?.username ?? 'Someone'
  const action = item.action?.replace('.', ' ') ?? ''
  const mediaName = item.movie?.title ?? item.show?.title ?? ''
  const episodeInfo = item.episode
    ? ` S${String(item.episode.season).padStart(2, '0')}E${String(item.episode.number).padStart(2, '0')}`
    : ''
  const listName = item.list?.name ?? ''

  switch (item.action) {
    case 'history.added':
      return mediaName ? `${username} watched ${mediaName}${episodeInfo}` : `${username} watched something`
    case 'rating.added':
      return mediaName ? `${username} rated ${mediaName}${episodeInfo} ${'⭐'.repeat(item.rating ?? 0)}` : `${username} rated something`
    case 'watchlist.added':
      return mediaName ? `${username} added ${mediaName} to their watchlist` : `${username} added to watchlist`
    case 'comment.created':
      return mediaName ? `${username} commented on ${mediaName}${episodeInfo}` : `${username} left a comment`
    case 'favorite.added':
      return mediaName ? `${username} favorited ${mediaName}` : `${username} added a favorite`
    case 'follow.created':
      return `${username} started following someone`
    case 'list.liked':
      return listName ? `${username} liked list "${listName}"` : `${username} liked a list`
    default:
      return `${username} ${action}`
  }
}

function getMediaLink(item: ActivityItemType): string | null {
  if (item.movie) return `/movies/${item.movie.ids.trakt}`
  if (item.show) {
    if (item.episode) return `/episodes/${item.episode.ids.trakt}`
    return `/shows/${item.show.ids.trakt}`
  }
  if (item.list) return `/lists/${item.list.ids.trakt}`
  return null
}

interface ActivityItemProps {
  item: ActivityItemType
  className?: string
}

export function ActivityItem({ item, className }: ActivityItemProps) {
  const navigate = useNavigate()
  const link = getMediaLink(item)

  return (
    <div
      className={cn(
        'flex items-start gap-3 rounded-[--sf-radius] p-3 transition-colors hover:bg-white/[0.03]',
        className
      )}
    >
      {/* Icon */}
      <div
        className={cn(
          'mt-0.5 grid size-8 shrink-0 place-items-center rounded-full',
          ACTIVITY_COLORS[item.action] ?? 'text-sf-ink-muted bg-white/5'
        )}
      >
        {ACTIVITY_ICONS[item.action] ?? <Tv className="size-4" />}
      </div>

      {/* Content */}
      <div className="min-w-0 flex-1">
        <p className="text-sm text-sf-ink">
          {link ? (
            <button
              type="button"
              onClick={() => navigate({ to: link })}
              className="font-medium text-sf-accent-2 hover:underline"
            >
              {getActivityText(item)}
            </button>
          ) : (
            getActivityText(item)
          )}
        </p>
        <p className="mt-0.5 text-xs text-sf-ink-muted">
          {timeAgo(item.timestamps?.start ?? '')}
        </p>
      </div>

      {/* Thumbnail */}
      {(item.movie?.images?.poster || item.show?.images?.poster) && (
        <div
          className="h-12 w-8 shrink-0 cursor-pointer overflow-hidden rounded"
          onClick={() => link && navigate({ to: link })}
        >
          <img
            src={item.movie?.images?.poster ?? item.show?.images?.poster}
            alt=""
            className="h-full w-full object-cover"
          />
        </div>
      )}
    </div>
  )
}
