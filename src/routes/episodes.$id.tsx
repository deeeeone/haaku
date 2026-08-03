import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { useEpisode, useEpisodeStats } from '@/api/hooks/useEpisodes'
import { Page, Section } from '@/components/layout'
import {
  Hero,
  DetailStat,
  InfoPanel,
  StatCard,
  Button,
  ShimmerText,
  FactRow,
} from '@/components/ui'
import { Star, Tv, Users, Play } from 'lucide-react'
import { formatDate, formatRuntime } from '@/lib/utils'

export const Route = createFileRoute('/episodes/$id')({
  component: EpisodeDetailPage,
})

function EpisodeDetailPage() {
  const { id } = Route.useParams()
  const navigate = useNavigate()

  const { data: episode, isLoading, isError } = useEpisode(id)
  const { data: stats } = useEpisodeStats(id)

  if (isLoading) {
    return (
      <Page>
        <div className="pointer-events-none fixed inset-0 z-0 grid-bg" />
        <div className="space-y-6 py-16">
          <ShimmerText className="h-[300px] w-full rounded-sf-xl" />
          <ShimmerText className="h-8 w-64" />
          <ShimmerText className="h-4 w-full" />
        </div>
      </Page>
    )
  }

  if (isError || !episode) {
    return (
      <Page>
        <div className="pointer-events-none fixed inset-0 z-0 grid-bg" />
        <div className="flex flex-col items-center gap-4 py-24 text-center">
          <p className="text-lg font-semibold text-sf-ink">Episode not found</p>
          <Button variant="glass" onClick={() => navigate({ to: '/search' })}>
            Search episodes
          </Button>
        </div>
      </Page>
    )
  }

  const displayRating = episode.rating ? (episode.rating / 2).toFixed(1) : null

  return (
    <Page>
      <div className="pointer-events-none fixed inset-0 z-0 grid-bg" />
      <div className="pointer-events-none fixed inset-0 z-0 vignette" />

      <Hero
        eyebrow={`Season ${episode.season} · Episode ${episode.number}`}
        title={episode.title}
        subtitle={episode.overview}
        image={episode.images?.backdrop ?? episode.images?.poster}
      />

      <div className="mt-6 grid grid-cols-2 gap-4 sm:grid-cols-4">
        {displayRating && (
          <DetailStat label="Rating" value={`${displayRating}/5`} />
        )}
        {episode.runtime && (
          <DetailStat label="Runtime" value={formatRuntime(episode.runtime)} />
        )}
        {episode.first_aired && (
          <DetailStat label="Aired" value={formatDate(episode.first_aired)} />
        )}
        <DetailStat label="Season / Episode" value={`S${episode.season} E${episode.number}`} />
      </div>

      {episode.overview && (
        <Section title="Overview">
          <InfoPanel>
            <p className="text-sf-ink-muted leading-relaxed">{episode.overview}</p>
            <div className="mt-4 grid grid-cols-1 gap-1 sm:grid-cols-2">
              <FactRow label="Season" value={episode.season} />
              <FactRow label="Episode" value={episode.number} />
              {episode.first_aired && <FactRow label="Air Date" value={formatDate(episode.first_aired)} />}
              {episode.runtime && <FactRow label="Runtime" value={formatRuntime(episode.runtime)} />}
            </div>
          </InfoPanel>
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