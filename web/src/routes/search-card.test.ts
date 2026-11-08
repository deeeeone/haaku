// @vitest-environment happy-dom
import { describe, expect, it } from 'vitest'
import type { SearchResult } from '@/api/types'
import { cardOf } from '@/lib/search-card'

// A live TVDB hit as the server sends it: `in_catalog: false`, a tvdb id and a
// slug, and NO local row id. `normalizeIDs` turns the missing id into trakt: 0.
function providerHit(): SearchResult {
  return {
    type: 'show',
    score: 0,
    source: 'tvdb',
    show: {
      title: 'Breaking Bad',
      year: 2008,
      ids: { trakt: 0, id: 0, slug: 'breaking-bad', tvdb: '81189' },
    },
  } as unknown as SearchResult
}

function catalogHit(): SearchResult {
  return {
    type: 'show',
    score: 0,
    source: 'catalog',
    show: {
      title: 'Severance',
      year: 2022,
      ids: { trakt: 1, id: 1, slug: 'severance', tvdb: '371980' },
      in_catalog: true,
    },
  } as unknown as SearchResult
}

describe('cardOf — navigate only into rows that exist', () => {
  // The slug is not an id. Using it navigated to /shows/breaking-bad for a
  // title that was never ingested, so the detail page could not load and the
  // resolve path never ran.
  it('gives a provider-only hit no navigateTo and marks it out of catalogue', () => {
    const card = cardOf(providerHit())
    expect(card).not.toBeNull()
    expect(card?.inCatalog).toBe(false)
    expect(card?.navigateTo).toBeUndefined()
    // The TVDB id is still carried, which is what resolve needs.
    expect(card?.tvdbId).toBe('81189')
  })

  it('navigates a catalogue hit by its local id', () => {
    const card = cardOf(catalogHit())
    expect(card?.inCatalog).toBe(true)
    expect(card?.navigateTo).toEqual({ to: '/shows/$id', id: '1' })
  })
})
