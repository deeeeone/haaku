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

/**
 * cardOf reduces a search result to what a card needs: which media type it is,
 * how to render it, and how to identify it at the provider when it is not in
 * the catalogue yet.
 */
interface CardModel {
  mediaType: 'movie' | 'show' | 'episode' | 'person' | 'list'
  title: string
  meta?: string
  image?: string
  rating?: string
  aspect: '16:9' | '2:3'
  ids: Ids
  /** tvdbId is the external id /v1/media/resolve ingests from, when needed. */
  tvdbId?: string
  /** inCatalog means the row exists locally: link straight to it. */
  inCatalog: boolean
  navigateTo?: { to: string; id: string }
}

function cardOf(result: SearchResult): CardModel | null {
  switch (result.type) {
    case 'movie': {
      const m = result.movie
      if (!m) return null
      const localId = m.ids.trakt || m.ids.id || m.ids.slug
      return {
        mediaType: 'movie',
        title: String(m.title ?? ''),
        meta: m.year ? String(m.year) : undefined,
        image: m.images?.poster,
        rating: m.rating ? (Number(m.rating) / 2).toFixed(1) : undefined,
        aspect: '2:3',
        ids: m.ids,
        tvdbId: m.ids.tvdb,
        inCatalog: m.in_catalog !== false && Boolean(localId),
        navigateTo: localId
          ? { to: '/movies/$id', id: String(localId) }
          : undefined,
      }
    }
    case 'show': {
      const s = result.show
      if (!s) return null
      const localId = s.ids.trakt || s.ids.id || s.ids.slug
      return {
        mediaType: 'show',
        title: String(s.title ?? ''),
        meta: s.year ? String(s.year) : undefined,
        image: s.images?.poster,
        rating: s.rating ? (Number(s.rating) / 2).toFixed(1) : undefined,
        aspect: '2:3',
        ids: s.ids,
        tvdbId: s.ids.tvdb,
        inCatalog: s.in_catalog !== false && Boolean(localId),
        navigateTo: localId
          ? { to: '/shows/$id', id: String(localId) }
          : undefined,
      }
    }
    case 'episode': {
      const ep = result.episode
      if (!ep) return null
      const localId = ep.ids.trakt || ep.ids.slug
      return {
        mediaType: 'episode',
        title: String(ep.title ?? ''),
        meta: `S${ep.season} E${ep.number}`,
        image: ep.images?.poster ?? ep.images?.thumb,
        aspect: '16:9',
        ids: ep.ids,
        inCatalog: Boolean(localId),
        navigateTo: localId
          ? { to: '/episodes/$id', id: String(localId) }
          : undefined,
      }
    }
    case 'person': {
      const p = result.person
      if (!p) return null
      return {
        mediaType: 'person',
        title: String(p.name ?? ''),
        image: p.images?.headshot ?? p.images?.poster,
        aspect: '2:3',
        ids: p.ids,
        inCatalog: true,
        navigateTo: p.ids.slug
          ? { to: '/people/$id', id: String(p.ids.slug) }
          : undefined,
      }
    }
    case 'list': {
      const l = result.list
      if (!l) return null
      return {
        mediaType: 'list',
        title: String(l.name ?? ''),
        meta: `${String(l.item_count ?? 0)} items · by ${String(l.user?.username ?? '')}`,
        aspect: '16:9',
        ids: { trakt: 0, slug: '' },
        inCatalog: true,
      }
    }
    default:
      return null
  }
}

function SearchResultCard({ result }: { result: SearchResult }) {
  const navigate = useNavigate()
  const resolveMedia = useResolveMedia()
  const addToWatchlist = useAddToWatchlist()
  const [busy, setBusy] = useState(false)
  const card = useMemo(() => cardOf(result), [result])

  // ensureLocalIds returns ids that address a local row, ingesting the title
  // first when the result came from TVDB and is not in the catalogue yet.
  const ensureLocalIds = useCallback(async (): Promise<Ids | null> => {
    if (!card) return null
    if (card.ids.trakt > 0) return card.ids
    if (!card.tvdbId) return null
    const resolved = await resolveMedia.mutateAsync({
      type: card.mediaType === 'movie' ? 'movie' : 'show',
      id: card.tvdbId,
      idType: 'tvdb',
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
        toast.error('This title has no TVDB id to open')
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
