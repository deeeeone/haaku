// @vitest-environment happy-dom
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { beforeEach, describe, expect, it } from 'vitest'
import {
  clearSession,
  clearSessionOnUnauthorized,
  readSession,
  writeSession,
} from '@/auth/session-store'

function source(relFromThisFile: string): string {
  return readFileSync(
    fileURLToPath(new URL(relFromThisFile, import.meta.url)),
    'utf8'
  )
}

describe('clearSessionOnUnauthorized — the shared 401 rule', () => {
  beforeEach(() => localStorage.clear())

  it('clears a held local session when the response is 401', () => {
    writeSession({ token: 'hs_stale', user: { id: 'local_1' } as never })
    expect(readSession()).not.toBeNull()

    clearSessionOnUnauthorized(new Response(null, { status: 401 }))

    expect(readSession()).toBeNull()
  })

  it('leaves a held session alone on a non-401 error', () => {
    writeSession({ token: 'hs_ok', user: { id: 'local_1' } as never })

    clearSessionOnUnauthorized(new Response(null, { status: 404 }))

    expect(readSession()).not.toBeNull()
    clearSession()
  })

  it('is a no-op on a 401 when no session is held', () => {
    expect(() =>
      clearSessionOnUnauthorized(new Response(null, { status: 401 }))
    ).not.toThrow()
    expect(readSession()).toBeNull()
  })
})

/**
 * Regression guard for #75. The 401-clears-session rule used to be copied in
 * both `authfetch.ts` (tested) and `client.ts` (every other screen, untested),
 * which could drift silently. These assertions fail if either module ever
 * grows its own copy again instead of delegating to the shared helper.
 */
describe('the 401 rule has exactly one implementation', () => {
  const callSites: Record<string, string> = {
    'authfetch.ts': source('./authfetch.ts'),
    'client.ts': source('./client.ts'),
  }

  for (const [name, text] of Object.entries(callSites)) {
    it(`${name} imports clearSessionOnUnauthorized from the session store`, () => {
      expect(text).toMatch(
        /import \{[^}]*\bclearSessionOnUnauthorized\b[^}]*\} from '@\/auth\/session-store'/
      )
    })

    it(`${name} does not reimplement the 401 rule locally`, () => {
      // A bare clearSession() call here would be a second copy of the rule.
      expect(text).not.toMatch(/\bclearSession\s*\(/)
    })
  }
})
