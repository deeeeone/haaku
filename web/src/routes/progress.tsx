import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { EyeOff, Play } from 'lucide-react'
import { useState } from 'react'
import { usePlaybackEpisodes, usePlaybackMovies } from '@/api/hooks/useSync'
import type { PlaybackItem } from '@/api/types'
import { Page, PosterGrid, Section } from '@/components/layout'
import { ProgressBar } from '@/components/social/ProgressBar'
import { FilterPill, PosterCard } from '@/components/ui'

type MediaFilter = 'all' | 'movies' | 'shows'

export const Route = createFileRoute('/progress')({
  component: ProgressPage,
})

function ProgressPage() {
  const navigate = useNavigate()
  const [filter, setFilter] = useState<MediaFilter>('all')
  const [hidden, setHidden] = useState<Set<number>>(new Set())

  const { data: movies = [], isLoading: moviesLoading } = usePlaybackMovies()
  const { data: episodes = [], isLoading: episodesLoading } =
    usePlaybackEpisodes()

  const isLoading = moviesLoading || episodesLoading

  // Combine and sort by most recent
  const allItems: PlaybackItem[] = [...movies, ...episodes]
    .filter((item) => !hidden.has(item.id))
    .sort(
      (a, b) =>
        new Date(b.paused_at).getTime() - new Date(a.paused_at).getTime()
    )

  const filtered =
    filter === 'all'
      ? allItems
      : filter === 'movies'
        ? allItems.filter((i) => i.type === 'movie')
        : allItems.filter((i) => i.type === 'episode')

  const handleRemove = (id: number) => {
    setHidden((prev) => new Set(prev).add(id))
  }

  return (
    <Page>
      <Section>
        <div className="mb-6">
          <h1 className="font-display text-2xl font-bold text-sf-ink">
            Continue Watching
          </h1>
          <p className="mt-1 text-sm text-sf-ink-muted">
            Pick up where you left off
          </p>
        </div>

        <div className="mb-6 flex gap-1 flex-wrap">
          {(['all', 'movies', 'shows'] as MediaFilter[]).map((f) => (
            <FilterPill
              key={f}
              active={filter === f}
              onClick={() => setFilter(f)}
            >
              {f.charAt(0).toUpperCase() + f.slice(1)}
            </FilterPill>
          ))}
        </div>

        {isLoading ? (
          <PosterGrid>
            {Array.from({ length: 8 }).map((_, i) => (
              <div
                key={i}
                className="glass aspect-[2/3] animate-pulse rounded-sf"
              />
            ))}
          </PosterGrid>
        ) : filtered.length === 0 ? (
          <div className="glass flex flex-col items-center gap-4 rounded-[var(--sf-radius-lg)] px-6 py-14 text-center">
            <span className="grid size-14 place-items-center rounded-full bg-sf-accent/12 text-sf-accent-2">
              <Play className="size-6" />
            </span>
            <h3 className="font-display text-xl font-bold text-sf-ink">
              No content in progress
            </h3>
            <p className="text-sm text-sf-ink-muted">
              Start watching something and come back to continue later.
            </p>
          </div>
        ) : (
          <PosterGrid>
            {filtered.map((item) => {
              const media = item.movie ?? item.episode
              if (!media) return null

              const title = 'title' in media ? media.title : ''
              const showInfo = item.show ? ` · ${item.show.title}` : ''
              const epInfo =
                'season' in media && 'number' in media
                  ? `S${String((media as { season: number }).season).padStart(2, '0')}E${String((media as { number: number }).number).padStart(2, '0')}`
                  : ''

              const onClick = () => {
                if (item.movie)
                  navigate({ to: `/movies/${item.movie.ids.trakt}` })
                else if (item.show)
                  navigate({ to: `/shows/${item.show.ids.trakt}` })
              }

              return (
                <div key={item.id} className="group relative">
                  <PosterCard
                    title={title}
                    meta={epInfo || showInfo || String(item.type)}
                    aspect="2:3"
                    type={item.type === 'movie' ? 'movie' : 'series'}
                    progress={item.progress}
                    onClick={onClick}
                  />

                  {/* Progress bar overlay */}
                  <div className="absolute bottom-8 left-2 right-2 opacity-0 transition-opacity group-hover:opacity-100">
                    <ProgressBar
                      value={item.progress}
                      size="sm"
                      showLabel={false}
                    />
                  </div>

                  {/* Progress badge */}
                  <div className="absolute top-2 left-2 rounded-full bg-black/60 px-1.5 py-0.5 backdrop-blur-sm">
                    <span className="text-[10px] font-bold text-sf-accent-2">
                      {Math.round(item.progress)}%
                    </span>
                  </div>

                  {/* Remove button */}
                  <button
                    type="button"
                    onClick={(e) => {
                      e.stopPropagation()
                      handleRemove(item.id)
                    }}
                    className="absolute top-2 right-2 rounded-full bg-black/60 p-1.5 text-white opacity-0 backdrop-blur-sm transition-opacity group-hover:opacity-100"
                    aria-label="Remove from continue watching"
                  >
                    <EyeOff className="size-3" />
                  </button>
                </div>
              )
            })}
          </PosterGrid>
        )}
      </Section>
    </Page>
  )
}
