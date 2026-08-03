import { useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { Star, Trash2 } from 'lucide-react'
import { Page, Section, PosterGrid } from '@/components/layout'
import {
  PosterCard,
  FilterPill,
  RatingStars,
  PaginationControls,
} from '@/components/ui'
import { useRatings, useRemoveRating } from '@/api/hooks'
import type { RatingItem } from '@/api/types'

export const Route = createFileRoute('/ratings')({
  component: RatingsPage,
})

type MediaFilter = 'all' | 'movies' | 'shows' | 'episodes' | 'seasons'

const typeFilterMap: Record<MediaFilter, string | undefined> = {
  all: undefined,
  movies: 'movie',
  shows: 'show',
  episodes: 'episode',
  seasons: 'season',
}

function RatingsPage() {
  const [mediaType, setMediaType] = useState<MediaFilter>('all')
  const [page, setPage] = useState(1)

  const { data, isLoading } = useRatings({
    type: typeFilterMap[mediaType],
    page,
    limit: 24,
  })

  const removeMutation = useRemoveRating()

  const items = data?.data ?? []
  const totalPages = data?.total_pages ?? 1

  const handleRemove = (item: RatingItem) => {
    const body = buildRemoveRatingBody(item)
    removeMutation.mutate(body)
  }

  return (
    <Page>
      <div className="pointer-events-none fixed inset-0 z-0 grid-bg" />

      <Section>
        <div className="mb-6 flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <h1 className="font-display text-2xl font-bold text-sf-ink">
              Ratings
            </h1>
            {data && (
              <p className="mt-1 text-sm text-sf-ink-muted">
                {data.total} rated items
              </p>
            )}
          </div>

          <div className="flex gap-1 flex-wrap">
            {(['all', 'movies', 'shows', 'episodes', 'seasons'] as MediaFilter[]).map((f) => (
              <FilterPill
                key={f}
                active={mediaType === f}
                onClick={() => { setMediaType(f); setPage(1) }}
              >
                {f.charAt(0).toUpperCase() + f.slice(1)}
              </FilterPill>
            ))}
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
              <Star className="size-6" />
            </span>
            <h3 className="font-display text-xl font-bold text-sf-ink">
              No ratings yet
            </h3>
            <p className="text-sm text-sf-ink-muted">
              Rate movies and shows to see them here.
            </p>
          </div>
        ) : (
          <>
            <PosterGrid>
              {items.map((item) => {
                const media = item.movie ?? item.show ?? item.episode ?? item.season
                if (!media) return null
                const title = 'title' in media ? (media as { title: string }).title : ''
                const year = 'year' in media ? String((media as { year: number }).year) : ''

                return (
                  <div key={`${item.type}-${media.ids.trakt}-${item.rated_at}`} className="group relative">
                    <PosterCard
                      title={title || `Season ${'number' in media ? (media as { number: number }).number : ''}`}
                      meta={year}
                      aspect="2:3"
                      type={item.type === 'movie' ? 'movie' : 'series'}
                    />
                    <div className="absolute bottom-12 left-0 right-0 flex justify-center opacity-0 transition-opacity group-hover:opacity-100">
                      <RatingStars value={item.rating} readOnly size="sm" />
                    </div>
                    <div className="absolute top-1 left-1 rounded-full bg-black/60 px-1.5 py-0.5 backdrop-blur-sm">
                      <span className="flex items-center gap-0.5 text-xs font-bold text-sf-accent-2">
                        <Star className="size-3 fill-sf-accent-2" />
                        {item.rating}
                      </span>
                    </div>
                    <button
                      type="button"
                      onClick={() => handleRemove(item)}
                      className="absolute top-1 right-1 rounded-full bg-black/60 p-1.5 text-white opacity-0 backdrop-blur-sm transition-opacity group-hover:opacity-100"
                      aria-label="Remove rating"
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

function buildRemoveRatingBody(item: RatingItem) {
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
