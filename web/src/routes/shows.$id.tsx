import { useAuth } from '@clerk/react'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import {
  Bookmark,
  BookmarkCheck,
  Heart,
  Play,
  Star,
  Tv,
  Users,
} from 'lucide-react'
import { useState } from 'react'
import {
  useShow,
  useShowPeople,
  useShowRecommendations,
  useShowSeasonEpisodes,
  useShowSeasons,
  useShowStats,
} from '@/api/hooks/useShows'
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
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  ShimmerText,
  StatCard,
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui'
import { activateOnKey } from '@/lib/a11y'
import { cn, formatDate, formatRuntime } from '@/lib/utils'

export const Route = createFileRoute('/shows/$id')({
  component: ShowDetailPage,
})

function buildShowPayload(ids: Ids, rating?: number) {
  const item = rating ? { ids, rating } : { ids }
  return { shows: [item] }
}

function ShowDetailPage() {
  const { id } = Route.useParams()
  const navigate = useNavigate()
  const { isSignedIn } = useAuth()

  const { data: show, isLoading, isError } = useShow(id)
  const { data: people } = useShowPeople(id)
  const { data: stats } = useShowStats(id)
  const { data: seasons } = useShowSeasons(id)
  const { data: recommendations } = useShowRecommendations(id)

  const [selectedSeason, setSelectedSeason] = useState<number>(1)
  const { data: episodes } = useShowSeasonEpisodes(id, selectedSeason)

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
    if (!show || !isSignedIn) return
    const payload = buildShowPayload(show.ids)
    if (isWatchlisted) {
      removeWatchlist.mutate(payload)
      setIsWatchlisted(false)
    } else {
      addWatchlist.mutate(payload)
      setIsWatchlisted(true)
    }
  }

  const handleFavoriteToggle = () => {
    if (!show || !isSignedIn) return
    const payload = buildShowPayload(show.ids)
    if (isFavorited) {
      removeFavorite.mutate(payload)
      setIsFavorited(false)
    } else {
      addFavorite.mutate(payload)
      setIsFavorited(true)
    }
  }

  const handleRate = (rating: number) => {
    if (!show || !isSignedIn) return
    const payload = buildShowPayload(show.ids, rating)
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

  if (isError || !show) {
    return (
      <Page>
        <div className="flex flex-col items-center gap-4 py-24 text-center">
          <p className="text-lg font-semibold text-sf-ink">Show not found</p>
          <Button variant="glass" onClick={() => navigate({ to: '/search' })}>
            Search shows
          </Button>
        </div>
      </Page>
    )
  }

  const cast = people?.cast?.slice(0, 10) ?? []
  const displayRating = show.rating ? (show.rating / 2).toFixed(1) : null

  return (
    <Page>
      <Hero
        eyebrow={String(show.year)}
        title={show.title}
        subtitle={show.overview}
        image={show.images?.backdrop ?? show.images?.poster}
        chips={
          <GenreStrip genres={(show.genres ?? []).map((g) => ({ label: g }))} />
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
        {show.runtime && (
          <DetailStat
            label="Episode Runtime"
            value={formatRuntime(show.runtime)}
          />
        )}
        {show.first_aired && (
          <DetailStat
            label="First Aired"
            value={formatDate(show.first_aired)}
          />
        )}
        {show.status && <DetailStat label="Status" value={show.status} />}
      </div>

      {show.overview && (
        <Section title="Overview">
          <InfoPanel>
            <p className="text-sf-ink-muted leading-relaxed">{show.overview}</p>
            <div className="mt-4 grid grid-cols-1 gap-1 sm:grid-cols-2">
              {show.network && <FactRow label="Network" value={show.network} />}
              {show.country && <FactRow label="Country" value={show.country} />}
              {show.certification && (
                <FactRow label="Rating" value={show.certification} />
              )}
              {show.aired_episodes !== undefined && (
                <FactRow label="Episodes" value={show.aired_episodes} />
              )}
              {show.homepage && (
                <FactRow
                  label="Homepage"
                  value={
                    <a
                      href={show.homepage}
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

      {seasons && seasons.length > 0 && (
        <Section>
          <SectionHead title="Seasons" />
          <div className="mb-4 flex items-center gap-3">
            <span className="text-sm text-sf-ink-muted">Season:</span>
            <Select
              value={String(selectedSeason)}
              onValueChange={(v) => setSelectedSeason(Number(v))}
            >
              <SelectTrigger className="w-[200px]">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {seasons
                  .filter((s) => s.number > 0)
                  .map((s) => (
                    <SelectItem key={s.number} value={String(s.number)}>
                      Season {s.number}
                      {s.episode_count ? ` (${s.episode_count} eps)` : ''}
                    </SelectItem>
                  ))}
              </SelectContent>
            </Select>
          </div>

          {episodes && episodes.length > 0 && (
            <div className="space-y-2">
              {episodes.map((ep) => (
                <div
                  key={ep.ids?.trakt ?? `${ep.season}-${ep.number}`}
                  role="button"
                  tabIndex={0}
                  className="glass flex cursor-pointer items-center gap-4 rounded-sf p-4 transition-colors hover:bg-sf-panel-2"
                  onClick={() => {
                    if (ep.ids?.trakt) {
                      navigate({
                        to: '/episodes/$id',
                        params: { id: String(ep.ids.trakt) },
                      })
                    }
                  }}
                  onKeyDown={activateOnKey}
                >
                  <div className="flex h-16 w-28 shrink-0 items-center justify-center overflow-hidden rounded-sf-sm bg-sf-bg-3">
                    {ep.images?.poster ? (
                      <img
                        src={ep.images.poster}
                        alt={ep.title}
                        className="h-full w-full object-cover"
                        loading="lazy"
                      />
                    ) : (
                      <Tv className="size-6 text-sf-ink-hint" />
                    )}
                  </div>
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <span className="text-xs font-bold text-sf-accent">
                        S{ep.season} E{ep.number}
                      </span>
                      {ep.rating && (
                        <span className="flex items-center gap-0.5 text-xs text-sf-ink-muted">
                          <Star className="size-3 fill-sf-accent-2 text-sf-accent-2" />
                          {(ep.rating / 2).toFixed(1)}
                        </span>
                      )}
                    </div>
                    <p className="font-semibold text-sf-ink">{ep.title}</p>
                    {ep.overview && (
                      <p className="mt-1 line-clamp-2 text-xs text-sf-ink-muted">
                        {ep.overview}
                      </p>
                    )}
                  </div>
                  <div className="shrink-0 text-right text-xs text-sf-ink-faint">
                    {ep.first_aired ? formatDate(ep.first_aired) : ''}
                    {ep.runtime && <div>{formatRuntime(ep.runtime)}</div>}
                  </div>
                </div>
              ))}
            </div>
          )}
        </Section>
      )}

      {cast.length > 0 && (
        <Section>
          <SectionHead title="Cast" />
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
          <Rail title="Similar Shows" accent="cyan">
            {recommendations.map((s) => (
              <PosterCard
                key={s.ids?.trakt ?? s.ids?.slug}
                title={s.title}
                meta={String(s.year ?? '')}
                image={s.images?.poster}
                rating={s.rating ? (s.rating / 2).toFixed(1) : undefined}
                type="series"
                aspect="2:3"
                onClick={() =>
                  navigate({
                    to: '/shows/$id',
                    params: { id: String(s.ids?.trakt ?? s.ids?.slug) },
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
