import { Check, Loader2, Plus } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import type { TraktList } from '@/api/types/lists'
import { Button, Input } from '@/components/ui'
import { cn } from '@/lib/utils'

export interface AddToListDropdownProps {
  lists: TraktList[]
  loading?: boolean
  selectedIds: number[]
  onToggle: (listId: number) => void
  onCreateNew: (name: string) => void
  creating?: boolean
  className?: string
}

export function AddToListDropdown({
  lists,
  loading,
  selectedIds,
  onToggle,
  onCreateNew,
  creating,
  className,
}: AddToListDropdownProps) {
  const [open, setOpen] = useState(false)
  const [newName, setNewName] = useState('')
  const ref = useRef<HTMLDivElement>(null)

  // Close on outside click
  useEffect(() => {
    if (!open) return
    const handler = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [open])

  return (
    <div ref={ref} className={cn('relative inline-block', className)}>
      <Button
        variant="ghost"
        size="sm"
        onClick={() => setOpen(!open)}
        className="flex items-center gap-1.5"
      >
        <Plus className="size-3.5" />
        Add to List
      </Button>

      {open && (
        <div className="absolute top-full left-0 mt-2 z-50 w-64 glass rounded-2xl border border-sf-line shadow-lg p-2">
          {/* Create new list */}
          <div className="flex gap-1.5 mb-2">
            <Input
              size="sm"
              placeholder="New list name..."
              value={newName}
              onChange={(e) => setNewName(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && newName.trim()) {
                  onCreateNew(newName.trim())
                  setNewName('')
                }
              }}
            />
            <Button
              size="icon"
              variant="ghost"
              onClick={() => {
                if (newName.trim()) {
                  onCreateNew(newName.trim())
                  setNewName('')
                }
              }}
              disabled={!newName.trim() || creating}
            >
              {creating ? (
                <Loader2 className="size-3.5 animate-spin" />
              ) : (
                <Plus className="size-3.5" />
              )}
            </Button>
          </div>

          {/* Divider */}
          <div className="h-px bg-sf-line my-2" />

          {/* List checkboxes */}
          {loading ? (
            <div className="py-4 text-center text-sm text-sf-ink-muted">
              Loading...
            </div>
          ) : lists.length === 0 ? (
            <div className="py-4 text-center text-sm text-sf-ink-muted">
              No lists yet
            </div>
          ) : (
            <div className="max-h-56 overflow-y-auto space-y-0.5">
              {lists.map((list) => {
                const isSelected = selectedIds.includes(list.ids.trakt)
                return (
                  <button
                    key={list.ids.trakt}
                    type="button"
                    onClick={() => onToggle(list.ids.trakt)}
                    className={cn(
                      'flex items-center gap-2.5 w-full rounded-xl px-3 py-2 text-sm text-left transition-colors',
                      isSelected
                        ? 'bg-sf-accent/15 text-sf-ink font-medium'
                        : 'text-sf-ink-muted hover:bg-sf-panel-2 hover:text-sf-ink'
                    )}
                  >
                    <span
                      className={cn(
                        'flex size-4 items-center justify-center rounded border shrink-0 transition-colors',
                        isSelected
                          ? 'bg-sf-accent border-sf-accent text-white'
                          : 'border-sf-line'
                      )}
                    >
                      {isSelected && <Check className="size-3" />}
                    </span>
                    <span className="truncate flex-1">{list.name}</span>
                    <span className="text-xs text-sf-ink-faint">
                      {list.item_count}
                    </span>
                  </button>
                )
              })}
            </div>
          )}
        </div>
      )}
    </div>
  )
}
