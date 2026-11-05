import { cn } from '../../lib/utils'

export type WatchProgressProps = {
  value: number
  className?: string
  barClassName?: string
}

export function WatchProgress({
  value,
  className,
  barClassName,
}: WatchProgressProps) {
  const clamped = Math.min(100, Math.max(0, value))
  return (
    <div
      role="progressbar"
      aria-valuenow={Math.round(clamped)}
      aria-valuemin={0}
      aria-valuemax={100}
      className={cn(
        'h-1 w-full overflow-hidden rounded-full bg-black/45',
        className
      )}
    >
      <div
        className={cn(
          'h-full rounded-full bg-linear-to-r from-sf-accent to-sf-accent-2',
          barClassName
        )}
        style={{ width: `${clamped}%` }}
      />
    </div>
  )
}
WatchProgress.displayName = 'WatchProgress'
