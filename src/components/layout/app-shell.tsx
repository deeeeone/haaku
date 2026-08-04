import type { PropsWithChildren } from 'react'
import { Topbar } from '@/components/ui'
import { Sidebar } from './sidebar'
import { MobileNav } from './mobile-nav'

export function AppShell({ children }: PropsWithChildren) {
  return (
    <div className="flex h-screen flex-col overflow-hidden">
      <Topbar brand="TKiller" />
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