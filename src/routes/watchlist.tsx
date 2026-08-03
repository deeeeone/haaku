import { createFileRoute } from '@tanstack/react-router'
import { Page, Section } from '@/components/layout'

export const Route = createFileRoute('/watchlist')({
  component: WatchlistPage,
})

function WatchlistPage() {
  return (
    <Page>
      <div className="pointer-events-none fixed inset-0 z-0 grid-bg" />
      <Section title="Watchlist">
        <p className="text-sf-ink-muted">Watchlist coming soon.</p>
      </Section>
    </Page>
  )
}