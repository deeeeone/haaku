import { useState } from 'react'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/ui'
import { Button } from '@/components/ui'
import { Input } from '@/components/ui'
import { Label } from '@/components/ui'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui'
import { FilterPill } from '@/components/ui'
import { Badge } from '@/components/ui'
import { cn } from '@/lib/utils'
import type { SmartListFilter } from '@/api/types/lists'

const MOVIE_GENRES = [
  'Action',
  'Adventure',
  'Animation',
  'Anime',
  'Comedy',
  'Crime',
  'Documentary',
  'Drama',
  'Family',
  'Fantasy',
  'History',
  'Horror',
  'Music',
  'Mystery',
  'Romance',
  'Science Fiction',
  'Thriller',
  'War',
  'Western',
]

const SHOW_GENRES = [
  'Action',
  'Adventure',
  'Animation',
  'Anime',
  'Comedy',
  'Crime',
  'Documentary',
  'Drama',
  'Family',
  'Fantasy',
  'Food',
  'Game Show',
  'History',
  'Home and Garden',
  'Horror',
  'Mini-Series',
  'Music',
  'Mystery',
  'News',
  'Reality',
  'Romance',
  'Science Fiction',
  'Soap',
  'Special Interest',
  'Sport',
  'Suspense',
  'Talk Show',
  'Thriller',
  'Travel',
  'War',
  'Western',
]

const STATUS_OPTIONS = [
  { value: '', label: 'Any' },
  { value: 'returning', label: 'Returning Series' },
  { value: 'ended', label: 'Ended' },
  { value: 'canceled', label: 'Canceled' },
]

export interface SmartListEditorProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  initialName?: string
  initialFilter?: SmartListFilter
  onSave: (name: string, filter: SmartListFilter) => void
  onPreview?: (filter: SmartListFilter) => void
  previewCount?: number
}

