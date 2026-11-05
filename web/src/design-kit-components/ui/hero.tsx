import { motion } from 'framer-motion'
import type * as React from 'react'
import { Eyebrow } from './eyebrow'
import { cn } from '../../lib/utils'

export type HeroProps = {
  eyebrow?: string
  eyebrowAccent?: 'purple' | 'cyan'
  title: React.ReactNode
  subtitle?: React.ReactNode
  image?: string
  chips?: React.ReactNode
  actions?: React.ReactNode
  className?: string
  contentClassName?: string
}

export function Hero({
  eyebrow,
  eyebrowAccent = 'purple',
  title,
  subtitle,
  image,
  chips,
  actions,
  className,
  contentClassName,
}: HeroProps) {
  return (
    <section
      className={cn(
        'relative overflow-hidden rounded-[var(--sf-radius-xl)] border border-sf-line',
        className
      )}
    >
      {image && (
        <img
          src={image}
          alt=""
          aria-hidden="true"
          className="absolute inset-0 h-full w-full object-cover"
        />
      )}
      <div
        className={cn(
          'absolute inset-0',
          image
            ? 'bg-linear-to-t from-black via-black/55 to-black/20'
            : 'grid-bg bg-linear-to-br from-sf-bg-3 via-sf-bg-2 to-sf-bg'
        )}
      />
      <div className="vignette absolute inset-0" />

      <div
        className={cn(
          'relative flex min-h-[min(62dvh,540px)] flex-col justify-end p-(--sf-edge) pb-10',
          contentClassName
        )}
      >
        <motion.div
          initial={{ opacity: 0, y: 24 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.7, ease: [0.2, 0.8, 0.2, 1] }}
          className="max-w-3xl space-y-5"
        >
          {eyebrow && <Eyebrow accent={eyebrowAccent}>{eyebrow}</Eyebrow>}

          <h1 className="title-shadow shine font-display text-[clamp(2.6rem,7vw,5.2rem)] leading-[0.98] font-bold tracking-[-0.05em]">
            {title}
          </h1>

          {subtitle && (
            <p className="max-w-xl text-balance text-base text-sf-ink-muted md:text-lg">
              {subtitle}
            </p>
          )}

          {chips && (
            <div className="flex flex-wrap items-center gap-2">{chips}</div>
          )}

          {actions && (
            <div className="flex flex-wrap items-center gap-3 pt-1">
              {actions}
            </div>
          )}
        </motion.div>
      </div>
    </section>
  )
}
Hero.displayName = 'Hero'
