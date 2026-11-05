import type * as React from 'react'

import { cn } from '../../lib/utils'

export type DetailStatProps = {
  label: string
  value: React.ReactNode
  className?: string
}

export function DetailStat({ label, value, className }: DetailStatProps) {
  return (
    <div className={cn('flex flex-col gap-0.5', className)}>
      <span className="text-[11px] font-bold tracking-[0.14em] text-sf-ink-hint uppercase">
        {label}
      </span>
      <span className="text-sm font-semibold text-sf-ink">{value}</span>
    </div>
  )
}
DetailStat.displayName = 'DetailStat'
