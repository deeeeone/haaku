import { createFileRoute } from '@tanstack/react-router'
import { Page, Section } from '@/components/layout'

export const Route = createFileRoute('/lists')({
  component: ListsPage,
})

function ListsPage() {
  return (
    <Page>
      <div className="pointer-events-none fixed inset-0 z-0 grid-bg" />
      <Section title="Lists">
        <p className="text-sf-ink-muted">Lists coming soon.</p>
      </Section>
    </Page>
  )
}