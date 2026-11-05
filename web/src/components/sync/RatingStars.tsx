import { AnimatePresence, motion } from 'framer-motion'
import { Star } from 'lucide-react'
import { useCallback, useState } from 'react'
import { cn } from '@/lib/utils'

export type RatingStarsProps = {
  value?: number
  onChange?: (value: number) => void
  readOnly?: boolean
  max?: number
  size?: 'sm' | 'md' | 'lg'
  className?: string
}

const sizeMap = {
  sm: 'size-4',
  md: 'size-5',
  lg: 'size-7',
}

export function RatingStars({
  value = 0,
  onChange,
  readOnly = false,
  max = 10,
  size = 'md',
  className,
}: RatingStarsProps) {
  const [hoverValue, setHoverValue] = useState<number>(0)
  const [animatingStar, setAnimatingStar] = useState<number | null>(null)

  const handleClick = useCallback(
    (star: number) => {
      if (readOnly) return
      const newVal = star === value ? 0 : star
      setAnimatingStar(star)
      setTimeout(() => setAnimatingStar(null), 400)
      onChange?.(newVal)
    },
    [readOnly, value, onChange]
  )

  const activeValue = hoverValue || value

  return (
    <div
      className={cn('inline-flex items-center gap-0.5', className)}
      onMouseLeave={() => setHoverValue(0)}
      role="radiogroup"
      aria-label={`Rating: ${value} out of ${max}`}
    >
      {Array.from({ length: max }, (_, i) => {
        const starNum = i + 1
        const isActive = starNum <= activeValue
        const isFull = starNum <= value
        const isAnimating = animatingStar === starNum
        return (
          <button
            key={starNum}
            type="button"
            disabled={readOnly}
            className={cn(
              'relative cursor-pointer bg-transparent p-0.5 transition-colors',
              readOnly && 'cursor-default'
            )}
            onClick={() => handleClick(starNum)}
            onMouseEnter={() => !readOnly && setHoverValue(starNum)}
            aria-label={`Rate ${starNum} out of ${max}`}
            role="radio"
            aria-checked={isFull}
          >
            <Star
              className={cn(
                sizeMap[size],
                'transition-all duration-150',
                isActive
                  ? 'fill-sf-accent-2 text-sf-accent-2'
                  : 'fill-none text-sf-ink-faint'
              )}
              strokeWidth={1.5}
            />
            <AnimatePresence>
              {isAnimating && (
                <motion.div
                  className="absolute inset-0 flex items-center justify-center"
                  initial={{ scale: 1 }}
                  animate={{ scale: [1, 1.4, 1] }}
                  exit={{ scale: 1 }}
                  transition={{ duration: 0.35, ease: 'easeOut' }}
                >
                  <Star
                    className={cn(
                      sizeMap[size],
                      'fill-sf-accent-2 text-sf-accent-2'
                    )}
                    strokeWidth={1.5}
                  />
                </motion.div>
              )}
            </AnimatePresence>
          </button>
        )
      })}
      {!readOnly && (
        <span className="ml-1.5 text-xs font-semibold text-sf-ink-muted tabular-nums min-w-[2ch]">
          {activeValue > 0 ? activeValue : '—'}
        </span>
      )}
    </div>
  )
}

RatingStars.displayName = 'RatingStars'
