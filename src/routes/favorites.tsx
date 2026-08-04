import { useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { Heart, Trash2 } from 'lucide-react'
import { Page, Section, PosterGrid } from '@/components/layout'
import {
  PosterCard,
  FilterPill,
  PaginationControls,
} from '@/components/ui'
import { useFavorites, useRemoveFromFavorites } from '@/api/hooks'
import type { FavoriteItem } from '@/api/types'

export const Route = createFileRoute('/favorites')({
  component: FavoritesPage,
})

type MediaFilter = 'all' | 'movies' | 'shows'

const typeFilterMap: Record<MediaFilter, string | undefined> = {
  all: undefined,
  movies: 'movie',
  shows: 'show',
}

function FavoritesPage() {
  const [mediaType, setMediaType] = useState<MediaFilter>('all')
  const [page, setPage] = useState(1)

  const { data, isLoading } = useFavorites({
    type: typeFilterMap[mediaType],
    page,
    limit: 24,
  })

  const removeMutation = useRemoveFromFavorites()

  const items = data?.data ?? []
  const totalPages = data?.total_pages ?? 1

  const handleRemove = (item: FavoriteItem) => {
    const body = buildRemoveFavoritesBody(item)
    removeMutation.mutate(body)
  }

  return (
    <Page>

      <Section>
        <div className="mb-6 flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <h1 className="font-display text-2xl font-bold text-sf-ink">
              Favorites
            </h1>
            {data && (
              <p className="mt-1 text-sm text-sf-ink-muted">
                {data.total} favorited items
              </p>
            )}
          </div>

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
              <Heart className="size-6" />
            </span>
            <h3 className="font-display text-xl font-bold text-sf-ink">
              No favorites yet
            </h3>
            <p className="text-sm text-sf-ink-muted">
              Heart movies and shows to add them to your favorites.
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
                    <div className="absolute top-1 left-1 rounded-full bg-black/60 px-1.5 py-0.5 backdrop-blur-sm">
                      <Heart className="size-3 fill-sf-brand-red text-sf-brand-red" />
                    </div>
                    <button
                      type="button"
                      onClick={() => handleRemove(item)}
                      className="absolute top-1 right-1 rounded-full bg-black/60 p-1.5 text-white opacity-0 backdrop-blur-sm transition-opacity group-hover:opacity-100"
                      aria-label="Remove from favorites"
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

function buildRemoveFavoritesBody(item: FavoriteItem) {
  if (item.movie) {
    return { movies: [{ ids: item.movie.ids }] }
  }
  if (item.show) {
    return { shows: [{ ids: item.show.ids }] }
  }
  return { movies: [] }
}
