import { cva, type VariantProps } from 'class-variance-authority'
import type * as React from 'react'

import { cn } from '../../lib/utils'

const chipVariants = cva(
  'inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-[11px] font-bold tracking-[0.08em] uppercase',
  {
    variants: {
      variant: {
        accent: 'bg-sf-accent/15 text-[#b9a6ff]',
        cyan: 'bg-sf-accent-2/10 text-sf-accent-2',
        glass: 'glass text-sf-ink-muted',
        red: 'bg-sf-brand-red/15 text-[#ff7a82]',
        green: 'bg-sf-accent-4/10 text-sf-accent-4',
      },
    },
    defaultVariants: {
      variant: 'glass',
    },
  }
)

type ChipVariants = VariantProps<typeof chipVariants>

export type ChipProps = React.ComponentPropsWithoutRef<'span'> &
  ChipVariants & {
    dot?: boolean
    live?: boolean
  }

function Chip({ className, variant, dot, live, children, ...rest }: ChipProps) {
  return (
    <span className={cn(chipVariants({ variant, className }))} {...rest}>
      {live && <span className="sf-dot-live" aria-hidden="true" />}
      {dot && (
        <span
          className="size-1.5 rounded-full bg-sf-accent-2"
          aria-hidden="true"
        />
      )}
      {children}
    </span>
  )
}
Chip.displayName = 'Chip'

export { Chip, chipVariants }
