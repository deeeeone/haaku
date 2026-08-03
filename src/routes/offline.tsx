import { createFileRoute } from '@tanstack/react-router'
import { WifiOff } from 'lucide-react'

export const Route = createFileRoute('/offline')({
  component: OfflinePage,
})

function OfflinePage() {
  return (
    <div className="flex min-h-[60vh] flex-col items-center justify-center gap-6 px-6 text-center">
      <div className="rounded-full bg-white/[0.06] p-6">
        <WifiOff className="h-12 w-12 text-[--sf-accent-secondary]" />
      </div>
      <h1 className="text-2xl font-semibold text-[--sf-text-primary]">
        You're offline
      </h1>
      <p className="max-w-md text-[--sf-text-secondary]">
        It looks like your connection dropped. Don't worry — previously viewed
        content is still available while you're offline. Reconnect to get fresh
        data.
      </p>
      <div className="rounded-xl border border-white/[0.08] bg-white/[0.03] px-4 py-2 text-sm text-[--sf-text-tertiary]">
        <span className="inline-block h-2 w-2 rounded-full bg-green-400/60 mr-2" />
        Cached content available
      </div>
    </div>
  )
}
