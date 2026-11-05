import { cn } from '@/lib/utils'

interface ProgressBarProps {
  value: number // 0-100
  className?: string
  size?: 'sm' | 'md' | 'lg'
  showLabel?: boolean
}

export function ProgressBar({
  value,
  className,
  size = 'md',
  showLabel = true,
}: ProgressBarProps) {
  const clamped = Math.min(100, Math.max(0, value))

  const sizeClasses = {
    sm: 'h-1',
    md: 'h-2',
    lg: 'h-3',
  }

  return (
    <div className={cn('flex items-center gap-2', className)}>
      <div
        className={cn(
          'relative w-full overflow-hidden rounded-full bg-white/10',
          sizeClasses[size]
        )}
      >
        <div
          className={cn(
            'h-full rounded-full bg-sf-accent-2 transition-all duration-500',
            sizeClasses[size]
          )}
          style={{ width: `${clamped}%` }}
        />
      </div>
      {showLabel && (
        <span className="text-xs font-medium text-sf-ink-muted tabular-nums">
          {Math.round(clamped)}%
        </span>
      )}
    </div>
  )
}
