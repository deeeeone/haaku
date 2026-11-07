import { useNavigate, useRouterState } from '@tanstack/react-router'
import { type PropsWithChildren, useEffect } from 'react'
import { useAuth } from '@/auth/context'
import { Topbar } from '@/components/ui'
import { MobileNav } from './mobile-nav'
import { Sidebar } from './sidebar'
import { UserMenu } from './user-menu'

/** Screens that render bare (their own centred layout), even when signed out. */
const BARE_SCREENS = new Set(['/login', '/signup', '/reset-password'])

export function AppShell({ children }: PropsWithChildren) {
  const { mode, isSignedIn, isLoaded } = useAuth()
  const pathname = useRouterState({ select: (s) => s.location.pathname })
  const navigate = useNavigate()
  const onBareScreen = BARE_SCREENS.has(pathname)

  // In LOCAL mode a signed-out visitor must land on the login screen rather
  // than browse an app whose every request would 401. Clerk mode keeps its
  // existing behaviour (Clerk's own sign-in prompts), so this never runs there.
  const shouldRedirect =
    mode === 'local' && isLoaded && !isSignedIn && !onBareScreen
  useEffect(() => {
    if (shouldRedirect) navigate({ to: '/login' })
  }, [shouldRedirect, navigate])

  if (onBareScreen) {
    return (
      <div className="flex min-h-screen items-center justify-center px-4 py-10">
        {children}
      </div>
    )
  }

  if (shouldRedirect) {
    return (
      <div className="grid min-h-screen place-items-center text-sm text-sf-ink-muted">
        Redirecting to sign in…
      </div>
    )
  }

  return (
    <div className="flex h-screen flex-col overflow-hidden">
      <Topbar brand="haaku" userMenu={<UserMenu />} />
      <div className="flex flex-1 overflow-hidden">
        <Sidebar />
        <main className="flex-1 overflow-y-auto overflow-x-hidden">
          {children}
        </main>
      </div>
      <MobileNav />
    </div>
  )
}
