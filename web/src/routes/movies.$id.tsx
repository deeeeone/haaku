import { useAuth } from '@clerk/react'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { Bookmark, BookmarkCheck, Heart, Play, Star, Users } from 'lucide-react'
import { useState } from 'react'
import {
  useMovie,
  useMoviePeople,
  useMovieRecommendations,
  useMovieStats,
} from '@/api/hooks/useMovies'
import {
  useAddRating,
  useAddToFavorites,
  useAddToWatchlist,
  useRemoveFromFavorites,
  useRemoveFromWatchlist,
} from '@/api/hooks/useSync'
import type { Ids } from '@/api/types'
import { Page, ScrollRow, Section } from '@/components/layout'
import {
  Button,
  DetailStat,
  FactRow,
  GenreStrip,
  Hero,
  InfoPanel,
  PosterCard,
  Rail,
  SectionHead,
  ShimmerText,
  StatCard,
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui'
import { activateOnKey } from '@/lib/a11y'
import { cn, formatDate, formatRuntime } from '@/lib/utils'

export const Route = createFileRoute('/movies/$id')({
  component: MovieDetailPage,
})

function buildMoviePayload(ids: Ids, rating?: number) {
  const item = rating ? { ids, rating } : { ids }
  return { movies: [item] }
}

function MovieDetailPage() {
  const { id } = Route.useParams()
  const navigate = useNavigate()
  const { isSignedIn } = useAuth()

  const { data: movie, isLoading, isError } = useMovie(id)
  const { data: people } = useMoviePeople(id)
  const { data: stats } = useMovieStats(id)
  const { data: recommendations } = useMovieRecommendations(id)

  const addWatchlist = useAddToWatchlist()
  const removeWatchlist = useRemoveFromWatchlist()
  const addFavorite = useAddToFavorites()
  const removeFavorite = useRemoveFromFavorites()
  const addRating = useAddRating()

  const [isWatchlisted, setIsWatchlisted] = useState(false)
  const [isFavorited, setIsFavorited] = useState(false)
  const [userRating, setUserRating] = useState(0)
  const [hoverRating, setHoverRating] = useState(0)

  const handleWatchlistToggle = () => {
    if (!movie || !isSignedIn) return
    const payload = buildMoviePayload(movie.ids)
    if (isWatchlisted) {
      removeWatchlist.mutate(payload)
      setIsWatchlisted(false)
    } else {
      addWatchlist.mutate(payload)
      setIsWatchlisted(true)
    }
  }

  const handleFavoriteToggle = () => {
    if (!movie || !isSignedIn) return
    const payload = buildMoviePayload(movie.ids)
    if (isFavorited) {
      removeFavorite.mutate(payload)
      setIsFavorited(false)
    } else {
      addFavorite.mutate(payload)
      setIsFavorited(true)
    }
  }

  const handleRate = (rating: number) => {
    if (!movie || !isSignedIn) return
    const payload = buildMoviePayload(movie.ids, rating)
    addRating.mutate(payload)
    setUserRating(rating)
  }

  if (isLoading) {
    return (
      <Page>
        <div className="space-y-6 py-16">
          <ShimmerText className="h-[400px] w-full rounded-sf-xl" />
          <ShimmerText className="h-8 w-64" />
          <ShimmerText className="h-4 w-full" />
        </div>
      </Page>
    )
  }

  if (isError || !movie) {
    return (
      <Page>
        <div className="flex flex-col items-center gap-4 py-24 text-center">
          <p className="text-lg font-semibold text-sf-ink">Movie not found</p>
          <Button variant="glass" onClick={() => navigate({ to: '/search' })}>
            Search movies
          </Button>
        </div>
      </Page>
    )
  }

  const cast = people?.cast?.slice(0, 10) ?? []
  const displayRating = movie.rating ? (movie.rating / 2).toFixed(1) : null

  return (
    <Page>
      <Hero
        eyebrow={String(movie.year)}
        title={movie.title}
        subtitle={movie.overview}
        image={movie.images?.backdrop ?? movie.images?.poster}
        chips={
          <GenreStrip
            genres={(movie.genres ?? []).map((g) => ({ label: g }))}
          />
        }
        actions={
          <div className="flex flex-wrap items-center gap-2">
            {isSignedIn && (
              <>
                <TooltipProvider>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Button
                        size="sm"
                        variant={isWatchlisted ? 'primary' : 'glass'}
                        onClick={handleWatchlistToggle}
                      >
                        {isWatchlisted ? (
                          <BookmarkCheck className="size-4" />
                        ) : (
                          <Bookmark className="size-4" />
                        )}
                        {isWatchlisted ? 'Watchlisted' : 'Watchlist'}
                      </Button>
                    </TooltipTrigger>
                    <TooltipContent>
                      {isWatchlisted
                        ? 'Remove from watchlist'
                        : 'Add to watchlist'}
                    </TooltipContent>
                  </Tooltip>
                </TooltipProvider>

                <TooltipProvider>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Button
                        size="icon"
                        variant={isFavorited ? 'primary' : 'glass'}
                        onClick={handleFavoriteToggle}
                      >
                        <Heart
                          className={cn(
                            'size-4',
                            isFavorited && 'fill-current text-sf-accent-3'
                          )}
                        />
                      </Button>
                    </TooltipTrigger>
                    <TooltipContent>
                      {isFavorited
                        ? 'Remove from favorites'
                        : 'Add to favorites'}
                    </TooltipContent>
                  </Tooltip>
                </TooltipProvider>

                <div className="flex items-center gap-0.5">
                  {[1, 2, 3, 4, 5, 6, 7, 8, 9, 10].map((r) => (
                    <button
                      key={r}
                      type="button"
                      className="cursor-pointer transition-colors"
                      onClick={() => handleRate(r)}
                      onMouseEnter={() => setHoverRating(r)}
                      onMouseLeave={() => setHoverRating(0)}
                      aria-label={`Rate ${r} out of 10`}
                    >
                      <Star
                        className={cn(
                          'size-4 transition-colors',
                          (hoverRating || userRating) >= r
                            ? 'fill-sf-accent-2 text-sf-accent-2'
                            : 'text-sf-ink-hint'
                        )}
                      />
                    </button>
                  ))}
                </div>
              </>
            )}

            {!isSignedIn && (
              <Button size="sm" variant="glass">
                Sign in to interact
              </Button>
            )}
          </div>
        }
      />

      <div className="mt-6 grid grid-cols-2 gap-4 sm:grid-cols-4">
        {displayRating && (
          <DetailStat label="Rating" value={`${displayRating}/5`} />
        )}
        {movie.runtime && (
          <DetailStat label="Runtime" value={formatRuntime(movie.runtime)} />
        )}
        {movie.released && (
          <DetailStat label="Released" value={formatDate(movie.released)} />
        )}
        {movie.country && <DetailStat label="Country" value={movie.country} />}
      </div>

      {movie.overview && (
        <Section title="Overview">
          <InfoPanel>
            <p className="text-sf-ink-muted leading-relaxed">
              {movie.overview}
            </p>
            <div className="mt-4 grid grid-cols-1 gap-1 sm:grid-cols-2">
              {movie.status && <FactRow label="Status" value={movie.status} />}
              {movie.language && (
                <FactRow label="Language" value={movie.language} />
              )}
              {movie.certification && (
                <FactRow label="Rating" value={movie.certification} />
              )}
              {movie.homepage && (
                <FactRow
                  label="Homepage"
                  value={
                    <a
                      href={movie.homepage}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="text-sf-accent-2 hover:underline"
                    >
                      Visit
                    </a>
                  }
                />
              )}
            </div>
          </InfoPanel>
        </Section>
      )}

      {cast.length > 0 && (
        <Section>
          <SectionHead title="Cast" action="View all" />
          <ScrollRow>
            {cast.map((member, i) => (
              <div
                key={`${member.person?.ids?.trakt ?? i}`}
                role="button"
                tabIndex={0}
                className="shrink-0 w-[160px] cursor-pointer"
                onClick={() => {
                  if (member.person?.ids?.trakt) {
                    navigate({
                      to: '/people/$id',
                      params: { id: String(member.person.ids.trakt) },
                    })
                  }
                }}
                onKeyDown={activateOnKey}
              >
                <PosterCard
                  title={member.person?.name ?? 'Unknown'}
                  meta={member.character}
                  image={member.person?.images?.headshot}
                  aspect="2:3"
                />
              </div>
            ))}
          </ScrollRow>
        </Section>
      )}

      {stats && (
        <Section title="Stats">
          <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
            {stats.watchers !== undefined && (
              <StatCard label="Watchers" value={stats.watchers} icon={Users} />
            )}
            {stats.plays !== undefined && (
              <StatCard label="Plays" value={stats.plays} icon={Play} />
            )}
            {stats.comments !== undefined && (
              <StatCard label="Comments" value={stats.comments} icon={Star} />
            )}
            {stats.lists !== undefined && (
              <StatCard label="Lists" value={stats.lists} icon={Bookmark} />
            )}
          </div>
        </Section>
      )}

      {recommendations && recommendations.length > 0 && (
        <Section>
          <Rail title="Similar Movies" accent="purple">
            {recommendations.map((m) => (
              <PosterCard
                key={m.ids?.trakt ?? m.ids?.slug}
                title={m.title}
                meta={String(m.year ?? '')}
                image={m.images?.poster}
                rating={m.rating ? (m.rating / 2).toFixed(1) : undefined}
                type="movie"
                aspect="2:3"
                onClick={() =>
                  navigate({
                    to: '/movies/$id',
                    params: { id: String(m.ids?.trakt ?? m.ids?.slug) },
                  })
                }
              />
            ))}
          </Rail>
        </Section>
      )}
    </Page>
  )
}
