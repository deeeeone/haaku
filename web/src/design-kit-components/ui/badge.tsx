import { Slot } from '@radix-ui/react-slot'
import { cva, type VariantProps } from 'class-variance-authority'
import type { LucideIcon } from 'lucide-react'
import type * as React from 'react'

import { cn } from '../../lib/utils'

const badgeVariants = cva(
  'inline-flex items-center justify-center rounded-full px-2.5 py-0.5 text-xs font-medium w-fit whitespace-nowrap shrink-0 [&>svg]:size-3 gap-1 [&>svg]:pointer-events-none transition-[color,box-shadow] overflow-hidden',
  {
    variants: {
      variant: {
        default:
          'bg-linear-to-r from-sf-accent to-sf-accent-2 text-white shadow-[0_4px_16px_rgba(140,107,255,0.35)]',
        secondary: 'glass text-sf-ink-muted',
        destructive:
          'bg-sf-brand-red/15 text-[#ff7a82] [a&]:hover:bg-sf-brand-red/25',
        outline: 'border border-sf-line-2 text-sf-ink',
      },
    },
    defaultVariants: {
      variant: 'default',
    },
  }
)

function Badge({
  className,
  variant,
  asChild = false,
  count,
  children,
  ...props
}: React.ComponentProps<'span'> &
  VariantProps<typeof badgeVariants> & { asChild?: boolean; count?: number }) {
  const Comp = asChild ? Slot : 'span'

  const badge = (
    <Comp
      data-slot="badge"
      className={cn(badgeVariants({ variant }), className)}
      {...props}
    >
      {children}
    </Comp>
  )

  if (count != null) {
    return (
      <span className="relative inline-flex">
        {badge}
        <span className="t-badge" data-open={count > 0 ? 'true' : 'false'}>
          {count > 0 && (
            <span className="t-badge-dot">{count > 99 ? '99+' : count}</span>
          )}
        </span>
      </span>
    )
  }

  return badge
}

export type IconBadgeColor =
  | 'green'
  | 'orange'
  | 'purple'
  | 'blue'
  | 'red'
  | 'pink'
  | 'yellow'
  | 'dark'

const iconBadgeTextStyles: Record<IconBadgeColor, string> = {
  green:
    'bg-green-900/20 border-green-700/30 text-green-400 [&>svg]:text-green-400',
  orange:
    'bg-orange-900/20 border-orange-700/30 text-orange-400 [&>svg]:text-orange-400',
  purple:
    'bg-purple-900/20 border-purple-700/30 text-purple-400 [&>svg]:text-purple-400',
  blue: 'bg-blue-900/20 border-blue-700/30 text-blue-400 [&>svg]:text-blue-400',
  red: 'bg-red-900/20 border-red-700/30 text-red-400 [&>svg]:text-red-400',
  pink: 'bg-pink-900/20 border-pink-700/30 text-pink-400 [&>svg]:text-pink-400',
  yellow:
    'bg-yellow-900/20 border-yellow-700/30 text-yellow-400 [&>svg]:text-yellow-400',
  dark: 'bg-slate-900/40 border-slate-600/40 text-slate-300 [&>svg]:text-slate-300',
}

const iconBadgeCircleStyles: Record<IconBadgeColor, string> = {
  green:
    'bg-green-900/30 border-green-500/20 text-green-400 [&>svg]:text-green-400',
  orange:
    'bg-orange-900/30 border-orange-500/20 text-orange-400 [&>svg]:text-orange-400',
  purple:
    'bg-purple-900/30 border-purple-500/20 text-purple-400 [&>svg]:text-purple-400',
  blue: 'bg-blue-900/30 border-blue-500/20 text-blue-400 [&>svg]:text-blue-400',
  red: 'bg-red-900/30 border-red-500/20 text-red-400 [&>svg]:text-red-400',
  pink: 'bg-pink-900/30 border-pink-500/20 text-pink-400 [&>svg]:text-pink-400',
  yellow:
    'bg-yellow-900/30 border-yellow-500/20 text-yellow-400 [&>svg]:text-yellow-400',
  dark: 'bg-slate-900/50 border-slate-500/20 text-slate-300 [&>svg]:text-slate-300',
}

const iconBadgeSizeStyles = {
  sm: 'p-2 [&>svg]:size-4',
  md: 'p-3 [&>svg]:size-6',
  lg: 'w-16 h-16 [&>svg]:size-8',
} as const

export interface IconBadgeProps extends React.ComponentProps<'div'> {
  icon: LucideIcon
  color?: IconBadgeColor
  /** 'badge' = pill with icon + children; 'icon' = circle with icon only */
  variant?: 'badge' | 'icon'
  /** Only for variant="icon": sm (p-2), md (p-3), lg (w-16 h-16) */
  size?: 'sm' | 'md' | 'lg'
  /** Second icon for toggle animation (t-icon-swap) */
  iconB?: LucideIcon
  /** Whether to show iconB instead of icon */
  toggled?: boolean
}

function IconBadge({
  className,
  icon: Icon,
  iconB: IconB,
  toggled = false,
  color = 'green',
  variant = 'badge',
  size = 'md',
  children,
  ...props
}: IconBadgeProps) {
  const hasSwap = !!IconB

  if (variant === 'icon') {
    return (
      <div
        data-slot="icon-badge"
        className={cn(
          'inline-flex items-center justify-center rounded-full border shrink-0',
          !hasSwap && 'transition-transform group-hover:scale-110',
          iconBadgeCircleStyles[color],
          iconBadgeSizeStyles[size],
          hasSwap && 't-icon-swap',
          className
        )}
        data-state={hasSwap ? (toggled ? 'b' : 'a') : undefined}
        {...props}
      >
        {hasSwap ? (
          <>
            <span className="t-icon" data-icon="a">
              <Icon className="shrink-0" aria-hidden />
            </span>
            <span className="t-icon" data-icon="b">
              <IconB className="shrink-0" aria-hidden />
            </span>
          </>
        ) : (
          <Icon className="shrink-0" aria-hidden />
        )}
      </div>
    )
  }
  return (
    <div
      data-slot="icon-badge"
      className={cn(
        'inline-flex items-center gap-2 px-3 py-1.5 md:px-4 md:py-2 rounded-full border font-medium text-xs md:text-sm',
        !hasSwap && 'transition-transform group-hover:scale-110',
        hasSwap && 't-icon-swap',
        iconBadgeTextStyles[color],
        iconBadgeSizeStyles[size],
        className
      )}
      data-state={hasSwap ? (toggled ? 'b' : 'a') : undefined}
      {...props}
    >
      {hasSwap ? (
        <>
          <span className="t-icon" data-icon="a">
            <Icon className="h-3 w-3 md:h-4 md:w-4 shrink-0" aria-hidden />
          </span>
          <span className="t-icon" data-icon="b">
            <IconB className="h-3 w-3 md:h-4 md:w-4 shrink-0" aria-hidden />
          </span>
        </>
      ) : (
        <Icon className="h-3 w-3 md:h-4 md:w-4 shrink-0" aria-hidden />
      )}
      {children != null && <span>{children}</span>}
    </div>
  )
}

export { Badge, badgeVariants, IconBadge }
