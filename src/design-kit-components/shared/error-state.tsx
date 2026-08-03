import { RefreshCw, TriangleAlert } from 'lucide-react'

import { cn } from '../../lib/utils'

export type ErrorStateProps = {
  title?: string
  description?: string
  onRetry?: () => void
  className?: string
}

export function ErrorState({
  title = 'Something went wrong',
  description = 'We could not load this content. Please try again.',
  onRetry,
  className,
}: ErrorStateProps) {
  return (
    <div
      className={cn(
        'glass flex flex-col items-center gap-4 rounded-[var(--sf-radius-lg)] px-6 py-14 text-center',
        className
      )}
    >
      <span className="grid size-14 place-items-center rounded-full bg-sf-brand-red/15 text-[#ff7a82]">
        <TriangleAlert className="size-6" />
      </span>
      <div className="space-y-1.5">
        <h3 className="font-display text-xl font-bold text-sf-ink">{title}</h3>
        <p className="mx-auto max-w-sm text-sm text-sf-ink-muted">
          {description}
        </p>
      </div>
      {onRetry && (
        <button
          type="button"
          onClick={onRetry}
          className="glass inline-flex cursor-pointer items-center gap-2 rounded-full px-4 py-2 text-sm font-semibold text-sf-ink transition-colors hover:bg-sf-panel-2"
        >
          <RefreshCw className="size-4" />
          Try again
        </button>
      )}
    </div>
  )
}
ErrorState.displayName = 'ErrorState'
