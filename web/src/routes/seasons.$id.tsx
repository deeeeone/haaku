import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { useSeason, useSeasonStats } from '@/api/hooks/useSeasons'
import { Page, Section } from '@/components/layout'
import {
  Hero,
  DetailStat,
  StatCard,
  SectionHead,
  Button,
  ShimmerText,
  InfoPanel,
} from '@/components/ui'
import { Star, Tv, Users, Play } from 'lucide-react'
import { formatDate } from '@/lib/utils'

export const Route = createFileRoute('/seasons/$id')({
  component: SeasonDetailPage,
})

function SeasonDetailPage() {
  const { id } = Route.useParams()
  const navigate = useNavigate()

  const { data: season, isLoading, isError } = useSeason(id)
  const { data: stats } = useSeasonStats(id)

  if (isLoading) {
    return (
      <Page>
        <div className="space-y-6 py-16">
          <ShimmerText className="h-[300px] w-full rounded-sf-xl" />
          <ShimmerText className="h-8 w-64" />
          <ShimmerText className="h-4 w-full" />
        </div>
      </Page>
    )
  }

  if (isError || !season) {
    return (
      <Page>
        <div className="flex flex-col items-center gap-4 py-24 text-center">
          <p className="text-lg font-semibold text-sf-ink">Season not found</p>
          <Button variant="glass" onClick={() => navigate({ to: '/search' })}>
            Search shows
          </Button>
        </div>
      </Page>
    )
  }

  const episodes = season.episodes ?? []
  const displayRating = season.rating ? (season.rating / 2).toFixed(1) : null

  return (
    <Page>

      <Hero
        eyebrow={`Season ${season.number}`}
        title={season.title ?? `Season ${season.number}`}
        subtitle={season.overview}
        image={season.images?.backdrop ?? season.images?.poster}
      />

      <div className="mt-6 grid grid-cols-2 gap-4 sm:grid-cols-4">
        {displayRating && (
          <DetailStat label="Rating" value={`${displayRating}/5`} />
        )}
        {season.episode_count !== undefined && (
          <DetailStat label="Episodes" value={season.episode_count} />
        )}
        {season.aired_episodes !== undefined && (
          <DetailStat label="Aired Episodes" value={season.aired_episodes} />
        )}
        {season.first_aired && (
          <DetailStat label="First Aired" value={formatDate(season.first_aired)} />
        )}
      </div>

      {season.overview && (
        <Section title="Overview">
          <InfoPanel>
            <p className="text-sf-ink-muted leading-relaxed">{season.overview}</p>
          </InfoPanel>
        </Section>
      )}

      {episodes.length > 0 && (
        <Section>
          <SectionHead title="Episodes" />
          <div className="space-y-2">
            {episodes.map((ep) => (
              <div
                key={ep.ids?.trakt ?? `${ep.season}-${ep.number}`}
                className="glass flex cursor-pointer items-center gap-4 rounded-sf p-4 transition-colors hover:bg-sf-panel-2"
                onClick={() => {
                  if (ep.ids?.trakt) {
                    navigate({ to: '/episodes/$id', params: { id: String(ep.ids.trakt) } })
                  }
                }}
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
                      E{ep.number}
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
                  {ep.runtime && <div>{ep.runtime}m</div>}
                </div>
              </div>
            ))}
          </div>
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
              <StatCard label="Lists" value={stats.lists} icon={Tv} />
            )}
          </div>
        </Section>
      )}
    </Page>
  )
}