import { Link } from '@tanstack/react-router'
import { Home, Search, Bookmark, Clock, User } from 'lucide-react'
import { cn } from '@/lib/utils'

const navItems = [
  { to: '/', icon: Home, label: 'Home' },
  { to: '/search', icon: Search, label: 'Search' },
  { to: '/watchlist', icon: Bookmark, label: 'Watch' },
  { to: '/history', icon: Clock, label: 'History' },
  { to: '/profile', icon: User, label: 'Profile' },
]

export function MobileNav() {
  return (
    <nav className="glass fixed bottom-0 left-0 right-0 z-50 border-t border-sf-line lg:hidden">
      <div className="mx-auto flex max-w-lg items-center justify-around py-2">
        {navItems.map((item) => (
          <Link
            key={item.to}
            to={item.to}
            className={cn(
              'flex flex-col items-center gap-0.5 rounded-sf-sm px-3 py-1 text-[11px] text-sf-ink-hint transition-colors',
              '[&.active]:text-sf-accent'
            )}
          >
            <item.icon className="h-5 w-5" />
            {item.label}
          </Link>
        ))}
      </div>
    </nav>
  )
}