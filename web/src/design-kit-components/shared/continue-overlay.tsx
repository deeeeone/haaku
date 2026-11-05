import { Play } from 'lucide-react'
import { cn } from '../../lib/utils'
import { WatchProgress } from './watch-progress'

export type ContinueOverlayProps = {
  title: string
  meta?: string
  progress: number
  className?: string
}

export function ContinueOverlay({
  title,
  meta,
  progress,
  className,
}: ContinueOverlayProps) {
  return (
    <div
      className={cn(
        'glass relative overflow-hidden rounded-[var(--sf-radius)] p-4',
        className
      )}
    >
      <div className="flex items-center justify-between gap-3">
        <div className="min-w-0">
          <p className="truncate text-sm font-semibold text-sf-ink">{title}</p>
          {meta && <p className="truncate text-xs text-sf-ink-faint">{meta}</p>}
        </div>
        <button
          type="button"
          aria-label={`Resume ${title}`}
          className="flex size-10 shrink-0 cursor-pointer items-center justify-center rounded-full bg-linear-to-r from-sf-accent to-sf-accent-2 text-white shadow-[0_6px_22px_rgba(140,107,255,0.4)] transition-transform active:scale-95"
        >
          <Play className="size-4 translate-x-0.5 fill-current" />
        </button>
      </div>
      <div className="mt-3">
        <WatchProgress value={progress} />
      </div>
    </div>
  )
}
ContinueOverlay.displayName = 'ContinueOverlay'
