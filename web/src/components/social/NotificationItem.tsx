import { useNavigate } from '@tanstack/react-router'
import {
  UserPlus,
  MessageSquare,
  Heart,
  Bookmark,
  Star,
  Bell,
} from 'lucide-react'
import { cn } from '@/lib/utils'
import type { Notification } from '@/api/hooks/useNotifications'

const NOTIFICATION_ICONS: Record<string, React.ReactNode> = {
  follow: <UserPlus className="size-4" />,
  comment: <MessageSquare className="size-4" />,
  like: <Heart className="size-4" />,
  watchlist: <Bookmark className="size-4" />,
  recommendation: <Star className="size-4" />,
}

const NOTIFICATION_COLORS: Record<string, string> = {
  follow: 'text-cyan-400 bg-cyan-400/10',
  comment: 'text-purple-400 bg-purple-400/10',
  like: 'text-red-400 bg-red-400/10',
  watchlist: 'text-blue-400 bg-blue-400/10',
  recommendation: 'text-yellow-400 bg-yellow-400/10',
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

interface NotificationItemProps {
  notification: Notification
  onClick?: () => void
  className?: string
}

export function NotificationItem({ notification, onClick, className }: NotificationItemProps) {
  const navigate = useNavigate()

  const handleClick = () => {
    onClick?.()
    if (notification.item) {
      const type = notification.item.type
      const id = notification.item.id
      const routes: Record<string, string> = {
        movie: `/movies/${id}`,
        show: `/shows/${id}`,
        episode: `/episodes/${id}`,
        list: `/lists/${id}`,
      }
      const route = routes[type]
      if (route) navigate({ to: route })
    }
  }

  return (
    <div
      className={cn(
        'flex items-start gap-3 rounded-[--sf-radius] p-3 transition-colors hover:bg-white/[0.03] cursor-pointer',
        !notification.read && 'bg-sf-accent/5',
        className,
      )}
      onClick={handleClick}
    >
      {/* Icon */}
      <div
        className={cn(
          'mt-0.5 grid size-8 shrink-0 place-items-center rounded-full',
          NOTIFICATION_COLORS[notification.type] ?? 'text-sf-ink-muted bg-white/5'
        )}
      >
        {NOTIFICATION_ICONS[notification.type] ?? <Bell className="size-4" />}
      </div>

      {/* Content */}
      <div className="min-w-0 flex-1">
        <p className="text-sm text-sf-ink">
          {notification.message}
        </p>
        <p className="mt-0.5 text-xs text-sf-ink-muted">
          {timeAgo(notification.created_at)}
        </p>
      </div>

      {/* Unread dot */}
      {!notification.read && (
        <div className="mt-2 size-2 shrink-0 rounded-full bg-sf-accent-2" />
      )}
    </div>
  )
}
