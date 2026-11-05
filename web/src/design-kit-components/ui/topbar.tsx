import { Bell, History, List, Search, Settings } from 'lucide-react'
import type * as React from 'react'
import { cn } from '../../lib/utils'
import { ProfilePill } from '../shared/profile-pill'
import { Dropdown } from './dropdown'

export type TopbarProps = {
  brand?: React.ReactNode
  nav?: Array<{ label: string; active?: boolean }>
  /** Hide demo user/profile section. Default: true (hidden). */
  hideUser?: boolean
  /** Custom user section (e.g. Clerk's <UserButton />). Rendered when provided. */
  userMenu?: React.ReactNode
  className?: string
}

export function Topbar({
  brand,
  nav,
  hideUser = true,
  userMenu,
  className,
}: TopbarProps) {
  return (
    <header
      className={cn(
        'sticky top-0 z-40 h-(--sf-nav-h) border-b border-sf-line bg-sf-bg/70 backdrop-blur-xl',
        className
      )}
    >
      <div className="mx-auto flex h-full max-w-380 items-center justify-between gap-4 px-(--sf-edge)">
        <div className="flex items-center gap-8">
          <div className="flex items-center gap-2.5">
            <span className="grid size-9 place-items-center rounded-full bg-linear-to-br from-sf-accent to-sf-accent-2 shadow-[0_6px_20px_rgba(140,107,255,0.4)]">
              <svg
                viewBox="0 0 24 24"
                className="size-4 fill-white"
                aria-hidden="true"
              >
                <path d="M8 5l10 7-10 7z" />
              </svg>
            </span>
            <span className="font-display text-lg font-bold tracking-tight text-sf-ink">
              {brand ?? 'Design Kit'}
            </span>
          </div>

          {nav && nav.length > 0 && (
            <nav className="hidden items-center gap-7 lg:flex">
              {nav.map((item) => (
                <a
                  key={item.label}
                  href="#"
                  data-active={item.active}
                  className="sf-underline text-sm font-semibold text-sf-ink-muted transition-colors hover:text-sf-ink"
                >
                  {item.label}
                </a>
              ))}
            </nav>
          )}
        </div>

        <div className="flex items-center gap-2.5">
          <button
            type="button"
            aria-label="Search"
            className="glass hidden size-10 cursor-pointer items-center justify-center rounded-full text-sf-ink-muted transition-colors hover:bg-sf-panel-2 hover:text-sf-ink sm:flex"
          >
            <Search className="size-4" />
          </button>

          <button
            type="button"
            aria-label="Notifications"
            className="glass relative hidden size-10 cursor-pointer items-center justify-center rounded-full text-sf-ink-muted transition-colors hover:bg-sf-panel-2 hover:text-sf-ink sm:flex"
          >
            <Bell className="size-4" />
            <span className="absolute top-2 right-2.5 size-1.5 rounded-full bg-sf-accent-2" />
          </button>

          {userMenu ??
            (!hideUser && (
              <Dropdown
                trigger={<ProfilePill name="Alex Rivera" subtitle="Member" />}
                items={[
                  { label: 'My List', icon: <List className="size-4" /> },
                  {
                    label: 'Watch History',
                    icon: <History className="size-4" />,
                  },
                  { label: 'Settings', icon: <Settings className="size-4" /> },
                ]}
              />
            ))}
        </div>
      </div>
    </header>
  )
}
Topbar.displayName = 'Topbar'
