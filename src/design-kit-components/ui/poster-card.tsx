import { motion } from 'framer-motion'
import { Play, RotateCcw } from 'lucide-react'
import * as React from 'react'
import { WatchProgress } from '../shared/watch-progress'
import { cn } from '../../lib/utils'

export type PosterCardProps = Omit<
  React.ComponentPropsWithoutRef<'div'>,
  'title'
> & {
  title: string
  meta?: string
  image?: string
  aspect?: '16:9' | '2:3'
  rating?: string
  type?: 'movie' | 'series' | 'episode'
  progress?: number
  fallback?: React.ReactNode
}

export function PosterCard({
  title,
  meta,
  image,
  aspect = '16:9',
  rating,
  type,
  progress,
  fallback,
  className,
  ...rest
}: PosterCardProps) {
  const [tilt, setTilt] = React.useState({ x: 0, y: 0 })

  const handleMove = (e: React.MouseEvent<HTMLDivElement>) => {
    const rect = e.currentTarget.getBoundingClientRect()
    const px = (e.clientX - rect.left) / rect.width
    const py = (e.clientY - rect.top) / rect.height
    setTilt({ y: (px - 0.5) * 8, x: (0.5 - py) * 8 })
  }

  const handleLeave = () => setTilt({ x: 0, y: 0 })

  return (
    <motion.div
      initial={{ opacity: 0, y: 18 }}
      whileInView={{ opacity: 1, y: 0 }}
      viewport={{ once: true, margin: '0px 0px -60px 0px' }}
      transition={{ duration: 0.5, ease: [0.2, 0.8, 0.2, 1] }}
      className="shrink-0 min-w-(--sf-card-min)"
    >
      {/* biome-ignore lint/a11y/noStaticElementInteractions: cursor tilt is decorative, no keyboard interaction needed */}
      <div
        data-rail-card
        onMouseMove={handleMove}
        onMouseLeave={handleLeave}
        className={cn('group relative w-full', className)}
        style={
          {
            '--tilt-x': `${tilt.x}deg`,
            '--tilt-y': `${tilt.y}deg`,
            transform:
              'perspective(1000px) rotateX(var(--tilt-x)) rotateY(var(--tilt-y))',
            transition: 'transform 220ms cubic-bezier(0.2,0.8,0.2,1)',
          } as React.CSSProperties
        }
        {...rest}
      >
        <div
          className={cn(
            'relative w-full overflow-hidden rounded-[var(--sf-radius)] border border-sf-line bg-sf-bg-3',
            aspect === '16:9' ? 'aspect-video' : 'aspect-[2/3]'
          )}
        >
          {image ? (
            <img
              src={image}
              alt={title}
              loading="lazy"
              className="h-full w-full object-cover transition-transform duration-500 ease-(--sf-ease) group-hover:scale-[1.06]"
            />
          ) : (
            (fallback ?? (
              <div className="grid h-full w-full place-items-center bg-linear-to-br from-sf-bg-3 to-sf-bg-2">
                <span className="font-display text-4xl font-bold text-sf-ink-faint">
                  {title.charAt(0)}
                </span>
              </div>
            ))
          )}

          <div className="pointer-events-none absolute inset-0 bg-linear-to-t from-black/70 via-black/10 to-transparent opacity-70 transition-opacity duration-300 group-hover:opacity-100" />

          <div className="absolute top-2.5 left-2.5 flex items-center gap-1.5 opacity-0 transition-all duration-300 group-hover:opacity-100">
            {rating && (
              <span className="inline-flex items-center gap-1 rounded-full bg-black/60 px-2 py-0.5 text-[11px] font-bold text-white backdrop-blur-sm">
                <StarMark />
                {rating}
              </span>
            )}
            {type && (
              <span className="inline-flex items-center rounded-full bg-black/60 px-2 py-0.5 text-[9px] font-bold tracking-[0.14em] text-white backdrop-blur-sm">
                {type.toUpperCase()}
              </span>
            )}
          </div>

          <div className="absolute inset-0 grid place-items-center">
            <span className="flex size-12 scale-75 items-center justify-center rounded-full bg-white text-black opacity-0 shadow-[0_10px_30px_rgba(0,0,0,0.5)] transition-all duration-300 group-hover:scale-100 group-hover:opacity-100">
              <Play className="size-5 translate-x-0.5 fill-current" />
            </span>
          </div>

          {typeof progress === 'number' && (
            <div className="absolute inset-x-0 bottom-0">
              <WatchProgress value={progress} />
            </div>
          )}

          {typeof progress === 'number' && progress > 0 && progress < 100 && (
            <span className="absolute bottom-3 right-2.5 inline-flex items-center gap-1 rounded-full bg-sf-accent/90 px-2 py-0.5 text-[10px] font-bold text-white opacity-0 transition-opacity duration-300 group-hover:opacity-100">
              <RotateCcw className="size-2.5" />
              Resume
            </span>
          )}
        </div>

        <div className="mt-2.5 px-0.5">
          <p className="truncate text-sm font-semibold text-sf-ink">{title}</p>
          {meta && <p className="truncate text-xs text-sf-ink-faint">{meta}</p>}
        </div>
      </div>
    </motion.div>
  )
}
PosterCard.displayName = 'PosterCard'

function StarMark() {
  return (
    <svg
      viewBox="0 0 24 24"
      className="size-2.5 fill-sf-accent-2 text-sf-accent-2"
      aria-hidden="true"
    >
      <path d="M12 2l2.9 6.2 6.6.8-4.9 4.6 1.3 6.6L12 17l-5.9 3.2 1.3-6.6L2.5 9l6.6-.8z" />
    </svg>
  )
}
