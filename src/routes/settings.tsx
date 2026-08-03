import { createFileRoute } from '@tanstack/react-router'
import { Page, Section } from '@/components/layout'

export const Route = createFileRoute('/settings')({
  component: SettingsPage,
})

function SettingsPage() {
  return (
    <Page>
      <div className="pointer-events-none fixed inset-0 z-0 grid-bg" />
      <Section title="Settings">
        <p className="text-sf-ink-muted">Settings coming soon.</p>
      </Section>
    </Page>
  )
}