import { createRootRoute, Outlet } from '@tanstack/react-router'
import { SignedIn, SignedOut, RedirectToSignIn } from '@clerk/clerk-react'
import { AppShell } from '@/components/layout/app-shell'

export const Route = createRootRoute({
  component: RootLayout,
})

function RootLayout() {
  return (
    <>
      <SignedIn>
        <AppShell>
          <Outlet />
        </AppShell>
      </SignedIn>
      <SignedOut>
        <RedirectToSignIn />
      </SignedOut>
    </>
  )
}