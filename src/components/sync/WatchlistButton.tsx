import { Bookmark } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui'

export type WatchlistButtonProps = {
  isInWatchlist?: boolean
  onToggle: () => void
  loading?: boolean
  size?: 'sm' | 'md' | 'lg'
  className?: string
}

const sizeClasses = {
  sm: 'size-3.5',
  md: 'size-4',
  lg: 'size-5',
}

const buttonSize = {
  sm: 'sm' as const,
  md: 'default' as const,
  lg: 'lg' as const,
}

export function WatchlistButton({
  isInWatchlist = false,
  onToggle,
  loading = false,
  size = 'md',
  className,
}: WatchlistButtonProps) {
  return (
    <Button
      variant={isInWatchlist ? 'primary' : 'glass'}
      size={buttonSize[size]}
      onClick={onToggle}
      loading={loading}
      className={cn(className)}
      aria-label={isInWatchlist ? 'Remove from watchlist' : 'Add to watchlist'}
      title={isInWatchlist ? 'Remove from watchlist' : 'Add to watchlist'}
    >
      <Bookmark
        className={cn(
          sizeClasses[size],
          'transition-all',
          isInWatchlist && 'fill-current',
        )}
      />
      <span className="hidden sm:inline">
        {isInWatchlist ? 'Watchlist' : 'Watchlist'}
      </span>
    </Button>
  )
}

WatchlistButton.displayName = 'WatchlistButton'