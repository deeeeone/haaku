import type * as React from 'react'

import { cn } from '../../lib/utils'

export type GlassPillProps = React.ComponentPropsWithoutRef<'div'>

export function GlassPill({ className, ...rest }: GlassPillProps) {
  return (
    <div
      className={cn(
        'glass inline-flex items-center gap-2 rounded-full px-4 py-2',
        className
      )}
      {...rest}
    />
  )
}
GlassPill.displayName = 'GlassPill'
