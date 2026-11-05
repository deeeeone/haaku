import type * as React from 'react'

import { cn } from '../../lib/utils'

export type MobileNavItem = {
  label: string
  icon: React.ReactNode
  active?: boolean
}

export type MobileNavProps = {
  items: MobileNavItem[]
  center?: React.ReactNode
  /** Hide on md+ screens (matches the source app). Set false to preview on desktop. */
  responsive?: boolean
  className?: string
}

export function MobileNav({
  items,
  center,
  responsive = true,
  className,
}: MobileNavProps) {
  return (
    <nav
      className={cn(
        'fixed inset-x-0 bottom-0 z-40 border-t border-sf-line bg-sf-bg-2/85 backdrop-blur-xl',
        responsive && 'md:hidden',
        'pb-[env(safe-area-inset-bottom)]',
        className
      )}
      aria-label="Mobile navigation"
    >
      <div className="relative grid grid-cols-5 items-center">
        {center && (
          <div className="absolute left-1/2 top-0 -translate-x-1/2 -translate-y-1/2">
            {center}
          </div>
        )}
        {items.map((item) => (
          <button
            key={item.label}
            type="button"
            className={cn(
              'flex cursor-pointer flex-col items-center gap-1 py-3 text-[10px] font-semibold transition-colors',
              item.active
                ? 'text-sf-accent-2'
                : 'text-sf-ink-hint hover:text-sf-ink-muted'
            )}
          >
            {item.icon}
            {item.label}
          </button>
        ))}
      </div>
    </nav>
  )
}
MobileNav.displayName = 'MobileNav'
