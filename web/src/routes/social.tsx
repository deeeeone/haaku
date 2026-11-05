import { createFileRoute } from '@tanstack/react-router'
import { Loader2, Users } from 'lucide-react'
import { useState } from 'react'
import { useActivityFeed } from '@/api/hooks/useSync'
import { Page, Section } from '@/components/layout'
import { ActivityItem } from '@/components/social/ActivityItem'
import { Button, FilterPill } from '@/components/ui'

type ActivityFilter = 'all' | 'friends' | 'followers' | 'following'

export const Route = createFileRoute('/social')({
  component: SocialPage,
})

function SocialPage() {
  const [filter, setFilter] = useState<ActivityFilter>('friends')
  const [page, setPage] = useState(1)
  const limit = 20

  const {
    data: items = [],
    isLoading,
    isFetching,
  } = useActivityFeed({
    type: filter === 'all' ? undefined : filter,
    page,
    limit,
  })

  const hasMore = items.length >= limit

  const loadMore = () => {
    setPage((p) => p + 1)
  }

  return (
    <Page>
      <Section>
        <div className="mb-6">
          <h1 className="font-display text-2xl font-bold text-sf-ink">
            Social Feed
          </h1>
          <p className="mt-1 text-sm text-sf-ink-muted">
            See what your friends are watching and rating
          </p>
        </div>

        {/* Filters */}
        <div className="mb-6 flex gap-1 flex-wrap">
          {(
            ['friends', 'followers', 'following', 'all'] as ActivityFilter[]
          ).map((f) => (
            <FilterPill
              key={f}
              active={filter === f}
              onClick={() => {
                setFilter(f)
                setPage(1)
              }}
            >
              {f.charAt(0).toUpperCase() + f.slice(1)}
            </FilterPill>
          ))}
        </div>

        {/* Feed */}
        {isLoading ? (
          <div className="space-y-2">
            {Array.from({ length: 5 }).map((_, i) => (
              <div key={i} className="animate-pulse rounded-[--sf-radius] p-3">
                <div className="flex gap-3">
                  <div className="h-8 w-8 rounded-full bg-white/10" />
                  <div className="flex-1 space-y-2">
                    <div className="h-4 w-3/4 rounded bg-white/10" />
                    <div className="h-3 w-1/3 rounded bg-white/5" />
                  </div>
                </div>
              </div>
            ))}
          </div>
        ) : items.length === 0 ? (
          <div className="glass flex flex-col items-center gap-4 rounded-[var(--sf-radius-lg)] px-6 py-14 text-center">
            <span className="grid size-14 place-items-center rounded-full bg-sf-accent/12 text-sf-accent-2">
              <Users className="size-6" />
            </span>
            <h3 className="font-display text-xl font-bold text-sf-ink">
              No activity yet
            </h3>
            <p className="text-sm text-sf-ink-muted">
              Follow other users to see their activity here.
            </p>
          </div>
        ) : (
          <div className="space-y-1">
            {items.map((item) => (
              <ActivityItem key={item.id} item={item} />
            ))}

            {/* Load more */}
            {hasMore && (
              <div className="flex justify-center pt-4">
                <Button
                  variant="ghost"
                  onClick={loadMore}
                  disabled={isFetching}
                >
                  {isFetching ? (
                    <Loader2 className="mr-2 size-4 animate-spin" />
                  ) : null}
                  Load More
                </Button>
              </div>
            )}
          </div>
        )}
      </Section>
    </Page>
  )
}
