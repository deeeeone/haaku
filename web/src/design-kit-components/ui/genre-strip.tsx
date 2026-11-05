import { FilterPill } from './filter-pill'
import { cn } from '../../lib/utils'

export type GenreStripProps = {
  genres: Array<{ label: string; count?: number }>
  active?: string
  onSelect?: (label: string) => void
  className?: string
}

export function GenreStrip({
  genres,
  active,
  onSelect,
  className,
}: GenreStripProps) {
  return (
    <div
      className={cn(
        'scrollbar-hide flex gap-2.5 overflow-x-auto py-1',
        className
      )}
    >
      {genres.map((g) => (
        <FilterPill
          key={g.label}
          active={g.label === active}
          count={g.count}
          onClick={() => onSelect?.(g.label)}
        >
          {g.label}
        </FilterPill>
      ))}
    </div>
  )
}
GenreStrip.displayName = 'GenreStrip'
