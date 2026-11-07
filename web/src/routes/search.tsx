import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { Film, List, Tv, User } from 'lucide-react'
import { useCallback, useEffect, useRef, useState } from 'react'
import { z } from 'zod'
import { useSearch as useSearchQuery } from '@/api/hooks/useSearch'
import type { SearchResult, SearchType } from '@/api/types'
import { Page, PosterGrid, Section } from '@/components/layout'
import {
  Button,
  FilterPill,
  PaginationControls,
  PosterCard,
  SearchBar,
  ShimmerText,
} from '@/components/ui'
import { activateOnKey } from '@/lib/a11y'

const searchSchema = z.object({
  q: z.string().optional().catch(''),
  type: z
    .enum(['movie', 'show', 'episode', 'person', 'list'])
    .optional()
    .catch(undefined),
  page: z.number().optional().catch(1),
})

export const Route = createFileRoute('/search')({
  component: SearchPage,
  validateSearch: searchSchema,
})

const FILTER_TYPES: { value: SearchType; label: string; icon: typeof Film }[] =
  [
    { value: 'movie', label: 'Movies', icon: Film },
    { value: 'show', label: 'TV Shows', icon: Tv },
    { value: 'episode', label: 'Episodes', icon: Tv },
    { value: 'person', label: 'People', icon: User },
    { value: 'list', label: 'Lists', icon: List },
  ]

function SearchPage() {
  const navigate = useNavigate()
  const searchParams = Route.useSearch()
  const [inputValue, setInputValue] = useState(searchParams.q ?? '')
  const debounceRef = useRef<ReturnType<typeof setTimeout>>(undefined)

  const activeQuery =
    searchParams.q && searchParams.q.length > 0
      ? {
          q: searchParams.q,
          type: searchParams.type,
          page: searchParams.page ?? 1,
          limit: 20,
        }
      : null

  const { data, isLoading, isError, refetch } = useSearchQuery(activeQuery)

  const handleInputChange = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      const val = e.target.value
      setInputValue(val)

      if (debounceRef.current) clearTimeout(debounceRef.current)
      debounceRef.current = setTimeout(() => {
        navigate({
          to: '/search',
          search: { q: val || undefined, type: searchParams.type, page: 1 },
          replace: true,
        })
      }, 300)
    },
    [navigate, searchParams.type]
  )

  const handleClear = useCallback(() => {
    setInputValue('')
    navigate({ to: '/search', search: {}, replace: true })
  }, [navigate])

  const handleTypeChange = useCallback(
    (type: SearchType | undefined) => {
      navigate({
        to: '/search',
        search: {
          q: searchParams.q || undefined,
          type: type === searchParams.type ? undefined : type,
          page: 1,
        },
        replace: true,
      })
    },
    [navigate, searchParams.q, searchParams.type]
  )

  const handlePageChange = useCallback(
    (page: number) => {
      navigate({
        to: '/search',
        search: { q: searchParams.q, type: searchParams.type, page },
        replace: true,
      })
      window.scrollTo({ top: 0, behavior: 'smooth' })
    },
    [navigate, searchParams.q, searchParams.type]
  )

  useEffect(() => {
    setInputValue(searchParams.q ?? '')
  }, [searchParams.q])

  const totalPages = data?.total_pages ?? 1
  const currentPage = searchParams.page ?? 1

  return (
    <Page>
      <Section>
        <SearchBar
          value={inputValue}
          onChange={handleInputChange}
          onClear={handleClear}
          placeholder="Search movies, shows, people..."
          className="w-full"
        />
      </Section>

      <div className="flex flex-wrap items-center gap-2 pb-2">
        {FILTER_TYPES.map((f) => (
          <FilterPill
            key={f.value}
            active={searchParams.type === f.value}
            onClick={() => handleTypeChange(f.value)}
          >
            <f.icon className="size-3.5" />
            {f.label}
          </FilterPill>
        ))}
      </div>

      <Section>
        {isLoading && (
          <div className="space-y-3">
            {Array.from({ length: 6 }).map((_, i) => (
              <ShimmerText key={i} className="h-16 w-full rounded-sf" />
            ))}
          </div>
        )}

        {isError && (
          <div className="flex flex-col items-center gap-4 py-16 text-center">
            <p className="text-sf-ink-muted">Something went wrong.</p>
            <Button variant="glass" onClick={() => refetch()}>
              Try again
            </Button>
          </div>
        )}

        {!isLoading &&
          !isError &&
          data &&
          data.data.length === 0 &&
          searchParams.q && (
            <div className="flex flex-col items-center gap-4 py-16 text-center">
              <p className="text-lg font-semibold text-sf-ink">
                No results for &ldquo;{searchParams.q}&rdquo;
              </p>
              <p className="text-sf-ink-muted">
                Try a different search term or adjust filters.
              </p>
              <div className="mt-2 flex flex-wrap justify-center gap-2">
                {['Action', 'Comedy', 'Drama', 'Horror', 'Sci-Fi'].map((g) => (
                  <FilterPill
                    key={g}
                    onClick={() => {
                      navigate({
                        to: '/search',
                        search: { q: g, type: 'movie' as SearchType, page: 1 },
                      })
                    }}
                  >
                    {g}
                  </FilterPill>
                ))}
              </div>
            </div>
          )}

        {!isLoading && !isError && !searchParams.q && (
          <div className="flex flex-col items-center gap-3 py-16 text-center">
            <p className="text-lg font-semibold text-sf-ink">
              Discover something new
            </p>
            <p className="text-sf-ink-muted">
              Search for your favorite movies, shows, episodes, people, or
              lists.
            </p>
          </div>
        )}

        {!isLoading && !isError && data && data.data.length > 0 && (
          <>
            <PosterGrid>
              {data.data.map((result, i) => (
                <SearchResultCard key={`${result.type}-${i}`} result={result} />
              ))}
            </PosterGrid>

            <div className="mt-8">
              <PaginationControls
                currentPage={currentPage}
                totalPages={totalPages}
                onPageChange={handlePageChange}
              />
            </div>
          </>
        )}
      </Section>
    </Page>
  )
}

