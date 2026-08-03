import type * as React from 'react'

import { cn } from '../../lib/utils'

export type MoodTileProps = {
  label: string
  from: string
  to: string
  icon?: React.ReactNode
  className?: string
}

export function MoodTile({ label, from, to, icon, className }: MoodTileProps) {
  return (
    <button
      type="button"
      className={cn(
        'sf-lift group relative h-28 cursor-pointer overflow-hidden rounded-[var(--sf-radius)] p-4 text-left',
        className
      )}
      style={{ backgroundImage: `linear-gradient(135deg, ${from}, ${to})` }}
    >
      <span className="absolute -top-6 -right-6 size-24 rounded-full bg-white/15 blur-2xl transition-transform duration-500 group-hover:scale-125" />
      <span className="absolute bottom-0 left-0 h-px w-full bg-gradient-to-r from-white/40 to-transparent" />
      <span className="relative flex h-full flex-col items-start justify-between">
        {icon && <span className="text-xl leading-none">{icon}</span>}
        <span className="font-display text-sm font-bold text-white drop-shadow-[0_1px_6px_rgba(0,0,0,0.4)]">
          {label}
        </span>
      </span>
    </button>
  )
}
MoodTile.displayName = 'MoodTile'
