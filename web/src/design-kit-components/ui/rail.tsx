import { ChevronLeft, ChevronRight } from 'lucide-react'
import * as React from 'react'
import { cn } from '../../lib/utils'
import { SectionHead } from './section-head'

export type RailProps = {
  title?: React.ReactNode
  eyebrow?: string
  action?: string
  onAction?: () => void
  accent?: 'purple' | 'cyan'
  children: React.ReactNode
  className?: string
  /** Where the prev/next controls sit. */
  controls?: 'floating' | 'top'
}

export function Rail({
  title,
  eyebrow,
  action,
  onAction,
  accent = 'purple',
  children,
  className,
  controls = 'floating',
}: RailProps) {
  const ref = React.useRef<HTMLDivElement>(null)

  const scroll = (dir: 1 | -1) => {
    const el = ref.current
    if (!el) return
    const card = el.querySelector<HTMLElement>('[data-rail-card]')
    const gap = parseFloat(getComputedStyle(el).columnGap || '16')
    const step = card ? card.offsetWidth + (Number.isNaN(gap) ? 16 : gap) : 320
    el.scrollBy({ left: dir * step * 2, behavior: 'smooth' })
  }

  return (
    <section className={cn('space-y-3', className)}>
      {title && (
        <div className="relative">
          <SectionHead
            eyebrow={eyebrow}
            title={title}
            accent={accent}
            action={action}
            onAction={onAction}
          />
          {controls === 'top' && (
            <RailControls
              className="absolute right-0 top-0"
              onPrev={() => scroll(-1)}
              onNext={() => scroll(1)}
            />
          )}
        </div>
      )}

      <div className="relative">
        <div
          ref={ref}
          className="mask-rail scrollbar-hide flex gap-(--sf-rail-gap) overflow-x-auto scroll-smooth pb-2"
        >
          {children}
        </div>
        {controls === 'floating' && (
          <RailControls
            className="hidden md:flex"
            onPrev={() => scroll(-1)}
            onNext={() => scroll(1)}
          />
        )}
      </div>
    </section>
  )
}
Rail.displayName = 'Rail'

function RailControls({
  onPrev,
  onNext,
  className,
}: {
  onPrev: () => void
  onNext: () => void
  className?: string
}) {
  return (
    <div className={cn('gap-2', className)}>
      <button
        type="button"
        aria-label="Scroll back"
        onClick={onPrev}
        className="glass absolute top-1/2 left-3 z-10 flex size-10 -translate-y-1/2 cursor-pointer items-center justify-center rounded-full text-sf-ink transition-all hover:bg-sf-panel-2 hover:text-sf-ink"
      >
        <ChevronLeft className="size-4" />
      </button>
      <button
        type="button"
        aria-label="Scroll forward"
        onClick={onNext}
        className="glass absolute top-1/2 right-3 z-10 flex size-10 -translate-y-1/2 cursor-pointer items-center justify-center rounded-full text-sf-ink transition-all hover:bg-sf-panel-2 hover:text-sf-ink"
      >
        <ChevronRight className="size-4" />
      </button>
    </div>
  )
}
