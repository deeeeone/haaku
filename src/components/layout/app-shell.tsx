import type { PropsWithChildren } from 'react'
import { Topbar } from '@/components/ui'
import { Sidebar } from './sidebar'
import { MobileNav } from './mobile-nav'

/**
 * Main application shell: topbar + sidebar + content area + mobile nav.
 * Used by the root route to wrap all authenticated pages.
 */
export function AppShell({ children }: PropsWithChildren) {
  return (
    <div className="flex min-h-screen flex-col">
      <Topbar />
      <div className="flex flex-1">
        <Sidebar />
        <main className="flex-1 overflow-x-hidden">{children}</main>
      </div>
      <MobileNav />
    </div>
  )
}