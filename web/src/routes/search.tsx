import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { Film, List, Plus, Tv, User } from 'lucide-react'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { toast } from 'sonner'
import { z } from 'zod'
import {
  useResolveMedia,
  useSearch as useSearchQuery,
} from '@/api/hooks/useSearch'
import { buildSyncPayload, useAddToWatchlist } from '@/api/hooks/useSync'
import type { Ids, SearchResult, SearchType } from '@/api/types'
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
import { cardOf } from '@/lib/search-card'

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
  const resolveMedia = useResolveMedia()
  const addToWatchlist = useAddToWatchlist()
  const [busy, setBusy] = useState(false)
  const card = useMemo(() => cardOf(result), [result])

  // ensureLocalIds returns ids that address a local row, ingesting the title
  // first when the result came from a live provider search and is not in the
  // catalogue yet. A live hit carries a TMDB id, which is the identity the
  // resolve endpoint keys on.
  const ensureLocalIds = useCallback(async (): Promise<Ids | null> => {
    if (!card) return null
    if (card.ids.trakt > 0) return card.ids
    if (!card.tmdbId) return null
    const resolved = await resolveMedia.mutateAsync({
      type: card.mediaType === 'movie' ? 'movie' : 'show',
      id: String(card.tmdbId),
      idType: 'tmdb',
    })
    return resolved.ids
  }, [card, resolveMedia])

  const handleOpen = useCallback(async () => {
    if (!card) return
    if (card.navigateTo) {
      navigate({
        to: card.navigateTo.to,
        params: { id: card.navigateTo.id },
      })
      return
    }
    if (card.mediaType !== 'movie' && card.mediaType !== 'show') return

    setBusy(true)
    try {
      const local = await ensureLocalIds()
      if (!local) {
        toast.error('This title has no TMDB id to open')
        return
      }
      navigate({
        to: card.mediaType === 'movie' ? '/movies/$id' : '/shows/$id',
        params: { id: String(local.trakt || local.slug) },
      })
    } catch {
      toast.error('Could not load this title')
    } finally {
      setBusy(false)
    }
  }, [card, ensureLocalIds, navigate])

  const handleAddToWatchlist = useCallback(async () => {
    if (!card) return
    setBusy(true)
    try {
      const local = await ensureLocalIds()
      if (!local) {
        toast.error('This title has no TVDB id to watchlist')
        return
      }
      await addToWatchlist.mutateAsync(
        buildSyncPayload(card.mediaType === 'movie' ? 'movie' : 'show', local)
      )
    } catch {
      // useAddToWatchlist and useResolveMedia surface their own toast; this
      // catch only stops an unhandled rejection.
    } finally {
      setBusy(false)
    }
  }, [addToWatchlist, card, ensureLocalIds])

  if (!card) return null

  const { mediaType, title, meta, image, rating, aspect, inCatalog } = card

  // Lists have no media row and nothing to watchlist: render a plain card.
  if (mediaType === 'list') {
    return (
      <div className="glass flex flex-col gap-2 rounded-sf p-4">
        <p className="font-semibold text-sf-ink">{title}</p>
        <p className="text-xs text-sf-ink-muted">{meta}</p>
      </div>
    )
  }

  const providerOnly = !inCatalog

  return (
    <div className="group relative">
      <div
        role="button"
        tabIndex={0}
        className={busy ? 'cursor-progress opacity-70' : 'cursor-pointer'}
        onClick={handleOpen}
        onKeyDown={activateOnKey}
      >
        <PosterCard
          title={title}
          meta={meta}
          image={image}
          rating={rating}
          type={
            mediaType === 'movie'
              ? 'movie'
              : mediaType === 'show'
                ? 'series'
                : 'episode'
          }
          aspect={aspect}
        />
      </div>

      {(mediaType === 'movie' || mediaType === 'show') && (
        <div className="mt-2 flex items-center justify-between gap-2">
          <button
            type="button"
            disabled={busy}
            onClick={handleAddToWatchlist}
            className="inline-flex items-center gap-1 rounded-sf px-2 py-1 text-xs text-sf-ink-muted transition hover:text-sf-ink disabled:opacity-50"
            title={
              providerOnly
                ? 'Adds it to the catalogue and to your watchlist'
                : 'Add to watchlist'
            }
          >
            <Plus className="size-3.5" />
            Add to watchlist
          </button>
          {providerOnly && (
            <span className="text-[10px] uppercase tracking-wide text-sf-ink-muted">
              TVDB
            </span>
          )}
        </div>
      )}
    </div>
  )
}
