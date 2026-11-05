import type * as React from 'react'

import { cn } from '../../lib/utils'

export type ShimmerTextProps = React.ComponentProps<'span'>

export function ShimmerText({
  className,
  children,
  ...props
}: ShimmerTextProps) {
  return (
    <span
      className={cn('t-shimmer', className)}
      data-text={typeof children === 'string' ? children : ''}
      {...props}
    >
      {children}
    </span>
  )
}
