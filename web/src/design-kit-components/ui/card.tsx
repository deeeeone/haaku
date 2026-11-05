import { motion } from 'framer-motion'
import type * as React from 'react'
import { useEffect, useRef } from 'react'

import { cn } from '../../lib/utils'

type CardColor =
  | 'green'
  | 'orange'
  | 'purple'
  | 'blue'
  | 'red'
  | 'pink'
  | 'yellow'
  | 'dark'

const colorStyles: Record<CardColor, string> = {
  green: 'rgba(20, 184, 166, 0.17)', // teal
  orange: 'rgba(245, 158, 11, 0.17)', // amber
  purple: 'rgba(139, 92, 246, 0.17)', // violet
  blue: 'rgba(14, 165, 233, 0.17)', // sky
  red: 'rgba(244, 63, 94, 0.17)', // rose
  pink: 'rgba(217, 70, 239, 0.17)', // fuchsia
  yellow: 'rgba(234, 179, 8, 0.17)', // yellow
  dark: 'rgba(30, 27, 40, 0.22)', // cool dark
}

/** Spotlight center color per card color (subtle glow); grey when no color. */
const spotlightCenterColors: Record<CardColor, string> = {
  green: 'rgba(20, 184, 166, 0.08)',
  orange: 'rgba(245, 158, 11, 0.08)',
  purple: 'rgba(139, 92, 246, 0.08)',
  blue: 'rgba(14, 165, 233, 0.08)',
  red: 'rgba(244, 63, 94, 0.08)',
  pink: 'rgba(217, 70, 239, 0.08)',
  yellow: 'rgba(234, 179, 8, 0.08)',
  dark: 'rgba(30, 27, 40, 0.12)',
}

const SPOTLIGHT_CENTER_GREY = 'rgba(255,255,255,0.06)'

function getSpotlightGradient(centerColor: string) {
  return `radial-gradient(600px circle at var(--mouse-x, 50%) var(--mouse-y, 50%), ${centerColor}, transparent 40%)`
}

interface CardProps extends React.ComponentProps<'div'> {
  color?: CardColor
  /** When true, card scales up slightly on hover (center origin). Default false. */
  hoverScale?: boolean
  /** When true, the card does not use the group class (so group-hover effects in children won't trigger from this card). Use for wrapper cards e.g. in dialogs. Default false. */
  disableGroup?: boolean
  /** When true, shows a mouse-following spotlight gradient on hover (grey or card color). Default true. */
  spotlight?: boolean
  /** When true, blurs the card and its contents. Default false. */
  blur?: boolean
  /** When true, applies t-resize transition for width/height changes. Default false. */
  resize?: boolean
}

