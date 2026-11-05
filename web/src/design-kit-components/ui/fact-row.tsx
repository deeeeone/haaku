import type * as React from 'react'

import { cn } from '../../lib/utils'

export type FactRowProps = {
  label: string
  value: React.ReactNode
  className?: string
}

export function FactRow({ label, value, className }: FactRowProps) {
  return (
    <div
      className={cn(
        'flex items-baseline justify-between gap-6 border-b border-sf-line/60 py-2.5 last:border-b-0',
        className
      )}
    >
      <span className="shrink-0 text-sm text-sf-ink-muted">{label}</span>
      <span className="text-right text-sm font-semibold text-sf-ink">
        {value}
      </span>
    </div>
  )
}
FactRow.displayName = 'FactRow'
