// @vitest-environment happy-dom
import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import type { ComponentType } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

// The route files call `createFileRoute(...)` at module scope and their screens
// use `<Link>`/`useNavigate`. Mock the router so a screen can be rendered bare:
// `createFileRoute` returns the options object, so `Route.component` is the
// component under test, and `Link` degrades to an anchor.
vi.mock('@tanstack/react-router', async () => {
  const React = await vi.importActual<typeof import('react')>('react')
  return {
    Link: ({ to, children }: { to: string; children?: unknown }) =>
      React.createElement('a', { href: String(to) }, children as never),
    useNavigate: () => () => {},
    createFileRoute: () => (opts: Record<string, unknown>) => ({
      ...opts,
      useSearch: () =>
        (globalThis as unknown as { __SEARCH__?: unknown }).__SEARCH__ ?? {},
    }),
  }
})

const loginMock = vi.fn()
const signupMock = vi.fn()
const consumeMock = vi.fn()
vi.mock('@/api/local-auth', () => ({
  login: (...args: unknown[]) => loginMock(...args),
  signup: (...args: unknown[]) => signupMock(...args),
  consumePasswordReset: (...args: unknown[]) => consumeMock(...args),
  logout: vi.fn(),
}))

import { AuthRequestError } from '@/api/authfetch'
import { Route as LoginRoute } from '@/routes/login'
import { Route as ResetRoute } from '@/routes/reset-password'
import { Route as SignupRoute } from '@/routes/signup'
import { renderWithAuth, testConfig } from '@/test/render-auth'

const LoginPage = (LoginRoute as unknown as { component: ComponentType })
  .component
const SignupPage = (SignupRoute as unknown as { component: ComponentType })
  .component
const ResetPage = (ResetRoute as unknown as { component: ComponentType })
  .component

function authError(status: number, code?: string, retryAfterSeconds?: number) {
  return new AuthRequestError(status, code, retryAfterSeconds, undefined)
}

beforeEach(() => {
  loginMock.mockReset()
  signupMock.mockReset()
  consumeMock.mockReset()
})
afterEach(cleanup)

describe('login screen', () => {
  it('shows the owner setup path on a fresh instance', () => {
    renderWithAuth(<LoginPage />, {
      config: { ...testConfig, first_run: true },
    })
    expect(screen.getByText(/no accounts yet/i)).toBeTruthy()
    expect(screen.getByText(/becomes the owner/i)).toBeTruthy()
  })

  it('answers a 401 with neutral copy that never names the email', async () => {
    loginMock.mockRejectedValue(authError(401, 'unauthorized'))
    const setSession = vi.fn()
    renderWithAuth(<LoginPage />, { setSession })

    fireEvent.change(screen.getByLabelText('Email'), {
      target: { value: 'nobody@example.com' },
    })
    fireEvent.change(screen.getByLabelText('Password'), {
      target: { value: 'whatever123' },
    })
    fireEvent.click(screen.getByRole('button', { name: /sign in/i }))

    await waitFor(() => expect(screen.getByRole('alert')).toBeTruthy())
    const alert = screen.getByRole('alert').textContent ?? ''
    expect(alert.toLowerCase()).toContain('incorrect email or password')
    expect(alert.toLowerCase()).not.toContain('no account')
    expect(setSession).not.toHaveBeenCalled()
  })

  it('stores the token and navigates on success', async () => {
    loginMock.mockResolvedValue({
      user: { id: 'local_1', email: 'a@b.c', username: 'a', is_admin: false },
      session: { token: 'hs_tok', expires_at: '2030-01-01T00:00:00Z' },
    })
    const setSession = vi.fn()
    renderWithAuth(<LoginPage />, { setSession })

    fireEvent.change(screen.getByLabelText('Email'), {
      target: { value: 'a@b.c' },
    })
    fireEvent.change(screen.getByLabelText('Password'), {
      target: { value: 'supersecret123' },
    })
    fireEvent.click(screen.getByRole('button', { name: /sign in/i }))

    await waitFor(() => expect(setSession).toHaveBeenCalled())
    expect(setSession.mock.calls[0]?.[0]).toMatchObject({ token: 'hs_tok' })
  })

  it('surfaces 403 account_banned distinctly from a bad password', async () => {
    loginMock.mockRejectedValue(authError(403, 'account_banned'))
    renderWithAuth(<LoginPage />)

    fireEvent.change(screen.getByLabelText('Email'), {
      target: { value: 'a@b.c' },
    })
    fireEvent.change(screen.getByLabelText('Password'), {
      target: { value: 'supersecret123' },
    })
    fireEvent.click(screen.getByRole('button', { name: /sign in/i }))

    await waitFor(() =>
      expect(screen.getByText(/account locked/i)).toBeTruthy()
    )
  })
})

