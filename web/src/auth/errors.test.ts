import { describe, expect, it } from 'vitest'
import { describeAuthError, describeAuthErrorWithBody } from './errors'

describe('describeAuthError — documented codes', () => {
  it('answers 401 identically for unknown email and wrong password', () => {
    // The API deliberately returns the same 401 for both; the UI must not
    // "helpfully" distinguish them or it undoes the enumeration protection.
    const a = describeAuthError({ status: 401 })
    const b = describeAuthError({ status: 401 })
    expect(a).toEqual(b)
    expect(a.message.toLowerCase()).toContain('email or password')
    expect(a.message.toLowerCase()).not.toContain('unknown')
    expect(a.message.toLowerCase()).not.toContain('not found')
  })

  it('maps 403 account_pending to an approval message', () => {
    const copy = describeAuthError({ status: 403, code: 'account_pending' })
    expect(copy.title).toBe('Awaiting approval')
  })

  it('maps 403 account_banned to a locked-account message', () => {
    const copy = describeAuthError({ status: 403, code: 'account_banned' })
    expect(copy.title).toBe('Account locked')
  })

  it('maps 403 signup_closed to a closed-signup message', () => {
    const copy = describeAuthError({ status: 403, code: 'signup_closed' })
    expect(copy.message.toLowerCase()).toContain('not accepting new accounts')
  })

  it('surfaces Retry-After seconds on a 429', () => {
    const copy = describeAuthError({ status: 429, retryAfterSeconds: 60 })
    expect(copy.title).toBe('Too many attempts')
    expect(copy.message).toContain('60')
  })

  it('falls back to a generic 429 message without Retry-After', () => {
    const copy = describeAuthError({ status: 429 })
    expect(copy.message.toLowerCase()).toContain('shortly')
  })

  it('maps 409 to a duplicate-email message', () => {
    const copy = describeAuthError({ status: 409 })
    expect(copy.message.toLowerCase()).toContain('already exists')
  })
})

describe('describeAuthErrorWithBody — only 400 surfaces the server text', () => {
  it('passes through the server validation reason on 400', () => {
    const copy = describeAuthErrorWithBody({
      status: 400,
      serverMessage: 'password must be at least 10 characters',
    })
    expect(copy.message).toBe('password must be at least 10 characters')
  })

  it('ignores a server message on 401 in favour of the neutral copy', () => {
    const copy = describeAuthErrorWithBody({
      status: 401,
      serverMessage: 'no account with that email',
    })
    expect(copy.message).not.toContain('no account')
    expect(copy.message.toLowerCase()).toContain('email or password')
  })
})
