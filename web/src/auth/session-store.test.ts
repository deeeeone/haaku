// @vitest-environment happy-dom
import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  clearSession,
  readSession,
  subscribeSession,
  writeSession,
} from './session-store'

describe('local session store', () => {
  beforeEach(() => localStorage.clear())

  it('round-trips a token + user', () => {
    writeSession({
      token: 'hs_abc',
      user: { id: 'local_1', email: 'a@b.c', username: 'a', is_admin: true },
    })
    expect(readSession()).toMatchObject({
      token: 'hs_abc',
      user: { id: 'local_1', is_admin: true },
    })
  })

  it('returns null when nothing is stored', () => {
    expect(readSession()).toBeNull()
  })

  it('returns null for a malformed stored value rather than throwing', () => {
    localStorage.setItem('haaku.local.session', 'not json')
    expect(readSession()).toBeNull()
    localStorage.setItem('haaku.local.session', JSON.stringify({ user: {} }))
    expect(readSession()).toBeNull()
  })

  it('clears the token on logout', () => {
    writeSession({ token: 'hs_abc', user: { id: 'local_1' } as never })
    clearSession()
    expect(readSession()).toBeNull()
  })

  it('notifies subscribers on set and clear', () => {
    const seen: Array<string | null> = []
    const unsubscribe = subscribeSession((session) =>
      seen.push(session?.token ?? null)
    )

    writeSession({ token: 'hs_1', user: { id: 'local_1' } as never })
    clearSession()
    expect(seen).toEqual(['hs_1', null])

    unsubscribe()
    writeSession({ token: 'hs_2', user: { id: 'local_1' } as never })
    expect(seen).toEqual(['hs_1', null])
  })

  it('does not let a storage failure crash the flow', () => {
    const setItem = vi
      .spyOn(Storage.prototype, 'setItem')
      .mockImplementation(() => {
        throw new Error('quota')
      })
    expect(() =>
      writeSession({ token: 'hs_x', user: { id: 'local_1' } as never })
    ).not.toThrow()
    setItem.mockRestore()
  })
})