function SearchResultCard({ result }: { result: SearchResult }) {
  const navigate = useNavigate()

  switch (result.type) {
    case 'movie': {
      const m = result.movie
      if (!m) return null
      return (
        <PosterCard
          title={String(m.title ?? '')}
          meta={String(m.year ?? '')}
          image={m.images?.poster}
          rating={m.rating ? (Number(m.rating) / 2).toFixed(1) : undefined}
          type="movie"
          aspect="2:3"
          onClick={() =>
            navigate({
              to: '/movies/$id',
              params: { id: String(m.ids.trakt ?? m.ids.slug ?? '') },
            })
          }
        />
      )
    }
    case 'show': {
      const s = result.show
      if (!s) return null
      return (
        <PosterCard
          title={String(s.title ?? '')}
          meta={String(s.year ?? '')}
          image={s.images?.poster}
          rating={s.rating ? (Number(s.rating) / 2).toFixed(1) : undefined}
          type="series"
          aspect="2:3"
          onClick={() =>
            navigate({
              to: '/shows/$id',
              params: { id: String(s.ids.trakt ?? s.ids.slug ?? '') },
            })
          }
        />
      )
    }
    case 'episode': {
      const ep = result.episode
      if (!ep) return null
      return (
        <PosterCard
          title={String(ep.title ?? '')}
          meta={`S${ep.season} E${ep.number}`}
          image={ep.images?.poster}
          type="episode"
          aspect="16:9"
          onClick={() =>
            navigate({
              to: '/episodes/$id',
              params: { id: String(ep.ids.trakt ?? ep.ids.slug ?? '') },
            })
          }
        />
      )
    }
    case 'person': {
      const p = result.person
      if (!p) return null
      return (
        <div
          role="button"
          tabIndex={0}
          className="cursor-pointer"
          onClick={() =>
            navigate({
              to: '/people/$id',
              params: { id: String(p.ids.trakt ?? p.ids.slug ?? '') },
            })
          }
          onKeyDown={activateOnKey}
        >
          <PosterCard
            title={String(p.name ?? '')}
            image={p.images?.headshot ?? p.images?.poster}
            aspect="2:3"
          />
        </div>
      )
    }
    case 'list': {
      const l = result.list
      if (!l) return null
      return (
        <div className="glass flex cursor-pointer flex-col gap-2 rounded-sf p-4">
          <p className="font-semibold text-sf-ink">{String(l.name ?? '')}</p>
          <p className="text-xs text-sf-ink-muted">
            {String(l.item_count ?? 0)} items · by{' '}
            {String(l.user?.username ?? '')}
          </p>
        </div>
      )
    }
    default:
      return null
  }
}
