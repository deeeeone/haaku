import type { PropsWithChildren } from 'react'
import { UserButton } from '@clerk/clerk-react'
import { Topbar } from '@/components/ui'
import { Sidebar } from './sidebar'
import { MobileNav } from './mobile-nav'

export function AppShell({ children }: PropsWithChildren) {
  return (
    <div className="flex h-screen flex-col overflow-hidden">
      <Topbar
        brand="TKiller"
        userMenu={
          <UserButton
            afterSignOutUrl="/"
            appearance={{
              // Theme Clerk's account popout to the app's design tokens.
              variables: {
                colorPrimary: 'var(--sf-accent)',
                colorBackground: 'var(--sf-bg-2)',
                colorText: 'var(--sf-ink)',
                colorInputBackground: 'var(--sf-panel-2)',
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