describe('signup screen', () => {
  it('renders the owner framing on first run', () => {
    renderWithAuth(<SignupPage />, {
      config: { ...testConfig, first_run: true },
    })
    expect(
      screen.getByRole('button', { name: /create owner account/i })
    ).toBeTruthy()
  })

  it('shows the awaiting-approval state (not an error) for a pending signup', async () => {
    signupMock.mockResolvedValue({
      user: { id: 'local_2', email: 'p@b.c', status: 'pending' },
      status: 'pending',
      session: null,
      message:
        'account created — an admin must approve it before you can log in',
    })
    renderWithAuth(<SignupPage />)

    fireEvent.change(screen.getByLabelText('Email'), {
      target: { value: 'p@b.c' },
    })
    fireEvent.change(screen.getByLabelText('Password'), {
      target: { value: 'supersecret123' },
    })
    fireEvent.click(screen.getByRole('button', { name: /create account/i }))

    await waitFor(() =>
      expect(screen.getAllByText(/account created/i).length).toBeGreaterThan(0)
    )
    expect(screen.getByText(/must approve it first/i)).toBeTruthy()
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('refuses the form when signups are closed', () => {
    renderWithAuth(<SignupPage />, {
      config: { ...testConfig, signup_policy: 'closed' },
    })
    expect(screen.getByText(/not accepting new accounts/i)).toBeTruthy()
    expect(screen.queryByLabelText('Email')).toBeNull()
  })

  it('blocks a too-short password before calling the API', async () => {
    renderWithAuth(<SignupPage />)
    fireEvent.change(screen.getByLabelText('Email'), {
      target: { value: 'p@b.c' },
    })
    fireEvent.change(screen.getByLabelText('Password'), {
      target: { value: 'short' },
    })
    fireEvent.click(screen.getByRole('button', { name: /create account/i }))
    await waitFor(() => expect(screen.getByRole('alert')).toBeTruthy())
    expect(signupMock).not.toHaveBeenCalled()
  })
})

describe('password-reset consume screen', () => {
  beforeEach(() => {
    ;(globalThis as unknown as { __SEARCH__?: unknown }).__SEARCH__ = {
      token: 'hr_tok',
    }
  })

  it('explains an invalid/expired token on 400', async () => {
    consumeMock.mockRejectedValue(authError(400, 'invalid_request'))
    renderWithAuth(<ResetPage />)
    fireEvent.change(screen.getByLabelText('New password'), {
      target: { value: 'brandnewpass1' },
    })
    fireEvent.change(screen.getByLabelText('Confirm password'), {
      target: { value: 'brandnewpass1' },
    })
    fireEvent.click(screen.getByRole('button', { name: /update password/i }))
    await waitFor(() =>
      expect(screen.getByText(/invalid or expired/i)).toBeTruthy()
    )
  })

  it('confirms success and revokes prior sessions', async () => {
    consumeMock.mockResolvedValue({ status: 'ok' })
    renderWithAuth(<ResetPage />)
    fireEvent.change(screen.getByLabelText('New password'), {
      target: { value: 'brandnewpass1' },
    })
    fireEvent.change(screen.getByLabelText('Confirm password'), {
      target: { value: 'brandnewpass1' },
    })
    fireEvent.click(screen.getByRole('button', { name: /update password/i }))
    await waitFor(() =>
      expect(screen.getByText(/password updated/i)).toBeTruthy()
    )
    expect(consumeMock).toHaveBeenCalledWith('hr_tok', 'brandnewpass1')
  })

  it('rejects mismatched confirmation without calling the API', async () => {
    renderWithAuth(<ResetPage />)
    fireEvent.change(screen.getByLabelText('New password'), {
      target: { value: 'brandnewpass1' },
    })
    fireEvent.change(screen.getByLabelText('Confirm password'), {
      target: { value: 'differentpass1' },
    })
    fireEvent.click(screen.getByRole('button', { name: /update password/i }))
    await waitFor(() => expect(screen.getByText(/do not match/i)).toBeTruthy())
    expect(consumeMock).not.toHaveBeenCalled()
  })
})
