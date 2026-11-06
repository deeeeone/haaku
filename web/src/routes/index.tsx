import { createFileRoute } from '@tanstack/react-router'
import { motion } from 'framer-motion'
import { Bookmark, Clock, Heart, Play, Star } from 'lucide-react'
import {
  useActivityFeed,
  usePlaybackEpisodes,
  usePlaybackMovies,
  useRecommendationsMovies,
  useRecommendationsShows,
  useSocialRecommendationsMovies,
  useSocialRecommendationsShows,
} from '@/api/hooks'
import type {
  ActivityItem,
  PlaybackItem,
  RecommendationItem,
} from '@/api/types'
import { Page, ScrollRow, Section } from '@/components/layout'
import { PosterCard, SectionHead } from '@/components/ui'
import { formatDate } from '@/lib/utils'

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
                key={`${item.type}-${item.movie?.ids.trakt ?? item.episode?.ids.trakt}`}
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
            {personalizedRecs.map((item) => {
              const media = item.movie ?? item.show
              if (!media) return null
              return (
                <PosterCard
                  key={`rec-${media.ids.trakt}`}
                  title={media.title}
                  meta={String(media.year)}
                  aspect="2:3"
                  type={item.movie ? 'movie' : 'series'}
                />
              )
            })}
          </ScrollRow>
        </Section>
      )}

      {/* Friends Are Watching */}
      {friendRecs.length > 0 && (
        <Section>
          <SectionHead title="Friends Are Watching" />
          <ScrollRow>
            {friendRecs.map((item) => {
              const media = item.movie ?? item.show
              if (!media) return null
              return (
                <PosterCard
                  key={`social-${media.ids.trakt}`}
                  title={media.title}
                  meta={String(media.year)}
                  aspect="2:3"
                  type={item.movie ? 'movie' : 'series'}
                />
              )
            })}
          </ScrollRow>
        </Section>
      )}

      {/* Activity Feed */}
      {(activity ?? []).length > 0 && (
        <Section>
          <SectionHead title="Activity Feed" />
          <div className="space-y-2">
            {(activity ?? []).map((item) => (
              <ActivityRow key={item.id} item={item} />
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

function getActivityIcon(action: string) {
  if (
    action.includes('watch') ||
    action.includes('scrobble') ||
    action.includes('checkin')
  )
    return <Play className="size-3.5" />
  if (action.includes('rating') || action.includes('rate'))
    return <Star className="size-3.5" />
  if (action.includes('watchlist') || action.includes('list'))
    return <Bookmark className="size-3.5" />
  if (
    action.includes('favorite') ||
    action.includes('like') ||
    action.includes('recommend')
  )
    return <Heart className="size-3.5" />
  return <Clock className="size-3.5" />
}

function ActivityRow({ item }: { item: ActivityItem }) {
  const media = item.movie ?? item.show ?? item.episode
  const mediaTitle = media?.title ?? 'something'
  const userName = item.user?.name ?? item.user?.username ?? 'Someone'

  let actionText = ''
  if (item.action.includes('watch') || item.action.includes('scrobble')) {
    actionText = `${userName} watched ${mediaTitle}`
  } else if (item.action.includes('rating') || item.action.includes('rate')) {
    actionText = `${userName} rated ${mediaTitle} ${item.rating}/10`
  } else if (
    item.action.includes('watchlist') ||
    item.action.includes('list')
  ) {
    actionText = `${userName} added ${mediaTitle} to their watchlist`
  } else if (item.action.includes('favorite')) {
    actionText = `${userName} favorited ${mediaTitle}`
  } else if (item.action.includes('recommend')) {
    actionText = `${userName} recommends ${mediaTitle}`
  } else if (item.action.includes('checkin')) {
    actionText = `${userName} is watching ${mediaTitle}`
  } else {
    actionText = `${userName} ${item.action} ${mediaTitle}`
  }

  return (
    <motion.div
      initial={{ opacity: 0, x: -12 }}
      animate={{ opacity: 1, x: 0 }}
      className="glass sf-lift flex items-center gap-3 rounded-[var(--sf-radius)] p-3"
    >
      <span className="grid size-8 shrink-0 place-items-center rounded-full bg-sf-accent/12 text-sf-accent-2">
        {getActivityIcon(item.action)}
      </span>
      <div className="min-w-0 flex-1">
        <p className="truncate text-sm text-sf-ink">{actionText}</p>
        <p className="text-xs text-sf-ink-faint">
          {formatDate(item.timestamps.start)}
        </p>
      </div>
    </motion.div>
  )
}
