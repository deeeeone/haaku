import { useNavigate } from '@tanstack/react-router'
import { Calendar, Clock } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { CalendarEpisode } from '@/api/hooks/useCalendar'

interface CalendarEntryProps {
  item: CalendarEpisode
  className?: string
}

export function CalendarEntry({ item, className }: CalendarEntryProps) {
  const navigate = useNavigate()
  const episode = item.episode
  const show = item.show
  const episodeType = item.type

  const typeLabel =
    episodeType === 'premiere' ? 'Season Premiere'
    : episodeType === 'finale' ? 'Season Finale'
    : episodeType === 'new' ? 'New Show'
    : null

  const episodeStr = `S${String(episode.season).padStart(2, '0')}E${String(episode.number).padStart(2, '0')}`

  return (
    <div
      className={cn(
        'flex gap-3 rounded-[--sf-radius] p-2.5 transition-colors hover:bg-white/[0.04] cursor-pointer',
        className
      )}
      onClick={() => navigate({ to: `/episodes/${episode.ids.trakt}` })}
    >
      {/* Show poster thumbnail */}
      <div className="h-14 w-10 shrink-0 overflow-hidden rounded">
        {show.images?.poster ? (
          <img
            src={show.images.poster}
            alt={show.title}
            className="h-full w-full object-cover"
          />
        ) : (
          <div className="flex h-full w-full items-center justify-center bg-white/5 text-sf-ink-muted">
            <Calendar className="size-4" />
          </div>
        )}
      </div>

      {/* Info */}
      <div className="min-w-0 flex-1">
        <p className="truncate text-sm font-medium text-sf-ink">
          {show.title}
        </p>
        <p className="truncate text-xs text-sf-ink-muted">
          {episodeStr}: {episode.title || `Episode ${episode.number}`}
        </p>
        <div className="mt-1 flex items-center gap-1.5">
          {episode.first_aired && (
            <span className="inline-flex items-center gap-0.5 text-[10px] text-sf-ink-muted">
              <Clock className="size-2.5" />
              {new Date(episode.first_aired).toLocaleTimeString([], {
                hour: '2-digit',
                minute: '2-digit',
              })}
            </span>
          )}
          {typeLabel && (
            <span
              className={cn(
                'rounded-full px-1.5 py-0.5 text-[10px] font-medium',
                episodeType === 'premiere'
                  ? 'bg-amber-400/10 text-amber-400'
                  : episodeType === 'finale'
                    ? 'bg-red-400/10 text-red-400'
                    : 'bg-green-400/10 text-green-400'
              )}
            >
              {typeLabel}
            </span>
          )}
        </div>
      </div>
    </div>
  )
}
