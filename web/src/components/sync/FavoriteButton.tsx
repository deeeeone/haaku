import { motion } from 'framer-motion'
import { Heart } from 'lucide-react'
import { Button } from '@/components/ui'
import { cn } from '@/lib/utils'

export type FavoriteButtonProps = {
  isFavorited?: boolean
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

export function FavoriteButton({
  isFavorited = false,
  onToggle,
  loading = false,
  size = 'md',
  className,
}: FavoriteButtonProps) {
  return (
    <Button
      variant={isFavorited ? 'danger' : 'glass'}
      size={buttonSize[size]}
      onClick={onToggle}
      loading={loading}
      className={cn(className)}
      aria-label={isFavorited ? 'Remove from favorites' : 'Add to favorites'}
      title={isFavorited ? 'Remove from favorites' : 'Add to favorites'}
    >
      <motion.span
        animate={{ scale: isFavorited ? [1, 1.3, 1] : 1 }}
        transition={{ duration: 0.3 }}
        key={String(isFavorited)}
      >
        <Heart
          className={cn(
            sizeClasses[size],
            'transition-colors',
            isFavorited && 'fill-current'
          )}
        />
      </motion.span>
      <span className="hidden sm:inline">
        {isFavorited ? 'Favorited' : 'Favorite'}
      </span>
    </Button>
  )
}

FavoriteButton.displayName = 'FavoriteButton'
