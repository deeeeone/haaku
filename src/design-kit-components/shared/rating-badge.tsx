import { cn } from '../../lib/utils'

export type RatingBadgeProps = {
  score: string
  className?: string
}

export function RatingBadge({ score, className }: RatingBadgeProps) {
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 rounded-full bg-black/60 px-2.5 py-1 text-xs font-bold text-white backdrop-blur-sm',
        className
      )}
    >
      <svg
        viewBox="0 0 24 24"
        className="size-3 fill-sf-accent-2"
        aria-hidden="true"
      >
        <path d="M12 2l2.9 6.2 6.6.8-4.9 4.6 1.3 6.6L12 17l-5.9 3.2 1.3-6.6L2.5 9l6.6-.8z" />
      </svg>
      {score}
    </span>
  )
}
RatingBadge.displayName = 'RatingBadge'
