import { createFileRoute } from '@tanstack/react-router'
import { Page, Section } from '@/components/layout'

export const Route = createFileRoute('/social')({
  component: SocialPage,
})

function SocialPage() {
  return (
    <Page>
      <div className="pointer-events-none fixed inset-0 z-0 grid-bg" />
      <Section title="Social">
        <p className="text-sf-ink-muted">Social feed coming soon.</p>
      </Section>
    </Page>
  )
}