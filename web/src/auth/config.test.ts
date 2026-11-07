import { afterEach, describe, expect, it, vi } from 'vitest'
import { fetchAuthConfig } from './config'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json' },
  })
}

describe('fetchAuthConfig — mode discovery', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('parses the documented local-mode payload', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () =>
        json({
          mode: 'local',
          local_auth: true,
          signup_policy: 'approval',
          first_run: true,
          password_min_length: 10,
          password_kdf: 'argon2id',
          password_reset_delivery: 'admin_handover',
          session: {
            token_prefix: 'hs_',
            transport: 'bearer',
            ttl_seconds: 2592000,
          },
        })
      )
    )

    const config = await fetchAuthConfig('http://api.test/')
    expect(config).toEqual({
      mode: 'local',
      local_auth: true,
      signup_policy: 'approval',
      first_run: true,
      password_min_length: 10,
      password_kdf: 'argon2id',
      password_reset_delivery: 'admin_handover',
      session: {
        token_prefix: 'hs_',
        transport: 'bearer',
        ttl_seconds: 2592000,
      },
    })
  })

  it('never reports clerk unless the server says so', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => json({ mode: 'clerk', local_auth: false }))
    )
    const config = await fetchAuthConfig('http://api.test/')
    expect(config.mode).toBe('clerk')
  })

  it('treats an unrecognised mode as local, not clerk', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => json({ mode: 'weird' }))
    )
    const config = await fetchAuthConfig('http://api.test/')
    // Clerk is the riskier default (its provider needs a key), so anything
    // unexpected falls back to local.
    expect(config.mode).toBe('local')
  })

  it('defaults a missing password_min_length rather than accepting 0', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => json({ mode: 'local', password_min_length: 0 }))
    )
    const config = await fetchAuthConfig('http://api.test/')
    expect(config.password_min_length).toBe(10)
  })

  it('coerces an unknown signup_policy to approval', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => json({ mode: 'local', signup_policy: 'anything' }))
    )
    const config = await fetchAuthConfig('http://api.test/')
    expect(config.signup_policy).toBe('approval')
  })

  it('throws on a non-ok response so the caller can degrade', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => json({}, 500))
    )
    await expect(fetchAuthConfig('http://api.test/')).rejects.toThrow()
  })
})
