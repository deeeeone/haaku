import { Clapperboard } from 'lucide-react'
import type * as React from 'react'

import { cn } from '../../lib/utils'

export type EmptyStateProps = {
  title: string
  description?: string
  action?: React.ReactNode
  className?: string
}

export function EmptyState({
  title,
  description,
  action,
  className,
}: EmptyStateProps) {
  return (
    <div
      className={cn(
        'glass flex flex-col items-center gap-4 rounded-[var(--sf-radius-lg)] px-6 py-14 text-center',
        className
      )}
    >
      <span className="grid size-14 place-items-center rounded-full bg-sf-accent/12 text-sf-accent-2">
        <Clapperboard className="size-6" />
      </span>
      <div className="space-y-1.5">
        <h3 className="font-display text-xl font-bold text-sf-ink">{title}</h3>
        {description && (
          <p className="mx-auto max-w-sm text-sm text-sf-ink-muted">
            {description}
          </p>
        )}
      </div>
      {action}
    </div>
  )
}
EmptyState.displayName = 'EmptyState'
