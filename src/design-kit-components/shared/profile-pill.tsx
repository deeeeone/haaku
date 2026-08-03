import { ChevronDown } from 'lucide-react'
import * as React from 'react'

import { cn } from '../../lib/utils'

export type ProfilePillProps = React.ComponentPropsWithoutRef<'button'> & {
  name: string
  subtitle?: string
  initials?: string
}

export const ProfilePill = React.forwardRef<
  HTMLButtonElement,
  ProfilePillProps
>(({ className, name, subtitle, initials, ...rest }, ref) => {
  const fallback = name
    .split(' ')
    .map((w) => w[0])
    .slice(0, 2)
    .join('')
    .toUpperCase()

  return (
    <button
      ref={ref}
      type="button"
      className={cn(
        'group flex cursor-pointer items-center gap-2.5 rounded-full border border-sf-line bg-sf-panel py-1 pr-2.5 pl-1 transition-colors hover:bg-sf-panel-2',
        className
      )}
      {...rest}
    >
      <span className="grid size-8 place-items-center rounded-full bg-linear-to-br from-sf-accent to-sf-accent-2 text-xs font-bold text-white">
        {initials ?? fallback}
      </span>
      <span className="hidden text-left sm:block">
        <span className="block text-sm leading-tight font-semibold text-sf-ink">
          {name}
        </span>
        {subtitle && (
          <span className="block text-[10px] leading-tight text-sf-ink-hint">
            {subtitle}
          </span>
        )}
      </span>
      <ChevronDown className="size-3.5 text-sf-ink-faint transition-transform duration-200 group-data-[state=open]:rotate-180" />
    </button>
  )
})
ProfilePill.displayName = 'ProfilePill'
