import type * as React from 'react'

import { cn } from '../../lib/utils'

export type EyebrowProps = React.ComponentPropsWithoutRef<'span'> & {
  accent?: 'purple' | 'cyan'
}

export function Eyebrow({
  className,
  accent = 'purple',
  ...rest
}: EyebrowProps) {
  return (
    <span
      className={cn(
        'inline-flex items-center gap-2.5 text-xs font-bold uppercase tracking-[0.2em]',
        accent === 'cyan' ? 'text-sf-accent-2' : 'text-[#b9a6ff]',
        className
      )}
      {...rest}
    />
  )
}
Eyebrow.displayName = 'Eyebrow'
