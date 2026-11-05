import { ArrowRight } from 'lucide-react'
import type * as React from 'react'
import { cn } from '../../lib/utils'
import { Eyebrow } from './eyebrow'

export type SectionHeadProps = {
  eyebrow?: string
  title: React.ReactNode
  accent?: 'purple' | 'cyan'
  action?: string
  onAction?: () => void
  className?: string
}

export function SectionHead({
  eyebrow,
  title,
  accent = 'purple',
  action,
  onAction,
  className,
}: SectionHeadProps) {
  return (
    <div className={cn('flex items-end justify-between gap-4 pb-3', className)}>
      <div className="min-w-0 space-y-2">
        {eyebrow && <Eyebrow accent={accent}>{eyebrow}</Eyebrow>}
        <h2 className="font-display text-2xl font-bold tracking-tight text-sf-ink md:text-3xl">
          {title}
        </h2>
      </div>
      {action && (
        <button
          type="button"
          onClick={onAction}
          className="group inline-flex shrink-0 cursor-pointer items-center gap-1.5 text-sm font-semibold text-sf-ink-muted transition-colors hover:text-sf-ink"
        >
          {action}
          <ArrowRight className="size-4 transition-transform duration-200 group-hover:translate-x-0.5" />
        </button>
      )}
    </div>
  )
}
SectionHead.displayName = 'SectionHead'
