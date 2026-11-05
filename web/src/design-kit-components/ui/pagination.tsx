import { ChevronLeft, ChevronRight } from 'lucide-react'
import * as React from 'react'

import { cn } from '../../lib/utils'

export type PaginationProps = {
  page: number
  total: number
  siblings?: number
  onChange: (page: number) => void
  className?: string
}

function range(start: number, end: number) {
  return Array.from({ length: end - start + 1 }, (_, i) => start + i)
}

export function Pagination({
  page,
  total,
  siblings = 1,
  onChange,
  className,
}: PaginationProps) {
  const items = React.useMemo(() => {
    const left = Math.max(2, page - siblings)
    const right = Math.min(total - 1, page + siblings)
    const set = new Set<number>([1, total])
    for (const n of range(left, right)) set.add(n)

    const out: Array<number | 'gap'> = []
    let prev = 0
    for (const n of [...set].sort((a, b) => a - b)) {
      if (n - prev > 1) out.push('gap')
      out.push(n)
      prev = n
    }
    return out
  }, [page, total, siblings])

  if (total <= 1) return null

  return (
    <nav
      className={cn('flex items-center gap-1.5', className)}
      aria-label="Pagination"
    >
      <button
        type="button"
        aria-label="Previous page"
        disabled={page <= 1}
        onClick={() => onChange(page - 1)}
        className="glass flex size-9 cursor-pointer items-center justify-center rounded-full text-sf-ink-muted transition-colors hover:bg-sf-panel-2 hover:text-sf-ink disabled:pointer-events-none disabled:opacity-40"
      >
        <ChevronLeft className="size-4" />
      </button>

      {items.map((item, i) =>
        item === 'gap' ? (
          <span
            key={`gap-${i}`}
            className="px-1 text-sm text-sf-ink-hint"
            aria-hidden="true"
          >
            …
          </span>
        ) : (
          <button
            key={item}
            type="button"
            onClick={() => onChange(item)}
            aria-current={item === page ? 'page' : undefined}
            className={cn(
              'flex size-9 cursor-pointer items-center justify-center rounded-full text-sm font-semibold transition-all',
              item === page
                ? 'bg-linear-to-r from-sf-accent to-sf-accent-2 text-white shadow-[0_6px_22px_rgba(140,107,255,0.35)]'
                : 'glass text-sf-ink-muted hover:bg-sf-panel-2 hover:text-sf-ink'
            )}
          >
            {item}
          </button>
        )
      )}

      <button
        type="button"
        aria-label="Next page"
        disabled={page >= total}
        onClick={() => onChange(page + 1)}
        className="glass flex size-9 cursor-pointer items-center justify-center rounded-full text-sf-ink-muted transition-colors hover:bg-sf-panel-2 hover:text-sf-ink disabled:pointer-events-none disabled:opacity-40"
      >
        <ChevronRight className="size-4" />
      </button>
    </nav>
  )
}
Pagination.displayName = 'Pagination'
