import type { LucideIcon } from 'lucide-react'
import type * as React from 'react'
import { cn } from '../../lib/utils'
import { IconBadge, type IconBadgeColor } from './badge'

export type MiniInfoCardProps = {
  icon: LucideIcon
  label: string
  value: React.ReactNode
  badgeColor?: IconBadgeColor
  className?: string
}

export function MiniInfoCard({
  icon,
  label,
  value,
  badgeColor = 'purple',
  className,
}: MiniInfoCardProps) {
  return (
    <div
      className={cn(
        'glass sf-lift flex items-center gap-3.5 rounded-[var(--sf-radius)] p-4',
        className
      )}
    >
      <IconBadge icon={icon} color={badgeColor} variant="icon" size="sm" />
      <div className="min-w-0">
        <p className="text-[11px] font-bold tracking-[0.14em] text-sf-ink-hint uppercase">
          {label}
        </p>
        <p className="truncate text-sm font-semibold text-sf-ink">{value}</p>
      </div>
    </div>
  )
}
MiniInfoCard.displayName = 'MiniInfoCard'
