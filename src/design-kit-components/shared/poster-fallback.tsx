import { Film } from 'lucide-react'

import { cn } from '../../lib/utils'

export type PosterFallbackProps = {
  title?: string
  aspect?: '16:9' | '2:3'
  className?: string
}

export function PosterFallback({
  title,
  aspect = '16:9',
  className,
}: PosterFallbackProps) {
  return (
    <div
      className={cn(
        'grid h-full w-full place-items-center bg-linear-to-br from-sf-bg-3 to-sf-bg-2',
        aspect === '2:3' ? 'aspect-[2/3]' : 'aspect-video',
        className
      )}
    >
      <div className="flex flex-col items-center gap-2 text-sf-ink-hint">
        <Film className="size-6" />
        {title && (
          <span className="font-display text-2xl font-bold text-sf-ink-faint">
            {title.charAt(0)}
          </span>
        )}
      </div>
    </div>
  )
}
PosterFallback.displayName = 'PosterFallback'
