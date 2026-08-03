import { cn } from '../../lib/utils'

export type TypeBadgeProps = {
  type: 'movie' | 'series' | 'episode'
  className?: string
}

const LABELS = { movie: 'MOVIE', series: 'SERIES', episode: 'EPISODE' }

export function TypeBadge({ type, className }: TypeBadgeProps) {
  return (
    <span
      className={cn(
        'inline-flex items-center rounded-full bg-black/60 px-2 py-0.5 text-[9px] font-bold tracking-[0.16em] text-white backdrop-blur-sm',
        className
      )}
    >
      {LABELS[type]}
    </span>
  )
}
TypeBadge.displayName = 'TypeBadge'
