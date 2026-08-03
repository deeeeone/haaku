import { useState, useRef, useEffect } from 'react'
import { Smile } from 'lucide-react'
import { cn } from '@/lib/utils'

const REACTIONS = [
  { emoji: '👍', label: 'Like', key: 'like' },
  { emoji: '❤️', label: 'Love', key: 'love' },
  { emoji: '😂', label: 'Laugh', key: 'laugh' },
  { emoji: '😮', label: 'Wow', key: 'wow' },
  { emoji: '😢', label: 'Sad', key: 'sad' },
  { emoji: '😡', label: 'Angry', key: 'angry' },
] as const

interface ReactionPickerProps {
  onReaction: (reaction: string) => void
  activeReactions?: string[]
  reactionCounts?: Record<string, number>
  className?: string
}

export function ReactionPicker({
  onReaction,
  activeReactions = [],
  reactionCounts = {},
  className,
}: ReactionPickerProps) {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [])

  return (
    <div ref={ref} className={cn('relative inline-flex items-center', className)}>
      {/* Reaction badges shown inline */}
      <div className="flex items-center gap-0.5 mr-1">
        {REACTIONS.map((r) => {
          const count = reactionCounts[r.key]
          if (!count || count < 1) return null
          return (
            <span
              key={r.key}
              className={cn(
                'inline-flex items-center gap-0.5 rounded-full px-1.5 py-0.5 text-xs',
                activeReactions.includes(r.key)
                  ? 'bg-sf-accent/20 text-sf-accent-2'
                  : 'bg-white/5 text-sf-ink-muted'
              )}
            >
              {r.emoji} <span className="tabular-nums">{count}</span>
            </span>
          )
        })}
      </div>

      {/* Picker trigger */}
      <button
        type="button"
        onClick={() => setOpen(!open)}
        className="rounded-full p-1 text-sf-ink-muted transition-colors hover:bg-white/10 hover:text-sf-ink"
        aria-label="Add reaction"
      >
        <Smile className="size-4" />
      </button>

      {/* Floating picker */}
      {open && (
        <div className="absolute bottom-full left-0 mb-2 z-50 flex gap-1 rounded-full border border-white/10 bg-sf-slate-800 p-1 shadow-lg">
          {REACTIONS.map((r) => (
            <button
              key={r.key}
              type="button"
              onClick={() => {
                onReaction(r.key)
                setOpen(false)
              }}
              className={cn(
                'rounded-full p-1.5 text-lg transition-all hover:scale-125 hover:bg-white/10',
                activeReactions.includes(r.key) && 'bg-sf-accent/20 scale-110'
              )}
              title={r.label}
            >
              {r.emoji}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}
