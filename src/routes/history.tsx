import { useState, useMemo } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { motion } from 'framer-motion'
import { Trash2, Camera } from 'lucide-react'
import { Page, Section } from '@/components/layout'
import {
  FilterPill,
  Button,
} from '@/components/ui'
import { useHistory, useRemoveFromHistory } from '@/api/hooks'
import type { HistoryItem } from '@/api/types'

export const Route = createFileRoute('/history')({
  component: HistoryPage,
})

type MediaFilter = 'all' | 'movies' | 'shows' | 'episodes'

const typeFilterMap: Record<MediaFilter, string | undefined> = {
  all: undefined,
  movies: 'movie',
  shows: 'show',
  episodes: 'episode',
}

function formatDateLabel(dateStr: string): string {
  const date = new Date(dateStr)
  const now = new Date()
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  const yesterday = new Date(today.getTime() - 86400000)
  const weekAgo = new Date(today.getTime() - 7 * 86400000)
  const targetDate = new Date(date.getFullYear(), date.getMonth(), date.getDate())

  if (targetDate.getTime() === today.getTime()) return 'Today'
  if (targetDate.getTime() === yesterday.getTime()) return 'Yesterday'
  if (targetDate.getTime() > weekAgo.getTime()) return 'This Week'

  return date.toLocaleDateString('en-US', { month: 'long', year: 'numeric' })
}

function HistoryPage() {
  const [mediaType, setMediaType] = useState<MediaFilter>('all')
  const [page, setPage] = useState(1)

  const { data, isLoading } = useHistory({
    type: typeFilterMap[mediaType],
    page,
    limit: 50,
  })

  const removeMutation = useRemoveFromHistory()

  const items = data?.data ?? []
  const totalPages = data?.total_pages ?? 1

  const groupedItems = useMemo(() => {
    const groups: Record<string, HistoryItem[]> = {}
    for (const item of items) {
      if (!item.watched_at) continue
      const label = formatDateLabel(item.watched_at)
      if (!groups[label]) groups[label] = []
      groups[label].push(item)
    }
    return groups
  }, [items])

  const handleRemove = (item: HistoryItem) => {
    const body = buildRemoveHistoryBody(item)
    removeMutation.mutate(body)
  }

  const handleLoadMore = () => {
    if (page < totalPages) {
      setPage((p) => p + 1)
    }
  }

  return (
    <Page>
      <div className="pointer-events-none fixed inset-0 z-0 grid-bg" />

      <Section>
        <div className="mb-6">
          <h1 className="font-display text-2xl font-bold text-sf-ink">
            Watch History
          </h1>
          {data && (
            <p className="mt-1 text-sm text-sf-ink-muted">
              {data.total} items
            </p>
          )}
        </div>

        <div className="mb-6 flex gap-1 flex-wrap">
          {(['all', 'movies', 'shows', 'episodes'] as MediaFilter[]).map((f) => (
            <FilterPill
              key={f}
              active={mediaType === f}
              onClick={() => { setMediaType(f); setPage(1) }}
            >
              {f === 'all' ? 'All' : f.charAt(0).toUpperCase() + f.slice(1)}
            </FilterPill>
          ))}
        </div>

        {isLoading && page === 1 ? (
          <div className="space-y-6">
            {Array.from({ length: 3 }).map((_, i) => (
              <div key={i}>
                <div className="mb-3 h-5 w-24 animate-pulse rounded bg-sf-panel-2" />
                <div className="space-y-2">
                  {Array.from({ length: 3 }).map((_, j) => (
                    <div
                      key={j}
                      className="glass h-16 animate-pulse rounded-sf"
                    />
                  ))}
                </div>
              </div>
            ))}
          </div>
        ) : items.length === 0 ? (
          <div className="glass flex flex-col items-center gap-4 rounded-[var(--sf-radius-lg)] px-6 py-14 text-center">
            <span className="grid size-14 place-items-center rounded-full bg-sf-accent/12 text-sf-accent-2">
              <Camera className="size-6" />
            </span>
            <h3 className="font-display text-xl font-bold text-sf-ink">
              No watch history
            </h3>
            <p className="text-sm text-sf-ink-muted">
              Start watching movies and shows to build your history.
            </p>
          </div>
        ) : (
          <>
            {Object.entries(groupedItems).map(([label, group]) => (
              <div key={label} className="mb-8">
                <h3 className="mb-3 text-sm font-semibold text-sf-ink-muted">
                  {label}
                </h3>
                <div className="space-y-2">
                  {group.map((item) => (
                    <HistoryRow
                      key={item.id}
                      item={item}
                      onRemove={handleRemove}
                    />
                  ))}
                </div>
              </div>
            ))}

            {page < totalPages && (
              <div className="mt-6 flex justify-center">
                <Button
                  variant="glass"
                  onClick={handleLoadMore}
                  loading={isLoading}
                >
                  Load More
                </Button>
              </div>
            )}
          </>
        )}
      </Section>
    </Page>
  )
}

function HistoryRow({
  item,
  onRemove,
}: {
  item: HistoryItem
  onRemove: (item: HistoryItem) => void
}) {
  const media = item.movie ?? item.show
  const title = media?.title ?? item.episode?.title ?? 'Unknown'
  const subtitle = item.type === 'episode'
    ? `${item.show?.title ?? ''} — S${item.episode?.season ?? '?'}E${item.episode?.number ?? '?'}`
    : item.type === 'season'
      ? `${item.show?.title ?? ''} — Season ${item.season?.number ?? '?'}`
      : String(media?.year ?? '')

  const watchedDate = item.watched_at
    ? new Date(item.watched_at).toLocaleTimeString('en-US', {
        hour: 'numeric',
        minute: '2-digit',
      })
    : ''

  return (
    <motion.div
      initial={{ opacity: 0, y: 8 }}
      animate={{ opacity: 1, y: 0 }}
      className="glass sf-lift group flex items-center gap-4 rounded-[var(--sf-radius)] p-3"
    >
      <div className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-sf-bg-3 text-xs font-bold text-sf-ink-faint">
        {item.type === 'movie' ? 'MOV' : item.type === 'show' ? 'TV' : item.type === 'episode' ? 'EP' : 'OT'}
      </div>

      <div className="min-w-0 flex-1">
        <p className="truncate text-sm font-semibold text-sf-ink">{title}</p>
        <p className="truncate text-xs text-sf-ink-faint">
          {subtitle}{watchedDate ? ` — ${watchedDate}` : ''}
        </p>
      </div>

      <button
        type="button"
        onClick={() => onRemove(item)}
        className="rounded-full p-1.5 text-sf-ink-faint opacity-0 transition-all hover:bg-sf-bg-3 hover:text-sf-brand-red group-hover:opacity-100"
        aria-label="Remove from history"
      >
        <Trash2 className="size-4" />
      </button>
    </motion.div>
  )
}

function buildRemoveHistoryBody(item: HistoryItem) {
  if (item.movie) {
    return { movies: [{ ids: item.movie.ids }] }
  }
  if (item.show) {
    return { shows: [{ ids: item.show.ids }] }
  }
  if (item.episode) {
    return { episodes: [{ ids: item.episode.ids }] }
  }
  if (item.season) {
    return { seasons: [{ ids: item.season.ids }] }
  }
  return { movies: [] }
}
