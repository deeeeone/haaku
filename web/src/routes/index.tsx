import { createFileRoute } from '@tanstack/react-router'
import {
  useActivityFeed,
  usePlaybackEpisodes,
  usePlaybackMovies,
  useRecommendationsMovies,
  useRecommendationsShows,
  useSocialRecommendationsMovies,
  useSocialRecommendationsShows,
} from '@/api/hooks'
import type { PlaybackItem, RecommendationItem } from '@/api/types'
import { Page, ScrollRow, Section } from '@/components/layout'
import { ActivityItem, PosterCard, SectionHead } from '@/components/ui'

export const Route = createFileRoute('/')({
  component: HomePage,
})

function HomePage() {
  const { data: playbackMovies, isLoading: pmLoading } = usePlaybackMovies()
  const { data: playbackEpisodes, isLoading: peLoading } = usePlaybackEpisodes()
  const { data: recsMovies } = useRecommendationsMovies()
  const { data: recsShows } = useRecommendationsShows()
  const { data: socialMovies } = useSocialRecommendationsMovies()
  const { data: socialShows } = useSocialRecommendationsShows()
  const { data: activity } = useActivityFeed({ limit: 20 })

  const continueWatching: PlaybackItem[] = [
    ...(playbackMovies ?? []),
    ...(playbackEpisodes ?? []),
  ]
    .filter((p) => p.progress > 0)
    .slice(0, 12)

  const personalizedRecs: RecommendationItem[] = [
    ...(recsMovies ?? []),
    ...(recsShows ?? []),
  ].slice(0, 12)

  const friendRecs: RecommendationItem[] = [
    ...(socialMovies ?? []),
    ...(socialShows ?? []),
  ].slice(0, 12)

  const hasContent =
    continueWatching.length > 0 ||
    personalizedRecs.length > 0 ||
    friendRecs.length > 0 ||
    (activity ?? []).length > 0

  return (
    <Page>
      {/* Continue Watching */}
      {continueWatching.length > 0 && (
        <Section>
          <SectionHead title="Continue Watching" />
          <ScrollRow>
            {continueWatching.map((item) => (
              <PosterCard
                key={`${item.type}-${item.id}-${item.episode?.season ?? ''}-${item.episode?.number ?? ''}`}
                title={
                  item.type === 'episode'
                    ? (item.show?.title ?? item.episode?.title ?? 'Unknown')
                    : (item.movie?.title ?? 'Unknown')
                }
                meta={
                  item.type === 'episode'
                    ? `S${item.episode?.season ?? '?'}E${item.episode?.number ?? '?'} — ${item.episode?.title ?? ''}`
                    : String(item.movie?.year ?? '')
                }
                progress={item.progress}
                aspect="2:3"
              />
            ))}
          </ScrollRow>
        </Section>
      )}

      {(pmLoading || peLoading) && (
        <Section>
          <SectionHead title="Continue Watching" />
          <ScrollRow>
            {Array.from({ length: 5 }).map((_, i) => (
              <div
                key={i}
                className="glass aspect-[2/3] animate-pulse rounded-sf"
                style={{ minWidth: 'var(--sf-card-min)' }}
              />
            ))}
          </ScrollRow>
        </Section>
      )}

      {/* Personalized Recommendations */}
      {personalizedRecs.length > 0 && (
        <Section>
          <SectionHead title="Recommended for You" />
          <ScrollRow>
            {personalizedRecs.map((item) => (
              <PosterCard
                key={`rec-${item.media_type}-${item.id}`}
                title={item.title}
                meta={recommendationMeta(item)}
                image={item.poster || undefined}
                aspect="2:3"
                type={item.media_type === 'movie' ? 'movie' : 'series'}
              />
            ))}
          </ScrollRow>
        </Section>
      )}

      {/* Friends Are Watching */}
      {friendRecs.length > 0 && (
        <Section>
          <SectionHead title="Friends Are Watching" />
          <ScrollRow>
            {friendRecs.map((item) => (
              <PosterCard
                key={`social-${item.media_type}-${item.id}`}
                title={item.title}
                meta={recommendationMeta(item)}
                image={item.poster || undefined}
                aspect="2:3"
                type={item.media_type === 'movie' ? 'movie' : 'series'}
              />
            ))}
          </ScrollRow>
        </Section>
      )}

      {/* Activity Feed */}
      {(activity ?? []).length > 0 && (
        <Section>
          <SectionHead title="Activity Feed" />
          <div className="space-y-2">
            {(activity ?? []).map((item) => (
              <ActivityItem key={item.id} item={item} />
            ))}
          </div>
        </Section>
      )}

      {/* Empty state for fresh accounts */}
      {!hasContent && (
        <Section title="Welcome to haaku">
          <p className="text-sf-ink-muted">
            Track what you watch. Build your library. Discover new favorites.
          </p>
          <div className="mt-6 grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
            {Array.from({ length: 6 }).map((_, i) => (
              <div
                key={i}
                className="glass aspect-[2/3] animate-pulse rounded-sf"
              />
            ))}
          </div>
        </Section>
      )}
    </Page>
  )
}

function recommendationMeta(item: RecommendationItem): string {
  return item.released?.slice(0, 4) || item.first_aired?.slice(0, 4) || ''
}
