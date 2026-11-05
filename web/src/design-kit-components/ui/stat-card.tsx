import type { LucideIcon } from 'lucide-react'
import type * as React from 'react'
import { IconBadge, type IconBadgeColor } from './badge'
import { cn } from '../../lib/utils'

export type StatCardProps = {
  label: string
  value: React.ReactNode
  delta?: string
  positive?: boolean
  icon?: LucideIcon
  badgeColor?: IconBadgeColor
  className?: string
}

export function StatCard({
  label,
  value,
  delta,
  positive,
  icon,
  badgeColor = 'purple',
  className,
}: StatCardProps) {
  return (
    <div
      className={cn(
        'glass sf-lift rounded-[var(--sf-radius)] p-5 md:p-6',
        className
      )}
    >
      <div className="flex items-start justify-between gap-3 md:gap-4">
        <p className="text-xs font-semibold tracking-[0.14em] text-sf-ink-muted uppercase">
          {label}
        </p>
        {icon && (
          <IconBadge
            icon={icon}
            color={badgeColor}
            variant="icon"
            size="sm"
            className="-mt-2 -mr-2"
          />
        )}
      </div>
      <p className="mt-3 font-display text-3xl font-bold tracking-tight text-sf-ink md:text-4xl">
        {value}
      </p>
      {delta && (
        <p
          className={cn(
            'mt-1 text-sm font-medium',
            positive ? 'text-sf-accent-4' : 'text-sf-ink-faint'
          )}
        >
          {delta}
        </p>
      )}
    </div>
  )
}
StatCard.displayName = 'StatCard'