export function SmartListEditor({
  open,
  onOpenChange,
  initialName = '',
  initialFilter,
  onSave,
  onPreview,
  previewCount,
}: SmartListEditorProps) {
  const [name, setName] = useState(initialName)
  const [filter, setFilter] = useState<SmartListFilter>(
    initialFilter ?? { type: 'shows' }
  )

  const updateFilter = (patch: Partial<SmartListFilter>) => {
    setFilter((prev) => ({ ...prev, ...patch }))
  }

  const toggleGenre = (genre: string) => {
    const genres = filter.genres ?? []
    const next = genres.includes(genre)
      ? genres.filter((g) => g !== genre)
      : [...genres, genre]
    setFilter((prev) => ({ ...prev, genres: next }))
  }

  const genres = filter.type === 'movies' ? MOVIE_GENRES : SHOW_GENRES
  const hasFilters =
    (filter.genres && filter.genres.length > 0) ||
    filter.year_min != null ||
    filter.year_max != null ||
    filter.rating_min != null ||
    filter.votes_min != null ||
    filter.status != null ||
    filter.watched ||
    filter.collected

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-xl max-h-[85vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>
            {initialName ? 'Edit Smart List' : 'Create Smart List'}
          </DialogTitle>
          <DialogDescription>
            Build a dynamic filter that auto-populates your list.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-5 mt-2">
          {/* Name */}
          <div>
            <Label className="mb-1.5 block text-xs font-semibold text-sf-ink-muted uppercase tracking-wider">
              List Name
            </Label>
            <Input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="My Smart List"
              className="w-full"
            />
          </div>

          {/* Type selector */}
          <div>
            <Label className="mb-1.5 block text-xs font-semibold text-sf-ink-muted uppercase tracking-wider">
              Media Type
            </Label>
            <div className="flex gap-2">
              {(['shows', 'movies'] as const).map((t) => (
                <FilterPill
                  key={t}
                  active={filter.type === t}
                  onClick={() =>
                    setFilter({ type: t, genres: [], status: undefined })
                  }
                >
                  {t === 'shows' ? 'TV Shows' : 'Movies'}
                </FilterPill>
              ))}
            </div>
          </div>

          {/* Genres */}
          <div>
            <Label className="mb-1.5 block text-xs font-semibold text-sf-ink-muted uppercase tracking-wider">
              Genres
            </Label>
            <div className="flex flex-wrap gap-1.5">
              {genres.map((g) => (
                <button
                  key={g}
                  type="button"
                  onClick={() => toggleGenre(g)}
                  className={cn(
                    'rounded-full px-3 py-1 text-xs font-medium border transition-colors',
                    (filter.genres ?? []).includes(g)
                      ? 'bg-sf-accent/20 border-sf-accent text-sf-accent-2'
                      : 'border-sf-line text-sf-ink-muted hover:border-sf-line-2 hover:text-sf-ink'
                  )}
                >
                  {g}
                </button>
              ))}
            </div>
          </div>

          {/* Year range */}
          <div>
            <Label className="mb-1.5 block text-xs font-semibold text-sf-ink-muted uppercase tracking-wider">
              Year Range
            </Label>
            <div className="flex gap-2 items-center">
              <Input
                type="number"
                placeholder="From"
                size="sm"
                value={filter.year_min ?? ''}
                onChange={(e) =>
                  updateFilter({
                    year_min: e.target.value
                      ? Number(e.target.value)
                      : undefined,
                  })
                }
              />
              <span className="text-sf-ink-faint">to</span>
              <Input
                type="number"
                placeholder="To"
                size="sm"
                value={filter.year_max ?? ''}
                onChange={(e) =>
                  updateFilter({
                    year_max: e.target.value
                      ? Number(e.target.value)
                      : undefined,
                  })
                }
              />
            </div>
          </div>

          {/* Rating & Votes */}
          <div className="grid grid-cols-2 gap-3">
            <div>
              <Label className="mb-1.5 block text-xs font-semibold text-sf-ink-muted uppercase tracking-wider">
                Min Rating
              </Label>
              <Input
                type="number"
                step="0.5"
                min="0"
                max="10"
                placeholder="7.0"
                value={filter.rating_min ?? ''}
                onChange={(e) =>
                  updateFilter({
                    rating_min: e.target.value
                      ? Number(e.target.value)
                      : undefined,
                  })
                }
              />
            </div>
            <div>
              <Label className="mb-1.5 block text-xs font-semibold text-sf-ink-muted uppercase tracking-wider">
                Min Votes
              </Label>
              <Input
                type="number"
                min="0"
                placeholder="100"
                value={filter.votes_min ?? ''}
                onChange={(e) =>
                  updateFilter({
                    votes_min: e.target.value
                      ? Number(e.target.value)
                      : undefined,
                  })
                }
              />
            </div>
          </div>

          {/* Status (shows only) */}
          {filter.type === 'shows' && (
            <div>
              <Label className="mb-1.5 block text-xs font-semibold text-sf-ink-muted uppercase tracking-wider">
                Status
              </Label>
              <Select
                value={filter.status ?? ''}
                onValueChange={(v) =>
                  updateFilter({
                    status: v ? (v as SmartListFilter['status']) : undefined,
                  })
                }
              >
                <SelectTrigger>
                  <SelectValue placeholder="Any status" />
                </SelectTrigger>
                <SelectContent>
                  {STATUS_OPTIONS.map((o) => (
                    <SelectItem key={o.value} value={o.value}>
                      {o.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          )}

          {/* Toggles */}
          <div className="flex gap-4">
            <label className="flex items-center gap-2 cursor-pointer">
              <input
                type="checkbox"
                checked={filter.watched ?? false}
                onChange={(e) => updateFilter({ watched: e.target.checked })}
                className="rounded border-sf-line"
              />
              <span className="text-sm text-sf-ink-muted">Only watched</span>
            </label>
            <label className="flex items-center gap-2 cursor-pointer">
              <input
                type="checkbox"
                checked={filter.collected ?? false}
                onChange={(e) => updateFilter({ collected: e.target.checked })}
                className="rounded border-sf-line"
              />
              <span className="text-sm text-sf-ink-muted">Only collected</span>
            </label>
          </div>

          {/* Active filters pills */}
          {hasFilters && (
            <div className="flex flex-wrap gap-1.5">
              {(filter.genres ?? []).map((g) => (
                <Badge
                  key={g}
                  variant="secondary"
                  className="cursor-pointer"
                  onClick={() => toggleGenre(g)}
                >
                  {g} ✕
                </Badge>
              ))}
              {filter.year_min != null && (
                <Badge variant="secondary">
                  Year ≥ {filter.year_min}
                </Badge>
              )}
              {filter.year_max != null && (
                <Badge variant="secondary">
                  Year ≤ {filter.year_max}
                </Badge>
              )}
              {filter.rating_min != null && (
                <Badge variant="secondary">
                  Rating ≥ {filter.rating_min}
                </Badge>
              )}
              {filter.votes_min != null && (
                <Badge variant="secondary">
                  Votes ≥ {filter.votes_min}
                </Badge>
              )}
              {filter.status && (
                <Badge variant="secondary">
                  Status: {filter.status}
                </Badge>
              )}
              {filter.watched && (
                <Badge variant="secondary">Watched</Badge>
              )}
              {filter.collected && (
                <Badge variant="secondary">Collected</Badge>
              )}
            </div>
          )}

          {/* Preview count */}
          {previewCount != null && (
            <div className="text-sm text-sf-ink-muted">
              <span className="font-bold text-sf-accent-2">{previewCount}</span>{' '}
              items match your filter
            </div>
          )}
        </div>

        <DialogFooter>
          {onPreview && (
            <Button
              variant="ghost"
              onClick={() => onPreview(filter)}
              disabled={!name.trim()}
            >
              Preview
            </Button>
          )}
          <Button
            variant="ghost"
            onClick={() => onOpenChange(false)}
          >
            Cancel
          </Button>
          <Button
            onClick={() => onSave(name, filter)}
            disabled={!name.trim()}
          >
            Save Smart List
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
