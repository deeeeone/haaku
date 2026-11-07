// @vitest-environment happy-dom
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from '@testing-library/react'
import type { ComponentType, ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@tanstack/react-router', async () => {
  const React = await vi.importActual<typeof import('react')>('react')
  return {
    Link: ({ to, children }: { to: string; children?: unknown }) =>
      React.createElement('a', { href: String(to) }, children as never),
    useNavigate: () => () => {},
    createFileRoute: () => (opts: Record<string, unknown>) => opts,
  }
})

const listUsersMock = vi.fn()
const userActionMock = vi.fn()
const issuePasswordResetMock = vi.fn()
vi.mock('@/api/admin', () => ({
  listUsers: (...args: unknown[]) => listUsersMock(...args),
  userAction: (...args: unknown[]) => userActionMock(...args),
  issuePasswordReset: (...args: unknown[]) => issuePasswordResetMock(...args),
}))

import { AuthContext, type AuthContextValue } from '@/auth/context'
import { Route as AdminRoute } from '@/routes/admin'
import { makeAuthValue } from '@/test/render-auth'

const AdminPage = (AdminRoute as unknown as { component: ComponentType })
  .component

const pendingUser = {
  id: 'local_pending',
  email: 'p@b.c',
  username: 'p',
  status: 'pending',
  is_admin: false,
  created_at: '2026-01-02T03:04:05Z',
  last_active_at: null,
}

function renderAdmin(overrides: Partial<AuthContextValue> = {}) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const value = makeAuthValue({
    isSignedIn: true,
    isAdmin: true,
    getToken: async () => 'hs_tok',
    ...overrides,
  })
  const wrap = (node: ReactNode) => (
    <QueryClientProvider client={client}>
      <AuthContext.Provider value={value}>{node}</AuthContext.Provider>
    </QueryClientProvider>
  )
  return render(wrap(<AdminPage />))
}

beforeEach(() => {
  listUsersMock.mockReset()
  userActionMock.mockReset()
  issuePasswordResetMock.mockReset()
})
afterEach(cleanup)

describe('admin panel', () => {
  it('hides the panel from a non-admin without calling the API', () => {
    renderAdmin({ isAdmin: false })
    expect(screen.getByText(/not authorized/i)).toBeTruthy()
    expect(listUsersMock).not.toHaveBeenCalled()
  })

  it('does not render the panel in clerk mode', () => {
    renderAdmin({ mode: 'clerk' })
    expect(screen.getByText(/local mode only/i)).toBeTruthy()
    expect(listUsersMock).not.toHaveBeenCalled()
  })

  it('lists accounts with the documented columns', async () => {
    listUsersMock.mockResolvedValue({
      users: [
        {
          id: 'local_1',
          email: 'a@b.c',
          username: 'a',
          status: 'active',
          is_admin: true,
          created_at: '2026-01-02T03:04:05Z',
          last_active_at: '2026-02-03T04:05:06Z',
        },
        pendingUser,
      ],
      total: 2,
      limit: 25,
      offset: 0,
    })

    renderAdmin()

    await waitFor(() => expect(screen.getByText('local_1')).toBeTruthy())
    expect(screen.getByText('local_pending')).toBeTruthy()
    expect(screen.getByText('a@b.c')).toBeTruthy()
    // Header row proves every required column is present.
    const headers = screen
      .getAllByRole('columnheader')
      .map((cell) => cell.textContent)
    for (const column of [
      'ID',
      'Email',
      'Username',
      'Status',
      'Admin',
      'Created',
      'Last active',
    ]) {
      expect(headers).toContain(column)
    }
    expect(screen.getByText('2 accounts')).toBeTruthy()
  })

  it('approves a pending account through the API', async () => {
    listUsersMock.mockResolvedValue({
      users: [pendingUser],
      total: 1,
      limit: 25,
      offset: 0,
    })
    userActionMock.mockResolvedValue({ ...pendingUser, status: 'active' })

    renderAdmin()
    await waitFor(() => expect(screen.getByText('local_pending')).toBeTruthy())
    fireEvent.click(screen.getByRole('button', { name: /approve/i }))

    await waitFor(() => expect(userActionMock).toHaveBeenCalled())
    expect(userActionMock).toHaveBeenCalledWith(
      'hs_tok',
      'local_pending',
      'approve'
    )
  })

  it('shows the reset url_path and token once, with a copy affordance', async () => {
    listUsersMock.mockResolvedValue({
      users: [pendingUser],
      total: 1,
      limit: 25,
      offset: 0,
    })
    issuePasswordResetMock.mockResolvedValue({
      user_id: 'local_pending',
      token: 'hr_onetime',
      expires_at: '2026-12-31T00:00:00Z',
      url_path: '/reset-password?token=hr_onetime',
      delivery: 'admin_handover',
    })

    renderAdmin()
    await waitFor(() => expect(screen.getByText('local_pending')).toBeTruthy())
    fireEvent.click(screen.getByRole('button', { name: /password reset/i }))

    await waitFor(() =>
      expect(screen.getByText('/reset-password?token=hr_onetime')).toBeTruthy()
    )
    expect(screen.getByText('hr_onetime')).toBeTruthy()
    expect(screen.getAllByRole('button', { name: /copy/i }).length).toBe(2)
    expect(screen.getByText(/not recoverable/i)).toBeTruthy()
  })

  it('renders the 403 refusal from the API rather than a crash', async () => {
    const { ApiError } = await import('@/api/fetch')
    listUsersMock.mockRejectedValue(
      new ApiError(403, 'admin access required', null)
    )
    renderAdmin()
    await waitFor(() =>
      expect(screen.getByText(/administrator access is required/i)).toBeTruthy()
    )
  })
})
