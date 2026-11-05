import { useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { TrendingUp, Flame } from 'lucide-react'
import { Page, Section } from '@/components/layout'
import {
  FilterPill,
  Banner,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui'
import { ListCard } from '@/components/lists/ListCard'
import { useApi } from '@/api/client'
import { useTrendingLists, usePopularLists } from '@/api/hooks/useLists'
import type { TraktList } from '@/api/types/lists'

export const Route = createFileRoute('/lists/community')({
  component: CommunityListsPage })

function CommunityListsPage() {
  const api = useApi()
  const [tab, setTab] = useState<'trending' | 'popular'>('trending')
  const [period, setPeriod] = useState<'weekly' | 'monthly' | 'all'>('weekly')

  const { data: trending, isLoading: trendingLoading } = useTrendingLists(api)
  const { data: popular, isLoading: popularLoading } = usePopularLists(
    api,
    period
  )

  const isLoading = tab === 'trending' ? trendingLoading : popularLoading
  const data = tab === 'trending' ? trending : popular

  const lists: TraktList[] =
    data?.map((item: { list: TraktList; watchers: number }) => item.list) ?? []

  return (
    <Page>

      <Section>
        <div className="mb-6">
          <h1 className="font-display text-2xl font-bold text-sf-ink">
            Community Lists
          </h1>
          <p className="mt-1 text-sm text-sf-ink-muted">
            Discover and explore lists created by the community.
          </p>
        </div>

        {/* Tabs */}
        <div className="mb-6 flex flex-wrap gap-2 items-center">
          <FilterPill
            active={tab === 'trending'}
            onClick={() => setTab('trending')}
          >
            <TrendingUp className="size-3.5 mr-1.5" />
            Trending
          </FilterPill>
          <FilterPill
            active={tab === 'popular'}
            onClick={() => setTab('popular')}
          >
            <Flame className="size-3.5 mr-1.5" />
            Popular
          </FilterPill>

          {tab === 'popular' && (
            <Select
              value={period}
              onValueChange={(v) => setPeriod(v as typeof period)}
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="weekly">This Week</SelectItem>
                <SelectItem value="monthly">This Month</SelectItem>
                <SelectItem value="all">All Time</SelectItem>
              </SelectContent>
            </Select>
          )}
        </div>

        {/* Lists Grid */}
        {isLoading ? (
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {Array.from({ length: 6 }).map((_, i) => (
              <div
                key={i}
                className="glass h-48 animate-pulse rounded-sf-lg"
              />
            ))}
          </div>
        ) : lists.length === 0 ? (
          <Banner id="banner-f225e981" type="info">
            <p className="text-sf-ink-muted">
              No {tab} lists found. Check back later.
            </p>
          </Banner>
        ) : (
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {lists.map((list) => (
              <ListCard
                key={list.ids.trakt}
                list={list}
                showActions={false}
              />
            ))}
          </div>
        )}
      </Section>
    </Page>
  )
}