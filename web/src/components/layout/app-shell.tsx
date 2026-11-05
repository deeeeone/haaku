import { UserButton } from '@clerk/react'
import type { PropsWithChildren } from 'react'
import { Topbar } from '@/components/ui'
import { MobileNav } from './mobile-nav'
import { Sidebar } from './sidebar'

export function AppShell({ children }: PropsWithChildren) {
  return (
    <div className="flex h-screen flex-col overflow-hidden">
      <Topbar
        brand="TKiller"
        userMenu={
          <UserButton
            appearance={{
              // Theme Clerk's account popout to the app's design tokens.
              variables: {
                colorPrimary: 'var(--sf-accent)',
                colorBackground: 'var(--sf-bg-2)',
                colorForeground: 'var(--sf-ink)',
                colorInput: 'var(--sf-panel-2)',
                borderRadius: '0.75rem',
              },
              elements: {
                avatarBox: 'size-9',
              },
            }}
          />
        }
      />
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