function Card({
  className,
  color,
  children,
  style: styleProp,
  hoverScale = false,
  disableGroup = false,
  spotlight = true,
  blur = false,
  resize = false,
  ...props
}: CardProps) {
  const cardRef = useRef<HTMLDivElement>(null)
  const hasColor = color != null
  const hasSpotlight = spotlight
  const c = colorStyles[color as CardColor]
  const spotlightGradient = getSpotlightGradient(
    hasColor ? spotlightCenterColors[color as CardColor] : SPOTLIGHT_CENTER_GREY
  )
  const gradient =
    hasColor &&
    `radial-gradient(ellipse 100% 140% at 0% 15%, ${c} 0%, ${c} 20%, transparent 55%), radial-gradient(ellipse 140% 100% at 100% 85%, ${c} 0%, ${c} 20%, transparent 55%)`

  useEffect(() => {
    const needsMouse = hasSpotlight || hasColor
    if (!needsMouse || !cardRef.current) return
    const el = cardRef.current
    const updateFromEvent = (e: MouseEvent) => {
      const card = el.getBoundingClientRect()
      const x = e.clientX - card.left
      const y = e.clientY - card.top
      if (hasSpotlight) {
        el.style.setProperty('--mouse-x', `${x}px`)
        el.style.setProperty('--mouse-y', `${y}px`)
      }
      if (hasColor) {
        const xNorm = card.width > 0 ? x / card.width : 0.5
        const rotation = (xNorm - 0.5) * 16
        el.style.setProperty('--corner-gradient-rotate', `${rotation}deg`)
      }
    }
    const onLeave = () => {
      if (hasColor) {
        el.style.setProperty('--corner-gradient-rotate', '0deg')
      }
    }
    el.addEventListener('mouseenter', updateFromEvent)
    el.addEventListener('mousemove', updateFromEvent)
    el.addEventListener('mouseleave', onLeave)
    return () => {
      el.removeEventListener('mouseenter', updateFromEvent)
      el.removeEventListener('mousemove', updateFromEvent)
      el.removeEventListener('mouseleave', onLeave)
    }
  }, [hasSpotlight, hasColor])

  const {
    onDrag,
    onDragStart,
    onDragEnd,
    onAnimationStart,
    onAnimationEnd,
    onAnimationIteration,
    ...restProps
  } = props
  return (
    <motion.div
      ref={cardRef}
      data-slot="card"
      className={cn(
        !disableGroup && 'group',
        'text-sf-ink flex flex-col gap-6 rounded-[var(--sf-radius-lg)] border border-sf-line py-6 shadow-sm',
        !hasColor && 'bg-sf-panel',
        (hasColor || hasSpotlight) && 'relative overflow-hidden',
        resize && 't-resize',
        className
      )}
      style={{
        ...(hasColor ? { backgroundColor: 'var(--sf-panel-2)' } : {}),
        ...styleProp,
      }}
      initial={false}
      whileHover={
        hoverScale
          ? {
              scale: 1.04,
              transition: { duration: 0.2, ease: [0.25, 0.46, 0.45, 0.94] },
            }
          : undefined
      }
      {...restProps}
    >
      {hasSpotlight && (
        <div
          className="pointer-events-none absolute inset-0 rounded-[var(--sf-radius-lg)] opacity-0 transition-opacity duration-300 ease-out group-hover:opacity-100"
          style={{ background: spotlightGradient }}
          aria-hidden
        />
      )}
      {hasColor && (
        <div
          className="pointer-events-none absolute left-1/2 top-1/2 w-[200%] h-[200%] opacity-50 transition-[opacity,transform] duration-300 ease-out group-hover:opacity-100"
          style={{
            backgroundImage: gradient || undefined,
            transform:
              'translate(-50%, -50%) rotate(var(--corner-gradient-rotate, 0deg))',
            transformOrigin: 'center center',
          }}
          aria-hidden
        />
      )}
      <div
        className={cn(
          'relative z-10 flex flex-col h-full transition-[filter] duration-200',
          blur && 'blur-md pointer-events-none select-none'
        )}
      >
        {children}
      </div>
    </motion.div>
  )
}

function CardHeader({ className, ...props }: React.ComponentProps<'div'>) {
  return (
    <div
      data-slot="card-header"
      className={cn(
        '@container/card-header grid auto-rows-min grid-rows-[auto_auto] items-start gap-2 px-6 has-data-[slot=card-action]:grid-cols-[1fr_auto] [.border-b]:pb-6',
        className
      )}
      {...props}
    />
  )
}

function CardTitle({ className, ...props }: React.ComponentProps<'div'>) {
  return (
    <div
      data-slot="card-title"
      className={cn('leading-none font-semibold', className)}
      {...props}
    />
  )
}

function CardDescription({ className, ...props }: React.ComponentProps<'div'>) {
  return (
    <div
      data-slot="card-description"
      className={cn('text-sf-ink-muted text-sm', className)}
      {...props}
    />
  )
}

function CardAction({ className, ...props }: React.ComponentProps<'div'>) {
  return (
    <div
      data-slot="card-action"
      className={cn(
        'col-start-2 row-span-2 row-start-1 self-start justify-self-end',
        className
      )}
      {...props}
    />
  )
}

function CardContent({ className, ...props }: React.ComponentProps<'div'>) {
  return (
    <div
      data-slot="card-content"
      className={cn('px-6', className)}
      {...props}
    />
  )
}

function CardFooter({ className, ...props }: React.ComponentProps<'div'>) {
  return (
    <div
      data-slot="card-footer"
      className={cn('flex items-center px-6 [.border-t]:pt-6', className)}
      {...props}
    />
  )
}

export {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
}
