import { createFileRoute } from '@tanstack/react-router'
import { Page, Section } from '@/components/layout'

export const Route = createFileRoute('/')({
  component: HomePage,
})

function HomePage() {
  return (
    <Page>
      {/* Grid background */}
      <div className="pointer-events-none fixed inset-0 z-0 grid-bg" />
      <div className="pointer-events-none fixed inset-0 z-0 vignette" />

      <Section title="Welcome to TKiller">
        <p className="text-sf-ink-muted">
          Track what you watch. Build your library. Discover new favorites.
        </p>
      </Section>

      <Section title="Trending Now">
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
          {Array.from({ length: 6 }).map((_, i) => (
            <div
              key={i}
              className="glass aspect-[2/3] animate-pulse rounded-sf"
            />
          ))}
        </div>
      </Section>
    </Page>
  )
}