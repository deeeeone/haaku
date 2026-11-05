import { Outlet, createFileRoute, Link, useLocation } from '@tanstack/react-router'
import { Page } from '@/components/layout'
import { cn } from '@/lib/utils'
import {
  User,
  Bell,
  Key,
  Webhook,
  Puzzle,
  Filter,
  AlertTriangle,
} from 'lucide-react'

export const Route = createFileRoute('/settings')({
  component: SettingsLayout,
})

const sidebarItems = [
  { to: '/settings', label: 'Profile', icon: User, exact: true },
  { to: '/settings/notifications', label: 'Notifications', icon: Bell },
  { to: '/settings/apikeys', label: 'API Keys', icon: Key },
  { to: '/settings/webhooks', label: 'Webhooks', icon: Webhook },
  { to: '/settings/integrations', label: 'Integrations', icon: Puzzle },
  { to: '/settings/filters', label: 'Filters', icon: Filter },
  { to: '/settings', label: 'Danger Zone', icon: AlertTriangle, hash: 'danger' },
]

function SettingsLayout() {
  const location = useLocation()
  const currentPath = location.pathname

  return (
    <Page>

      <div className="flex flex-col gap-6 lg:flex-row">
        {/* Sidebar */}
        <aside className="w-full shrink-0 lg:w-56">
          <nav className="sticky top-4 space-y-1">
            <h1 className="mb-4 font-display text-xl font-bold text-sf-ink">
              Settings
            </h1>
            {sidebarItems.map((item) => {
              const isActive = item.exact
                ? currentPath === '/settings' && !item.hash
                : currentPath === item.to ||
                  (item.to !== '/settings' && currentPath.startsWith(item.to))

              if (item.hash && currentPath === '/settings') {
                return (
                  <a
                    key={`${item.to}#${item.hash}`}
                    href={`#${item.hash}`}
                    className={cn(
                      'flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors',
                      'text-sf-ink-muted hover:bg-sf-panel-2 hover:text-sf-ink'
                    )}
                  >
                    <item.icon className="size-4" />
                    {item.label}
                  </a>
                )
              }

              return (
                <Link
                  key={item.to}
                  to={item.to as never}
                  className={cn(
                    'flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors',
                    isActive
                      ? 'bg-sf-accent/10 text-sf-accent'
                      : 'text-sf-ink-muted hover:bg-sf-panel-2 hover:text-sf-ink'
                  )}
                >
                  <item.icon className="size-4" />
                  {item.label}
                </Link>
              )
            })}
          </nav>
        </aside>

        {/* Content */}
        <div className="min-w-0 flex-1">
          <Outlet />
        </div>
      </div>
    </Page>
  )
}