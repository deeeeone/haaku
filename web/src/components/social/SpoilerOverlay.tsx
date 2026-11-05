import { EyeOff } from 'lucide-react'
import { Button } from '@/components/ui'
import { cn } from '@/lib/utils'

interface SpoilerOverlayProps {
  hidden: boolean
  onToggle: () => void
  className?: string
}

export function SpoilerOverlay({ hidden, onToggle, className }: SpoilerOverlayProps) {
  if (!hidden) return null

  return (
    <div
      className={cn(
        'absolute inset-0 z-10 flex items-center justify-center rounded-[--sf-radius] bg-black/80 backdrop-blur-sm',
        className
      )}
    >
      <Button variant="ghost" size="sm" onClick={onToggle}>
        <EyeOff className="mr-1.5 size-4" />
        Show Spoiler
      </Button>
    </div>
  )
}
