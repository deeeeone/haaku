import { createFileRoute } from '@tanstack/react-router'
import { WifiOff } from 'lucide-react'
import { Page } from '@/components/layout'

export const Route = createFileRoute('/offline')({
  component: OfflinePage,
})

function OfflinePage() {
  return (
    <Page className="flex flex-col items-center justify-center gap-6 text-center">
      <div className="rounded-full bg-white/[0.06] p-6">
        <WifiOff className="h-12 w-12 text-sf-accent-secondary" />
      </div>
      <h1 className="font-display text-2xl font-bold text-sf-ink">
        You're offline
      </h1>
      <p className="max-w-md text-sf-ink-muted">
        It looks like your connection dropped. Don't worry — previously viewed
        content is still available while you're offline. Reconnect to get fresh
        data.
      </p>
      <div className="glass flex items-center gap-2 rounded-sf px-4 py-2 text-sm text-sf-ink-faint">
        <span className="inline-block h-2 w-2 rounded-full bg-green-400/60" />
        Cached content available
      </div>
    </Page>
  )
}
