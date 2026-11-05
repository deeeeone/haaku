import type * as React from 'react'
import { cn } from '../../lib/utils'
import { SectionHead } from './section-head'

export type InfoPanelProps = React.ComponentPropsWithoutRef<'div'> & {
  title?: React.ReactNode
  eyebrow?: string
  action?: string
  onAction?: () => void
}

export function InfoPanel({
  title,
  eyebrow,
  action,
  onAction,
  className,
  children,
}: InfoPanelProps) {
  return (
    <div
      className={cn(
        'glass rounded-[var(--sf-radius-lg)] p-5 md:p-7',
        className
      )}
    >
      {(title || eyebrow) && (
        <SectionHead
          eyebrow={eyebrow}
          title={title}
          action={action}
          onAction={onAction}
          className="pb-4"
        />
      )}
      {children}
    </div>
  )
}
InfoPanel.displayName = 'InfoPanel'
