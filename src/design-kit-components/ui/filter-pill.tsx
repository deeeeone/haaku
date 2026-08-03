import { cva, type VariantProps } from 'class-variance-authority'
import type * as React from 'react'

import { cn } from '../../lib/utils'

const filterPillVariants = cva(
  [
    'inline-flex h-9 shrink-0 cursor-pointer items-center gap-1.5 rounded-full px-4 text-sm font-semibold',
    'transition-all duration-200 ease-(--sf-ease)',
  ].join(' '),
  {
    variants: {
      active: {
        true: [
          'bg-linear-to-r from-sf-accent to-sf-accent-2 text-white',
          'shadow-[0_6px_22px_rgba(140,107,255,0.35)]',
        ],
        false: 'glass text-sf-ink-muted hover:bg-sf-panel-2 hover:text-sf-ink',
      },
    },
    defaultVariants: {
      active: false,
    },
  }
)

type FilterPillVariants = VariantProps<typeof filterPillVariants>

export type FilterPillProps = React.ComponentPropsWithoutRef<'button'> &
  FilterPillVariants & {
    count?: number
  }

export function FilterPill({
  className,
  active,
  count,
  children,
  ...rest
}: FilterPillProps) {
  return (
    <button
      type="button"
      className={cn(filterPillVariants({ active, className }))}
      {...rest}
    >
      {children}
      {typeof count === 'number' && (
        <span
          className={cn(
            'rounded-full px-1.5 text-[11px] font-bold',
            active
              ? 'bg-white/25 text-white'
              : 'bg-sf-panel-2 text-sf-ink-faint'
          )}
        >
          {count}
        </span>
      )}
    </button>
  )
}
FilterPill.displayName = 'FilterPill'

export { filterPillVariants }
