import { useQuery } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import {
  Clock,
  Eye,
  Film,
  List,
  Settings,
  Star,
  Tv,
  User,
  Users,
} from 'lucide-react'
import { useApi } from '@/api/client'
import { Page, Section } from '@/components/layout'
import {
  Badge,
  Banner,
  Button,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  MiniInfoCard,
  SectionHead,
  ShimmerText,
  StatCard,
} from '@/components/ui'

export const Route = createFileRoute('/profile')({
  component: ProfilePage,
})

function ProfilePage() {
  const api = useApi()

  // Fetch current user profile
  const { data: profile, isLoading: profileLoading } = useQuery({
    queryKey: ['profile', 'me'],
    queryFn: async () => {
      if (!api) return null
      const res = await api.GET('/v1/users/me')
      return res.data as unknown as Record<string, unknown> | null
    },
    enabled: !!api,
  })

  // Fetch user stats
  const { data: stats, isLoading: statsLoading } = useQuery({
    queryKey: ['profile', 'stats'],
    queryFn: async () => {
      if (!api) return null
      const res = await api.GET('/v1/users/me/stats' as never)
      return res.data as unknown as Record<string, unknown> | null
    },
    enabled: !!api,
  })

  // Fetch recent activity
  const { data: activity } = useQuery({
    queryKey: ['profile', 'activity'],
    queryFn: async () => {
      if (!api) return null
      const res = await api.GET('/v1/users/me/activity')
      return res.data as unknown as unknown[] | null
    },
    enabled: !!api,
  })

  // Fetch user lists
  const { data: lists } = useQuery({
    queryKey: ['profile', 'lists'],
    queryFn: async () => {
      if (!api) return null
      const res = await api.GET('/v1/users/me/lists/collaborations')
      return res.data as unknown as unknown[] | null
    },
    enabled: !!api,
  })

  // Fetch followers
  const { data: followers } = useQuery({
    queryKey: ['profile', 'followers'],
    queryFn: async () => {
      if (!api) return null
      const res = await api.GET('/v1/users/me/followers' as never)
      return res.data as unknown as unknown[] | null
    },
    enabled: !!api,
  })

  // Fetch following
  const { data: following } = useQuery({
    queryKey: ['profile', 'following'],
    queryFn: async () => {
      if (!api) return null
      const res = await api.GET('/v1/users/me/following' as never)
      return res.data as unknown as unknown[] | null
    },
    enabled: !!api,
  })

  const displayName = (profile as Record<string, unknown>)?.name as
    | string
    | undefined
  const username = (profile as Record<string, unknown>)?.username as
    | string
    | undefined
  const bio = (profile as Record<string, unknown>)?.bio as string | undefined
  const avatarUrl = (profile as Record<string, unknown>)?.avatar_url as
    | string
    | undefined
  const bannerUrl = (profile as Record<string, unknown>)?.banner_url as
    | string
    | undefined
  const isPrivate = (profile as Record<string, unknown>)?.private as
    | boolean
    | undefined

  const statsData = stats as Record<string, Record<string, number>> | null

  const isLoading = profileLoading || statsLoading

  return (
    <Page>
      {isLoading ? (
        <div className="space-y-6">
          <ShimmerText className="h-8 w-48" />
          <ShimmerText className="h-4 w-96" />
          <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
            {[...Array(4)].map((_, i) => (
              <ShimmerText key={i} className="h-24 rounded-xl" />
            ))}
          </div>
        </div>
      ) : profile ? (
        <>
          {/* Hero Section */}
          <Section>
            {bannerUrl && (
              <div className="relative mb-6 h-48 overflow-hidden rounded-2xl sm:h-64">
                <img
                  src={bannerUrl}
                  alt="Profile banner"
                  className="h-full w-full object-cover"
                />
                <div className="absolute inset-0 bg-gradient-to-t from-sf-bg/80 to-transparent" />
              </div>
            )}

            <div className="flex flex-col gap-6 sm:flex-row sm:items-start sm:justify-between">
              <div className="flex items-center gap-4">
                <div className="flex size-20 items-center justify-center overflow-hidden rounded-full bg-sf-panel-2 ring-2 ring-sf-line sm:size-24">
                  {avatarUrl ? (
                    <img
                      src={avatarUrl}
                      alt={displayName ?? username ?? 'User'}
                      className="h-full w-full object-cover"
                    />
                  ) : (
                    <User className="size-10 text-sf-ink-muted" />
                  )}
                </div>
                <div>
                  <h1 className="font-display text-2xl font-bold text-sf-ink">
                    {displayName ?? username ?? 'User'}
                  </h1>
                  {username && (
                    <p className="text-sm text-sf-ink-muted">@{username}</p>
                  )}
                  {bio && (
                    <p className="mt-2 max-w-lg text-sm text-sf-ink-muted">
                      {bio}
                    </p>
                  )}
                  {isPrivate && (
                    <Badge variant="secondary" className="mt-2">
                      Private Profile
                    </Badge>
                  )}
                </div>
              </div>

              <div className="flex gap-2">
                <Button
                  variant="glass"
                  size="sm"
                  onClick={() => {
                    window.location.href = '/settings'
                  }}
                >
                  <Settings className="size-4" />
                  Edit Profile
                </Button>
              </div>
            </div>
          </Section>

          {/* Stats */}
          <Section>
            <SectionHead title="Stats" />
            <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
              <StatCard
                label="Watched"
                value={String(
                  statsData?.movies?.watched ?? statsData?.plays ?? 0
                )}
                icon={Eye}
                badgeColor="purple"
              />
              <StatCard
                label="Movies"
                value={String(statsData?.movies?.watched ?? 0)}
                icon={Film}
                badgeColor="blue"
              />
              <StatCard
                label="Shows"
                value={String(statsData?.shows?.watched ?? 0)}
                icon={Tv}
                badgeColor="green"
              />
              <StatCard
                label="Lists"
                value={(lists as unknown[])?.length ?? 0}
                icon={List}
                badgeColor="orange"
              />
            </div>
          </Section>

          {/* Recent Activity */}
          {activity && Array.isArray(activity) && activity.length > 0 && (
            <Section>
              <SectionHead title="Recent Activity" />
              <div className="space-y-3">
                {activity.slice(0, 10).map((item: unknown, i: number) => {
                  const act = item as Record<string, unknown>
                  const action = act.action as string | undefined
                  const timestamp = act.timestamp as string | undefined
                  const media = act.media as Record<string, unknown> | undefined
                  const mediaTitle =
                    (media?.title as string) ??
                    (media?.name as string) ??
                    'Unknown'

                  let actionLabel = 'interacted with'
                  if (action === 'watch') actionLabel = 'watched'
                  else if (action === 'rate') actionLabel = 'rated'
                  else if (action === 'watchlist')
                    actionLabel = 'added to watchlist'
                  else if (action === 'list_add') actionLabel = 'added to list'
                  else if (action === 'favorite') actionLabel = 'favorited'
                  else if (action === 'comment') actionLabel = 'commented on'

                  return (
                    <Card key={i} className="flex-row items-center gap-4 p-4">
                      <div className="flex flex-1 items-center gap-3">
                        <div className="flex size-10 items-center justify-center rounded-full bg-sf-panel-2">
                          <Clock className="size-4 text-sf-ink-muted" />
                        </div>
                        <div>
                          <p className="text-sm text-sf-ink">
                            <span className="font-medium">{actionLabel}</span>{' '}
                            {mediaTitle}
                          </p>
                          {timestamp && (
                            <p className="text-xs text-sf-ink-faint">
                              {new Date(timestamp).toLocaleDateString()}
                            </p>
                          )}
                        </div>
                      </div>
                    </Card>
                  )
                })}
              </div>
            </Section>
          )}

          {/* Favorite Genres */}
          <Section>
            <SectionHead title="Favorite Genres" />
            <div className="flex flex-wrap gap-2">
              {(
                [
                  'Action',
                  'Science Fiction',
                  'Drama',
                  'Comedy',
                  'Thriller',
                ] as string[]
              ).map((genre) => (
                <Badge key={genre} variant="secondary">
                  {genre}
                </Badge>
              ))}
            </div>
          </Section>

          {/* Top Rated */}
          <Section>
            <SectionHead title="Top Rated" />
            <div className="flex gap-4 overflow-x-auto pb-2">
              {[...Array(5)].map((_, i) => (
                <div
                  key={i}
                  className="flex w-32 shrink-0 flex-col items-center gap-2"
                >
                  <div className="flex h-44 w-full items-center justify-center rounded-lg bg-sf-panel-2">
                    <Star className="size-8 text-sf-ink-hint" />
                  </div>
                  <p className="text-xs text-sf-ink-muted">Top {i + 1}</p>
                </div>
              ))}
            </div>
          </Section>

          {/* Public Lists */}
          {lists && Array.isArray(lists) && lists.length > 0 && (
            <Section>
              <SectionHead title="Your Lists" />
              <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
                {(lists as Record<string, unknown>[])
                  .slice(0, 6)
                  .map((list, i) => (
                    <Card key={i}>
                      <CardHeader>
                        <CardTitle>
                          {(list.name as string) ?? 'Untitled List'}
                        </CardTitle>
                      </CardHeader>
                      <CardContent>
                        <p className="text-sm text-sf-ink-muted">
                          {(list.description as string) ?? 'No description'}
                        </p>
                      </CardContent>
                    </Card>
                  ))}
              </div>
            </Section>
          )}

          {/* Followers / Following */}
          <Section>
            <div className="grid grid-cols-1 gap-8 sm:grid-cols-2">
              <div>
                <SectionHead
                  title={`Followers (${(followers as unknown[])?.length ?? 0})`}
                />
                {followers &&
                Array.isArray(followers) &&
                followers.length > 0 ? (
                  <div className="space-y-2">
                    {(followers as Record<string, unknown>[])
                      .slice(0, 5)
                      .map((f: Record<string, unknown>, i: number) => (
                        <MiniInfoCard
                          key={i}
                          icon={Users}
                          label={(f.username as string) ?? 'User'}
                          value={(f.display_name as string) ?? ''}
                        />
                      ))}
                  </div>
                ) : (
                  <p className="text-sm text-sf-ink-muted">No followers yet</p>
                )}
              </div>
              <div>
                <SectionHead
                  title={`Following (${(following as unknown[])?.length ?? 0})`}
                />
                {following &&
                Array.isArray(following) &&
                following.length > 0 ? (
                  <div className="space-y-2">
                    {(following as Record<string, unknown>[])
                      .slice(0, 5)
                      .map((f: Record<string, unknown>, i: number) => (
                        <MiniInfoCard
                          key={i}
                          icon={User}
                          label={(f.username as string) ?? 'User'}
                          value={(f.display_name as string) ?? ''}
                        />
                      ))}
                  </div>
                ) : (
                  <p className="text-sm text-sf-ink-muted">
                    Not following anyone yet
                  </p>
                )}
              </div>
            </div>
          </Section>
        </>
      ) : (
        <Banner type="error" id="profile-load-error">
          Failed to load profile. Please try again.
        </Banner>
      )}
    </Page>
  )
}
