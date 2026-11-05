import { Github, Instagram, Twitter } from 'lucide-react'

import { cn } from '../../lib/utils'

export type FooterProps = {
  className?: string
}

export function Footer({ className }: FooterProps) {
  const links = [
    {
      title: 'Watch',
      items: ['Trending', 'New Releases', 'Originals', 'My List'],
    },
    { title: 'Browse', items: ['Movies', 'Series', 'Live TV', 'Genres'] },
    {
      title: 'Account',
      items: ['Profile', 'Settings', 'Billing', 'Help Center'],
    },
  ]

  return (
    <footer
      className={cn('mt-20 border-t border-sf-line bg-sf-bg-2/60', className)}
    >
      <div className="mx-auto max-w-380 px-(--sf-edge) py-12">
        <div className="grid gap-10 md:grid-cols-[1.4fr_1fr_1fr_1fr]">
          <div className="space-y-4">
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
                Design Kit
              </span>
            </div>
            <p className="max-w-xs text-sm text-sf-ink-faint">
              The everything-in-one streaming platform. Crafted to demo the
              design kit.
            </p>
            <div className="flex items-center gap-2">
              {[
                { Icon: Twitter, label: 'Twitter' },
                { Icon: Instagram, label: 'Instagram' },
                { Icon: Github, label: 'GitHub' },
              ].map(({ Icon, label }) => (
                <a
                  key={label}
                  href="#"
                  aria-label={label}
                  className="glass flex size-9 items-center justify-center rounded-full text-sf-ink-faint transition-colors hover:bg-sf-panel-2 hover:text-sf-ink"
                >
                  <Icon className="size-4" />
                </a>
              ))}
            </div>
          </div>

          {links.map((col) => (
            <div key={col.title} className="space-y-3">
              <p className="text-xs font-bold tracking-[0.16em] text-sf-ink-muted uppercase">
                {col.title}
              </p>
              <ul className="space-y-2.5">
                {col.items.map((item) => (
                  <li key={item}>
                    <a
                      href="#"
                      className="text-sm text-sf-ink-faint transition-colors hover:text-sf-ink"
                    >
                      {item}
                    </a>
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </div>

        <div className="mt-10 flex flex-col items-start justify-between gap-3 border-t border-sf-line pt-6 text-xs text-sf-ink-hint md:flex-row md:items-center">
          <p>
            © {new Date().getFullYear()} Design Kit. For demonstration
            only.
          </p>
          <p className="font-mono">
            Tokens re-authored — not byte-identical to the source.
          </p>
        </div>
      </div>
    </footer>
  )
}
Footer.displayName = 'Footer'
