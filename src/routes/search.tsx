import { createFileRoute } from '@tanstack/react-router'
import { Page, Section } from '@/components/layout'

export const Route = createFileRoute('/search')({
  component: SearchPage,
})

function SearchPage() {
  return (
    <Page>
      <div className="pointer-events-none fixed inset-0 z-0 grid-bg" />
      <Section title="Search">
        <p className="text-sf-ink-muted">Search coming soon.</p>
      </Section>
    </Page>
  )
}