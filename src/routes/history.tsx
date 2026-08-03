import { createFileRoute } from '@tanstack/react-router'
import { Page, Section } from '@/components/layout'

export const Route = createFileRoute('/history')({
  component: HistoryPage,
})

function HistoryPage() {
  return (
    <Page>
      <div className="pointer-events-none fixed inset-0 z-0 grid-bg" />
      <Section title="History">
        <p className="text-sf-ink-muted">History coming soon.</p>
      </Section>
    </Page>
  )
}