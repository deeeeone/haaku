import { Link } from '@tanstack/react-router'
import {
  Home,
  Search,
  Bookmark,
  Clock,
  List,
  Calendar,
  Users,
  User,
  Settings,
} from 'lucide-react'
import { cn } from '@/lib/utils'

const navItems = [
  { to: '/', icon: Home, label: 'Home' },
  { to: '/search', icon: Search, label: 'Search' },
  { to: '/watchlist', icon: Bookmark, label: 'Watchlist' },
  { to: '/history', icon: Clock, label: 'History' },
  { to: '/lists', icon: List, label: 'Lists' },
  { to: '/calendar', icon: Calendar, label: 'Calendar' },
  { to: '/social', icon: Users, label: 'Social' },
]

const bottomItems = [
  { to: '/profile', icon: User, label: 'Profile' },
  { to: '/settings', icon: Settings, label: 'Settings' },
]

export function Sidebar() {
  return (
    <aside className="sticky top-[--sf-nav-h] hidden h-[calc(100vh-var(--sf-nav-h))] w-[220px] flex-shrink-0 flex-col border-r border-sf-line lg:flex">
      <div className="flex-1 overflow-y-auto px-3 pt-6">
        <nav className="flex flex-col gap-1">
          {navItems.map((item) => (
            <SidebarLink key={item.to} {...item} />
          ))}
        </nav>
      </div>
      <div className="flex-shrink-0 border-t border-sf-line px-3 py-4">
        <nav className="flex flex-col gap-1">
          {bottomItems.map((item) => (
            <SidebarLink key={item.to} {...item} />
          ))}
        </nav>
      </div>
    </aside>
  )
}

function SidebarLink({
  to,
  icon: Icon,
  label,
}: {
  to: string
  icon: React.ComponentType<{ className?: string }>
  label: string
}) {
  return (
    <Link
      to={to}
      className={cn(
        'flex items-center gap-3 rounded-sf-sm px-3 py-2.5 text-sm font-medium text-sf-ink-muted transition-colors hover:bg-sf-panel hover:text-sf-ink',
        '[&.active]:bg-sf-panel [&.active]:text-sf-ink'
      )}
    >
      <Icon className="h-5 w-5" />
      {label}
    </Link>
  )
}