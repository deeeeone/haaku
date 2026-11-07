// @vitest-environment happy-dom
//
// Renders the account control in LOCAL mode with the REAL @clerk/react — no
// vi.mock of Clerk anywhere in this file, deliberately.
//
// A self-hoster hit exactly this on a fresh install: "UserButton can only be
// used within the <ClerkProvider /> component" and a blank error page, while
// running the default local auth mode. The #74 suite mocks @clerk/react, so it
// cannot see a Clerk component rendered while no ClerkProvider is mounted —
// this test renders the real library instead, which is the only way that
// failure mode is observable without a browser.
import { render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { AuthProvider } from '@/auth/provider'
import { clearSession } from '@/auth/session-store'
import { UserMenu } from '@/components/layout/user-menu'

const localConfig = {
  mode: 'local',
  local_auth: true,
  signup_policy: 'approval',
  first_run: false,
  password_min_length: 10,
  password_kdf: 'argon2id',
  password_reset_delivery: 'admin_handover',
  session: { token_prefix: 'hs_', transport: 'bearer', ttl_seconds: 2592000 },
}

describe('local mode', () => {
  beforeEach(() => {
    clearSession()
    vi.stubGlobal(
      'fetch',
      vi.fn(
        async () =>
          new Response(JSON.stringify(localConfig), {
            status: 200,
            headers: { 'content-type': 'application/json' },
          })
      )
    )
  })
  afterEach(() => vi.unstubAllGlobals())

  it('renders the local account control and never mounts Clerk, which needs a ClerkProvider', async () => {
    render(
      <AuthProvider>
        <UserMenu />
      </AuthProvider>
    )

    // The local branch renders the design-kit pill with a Member/Admin subtitle.
    // The Clerk branch renders <UserButton>, which throws without a provider —
    // so reaching this assertion at all is the point of the test.
    expect(await screen.findByText('Member')).toBeTruthy()
    expect(screen.queryByText('Account')).toBeTruthy()

    // Nothing Clerk shippped into the tree: its button carries this class.
    expect(document.querySelector('.cl-userButton-root')).toBeNull()
  })
})
