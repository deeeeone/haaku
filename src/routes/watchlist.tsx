import { useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { Grid3X3, Trash2 } from 'lucide-react'
import { Page, Section, PosterGrid } from '@/components/layout'
import {
  PosterCard,
  FilterPill,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  PaginationControls,
} from '@/components/ui'
import { useWatchlist, useRemoveFromWatchlist } from '@/api/hooks'
import type { WatchlistItem } from '@/api/types'

export const Route = createFileRoute('/watchlist')({
  component: WatchlistPage,
})

type MediaTypeFilter = 'all' | 'movies' | 'shows'

const typeMap: Record<MediaTypeFilter, string | undefined> = {
  all: undefined,
  movies: 'movie',
  shows: 'show',
}

function WatchlistPage() {
  const [mediaType, setMediaType] = useState<MediaTypeFilter>('all')
  const [sort, setSort] = useState<string>('rank')
  const [page, setPage] = useState(1)

  const { data, isLoading } = useWatchlist({
    type: typeMap[mediaType],
    sort,
    page,
    limit: 24,
  })

  const removeMutation = useRemoveFromWatchlist()

  const items = data?.data ?? []
  const totalPages = data?.total_pages ?? 1

  const handleRemove = (item: WatchlistItem) => {
    const body = buildRemoveBody(item)
    removeMutation.mutate(body)
  }

  return (
    <Page>
      <div className="pointer-events-none fixed inset-0 z-0 grid-bg" />

      <Section>
        <div className="mb-6 flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <h1 className="font-display text-2xl font-bold text-sf-ink">
              Watchlist
            </h1>
            {data && (
              <p className="mt-1 text-sm text-sf-ink-muted">
                {data.total} items
              </p>
            )}
          </div>

          <div className="flex items-center gap-3">
            <div className="flex gap-1">
              <FilterPill
                active={mediaType === 'all'}
                onClick={() => { setMediaType('all'); setPage(1) }}
              >
                All
              </FilterPill>
              <FilterPill
                active={mediaType === 'movies'}
                onClick={() => { setMediaType('movies'); setPage(1) }}
              >
                Movies
              </FilterPill>
              <FilterPill
                active={mediaType === 'shows'}
                onClick={() => { setMediaType('shows'); setPage(1) }}
              >
                Shows
              </FilterPill>
            </div>

            <Select value={sort} onValueChange={(v) => { setSort(v); setPage(1) }}>
              <SelectTrigger className="w-36">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="rank">Rank</SelectItem>
                <SelectItem value="title">Title</SelectItem>
                <SelectItem value="added_at">Added</SelectItem>
                <SelectItem value="released">Released</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </div>

        {isLoading ? (
          <PosterGrid>
            {Array.from({ length: 12 }).map((_, i) => (
              <div
                key={i}
                className="glass aspect-[2/3] animate-pulse rounded-sf"
              />
            ))}
          </PosterGrid>
        ) : items.length === 0 ? (
          <div className="glass flex flex-col items-center gap-4 rounded-[var(--sf-radius-lg)] px-6 py-14 text-center">
            <span className="grid size-14 place-items-center rounded-full bg-sf-accent/12 text-sf-accent-2">
              <Grid3X3 className="size-6" />
            </span>
            <h3 className="font-display text-xl font-bold text-sf-ink">
              Your watchlist is empty
            </h3>
            <p className="text-sm text-sf-ink-muted">
              Browse trending shows and movies to find something to watch.
            </p>
          </div>
        ) : (
          <>
            <PosterGrid>
              {items.map((item) => {
                const media = item.movie ?? item.show
                if (!media) return null
                return (
                  <div key={item.id} className="group relative">
                    <PosterCard
                      title={media.title}
                      meta={String(media.year)}
                      aspect="2:3"
                      type={item.movie ? 'movie' : 'series'}
                    />
                    <button
                      type="button"
                      onClick={() => handleRemove(item)}
                      className="absolute top-2 right-2 rounded-full bg-black/60 p-1.5 text-white opacity-0 backdrop-blur-sm transition-opacity group-hover:opacity-100"
                      aria-label="Remove from watchlist"
                    >
                      <Trash2 className="size-3.5" />
                    </button>
                  </div>
                )
              })}
            </PosterGrid>

            {totalPages > 1 && (
              <div className="mt-8 flex justify-center">
                <PaginationControls
                  currentPage={page}
                  totalPages={totalPages}
                  onPageChange={setPage}
                />
              </div>
            )}
          </>
        )}
      </Section>
    </Page>
  )
}

function buildRemoveBody(item: WatchlistItem) {
  if (item.movie) {
    return { movies: [{ ids: item.movie.ids }] }
  }
  if (item.show) {
    return { shows: [{ ids: item.show.ids }] }
  }
  if (item.episode) {
    return { episodes: [{ ids: item.episode.ids }] }
  }
  return { movies: [] }
}
