import { createFileRoute } from '@tanstack/react-router'
import { Bell, CheckCheck, Loader2 } from 'lucide-react'
import { Page, Section } from '@/components/layout'
import { Button } from '@/components/ui'
import { NotificationItem } from '@/components/social/NotificationItem'
import { useNotifications, useMarkNotificationsRead } from '@/api/hooks/useNotifications'

export const Route = createFileRoute('/notifications')({
  component: NotificationsPage,
})

function NotificationsPage() {
  const { data: notifications = [], isLoading } = useNotifications()
  const markRead = useMarkNotificationsRead()

  const unreadIds = notifications.filter((n) => !n.read).map((n) => n.id)

  const handleMarkAllRead = () => {
    if (unreadIds.length > 0) {
      markRead.mutate(unreadIds)
    }
  }

  return (
    <Page>
      <div className="pointer-events-none fixed inset-0 z-0 grid-bg" />

      <Section>
        <div className="mb-6 flex items-center justify-between">
          <div>
            <h1 className="font-display text-2xl font-bold text-sf-ink">
              Notifications
            </h1>
            <p className="mt-1 text-sm text-sf-ink-muted">
              {unreadIds.length > 0
                ? `${unreadIds.length} unread`
                : 'All caught up!'}
            </p>
          </div>

          {unreadIds.length > 0 && (
            <Button
              variant="ghost"
              size="sm"
              onClick={handleMarkAllRead}
              disabled={markRead.isPending}
            >
              {markRead.isPending ? (
                <Loader2 className="mr-1.5 size-4 animate-spin" />
              ) : (
                <CheckCheck className="mr-1.5 size-4" />
              )}
              Mark all read
            </Button>
          )}
        </div>

        {isLoading ? (
          <div className="space-y-2">
            {Array.from({ length: 5 }).map((_, i) => (
              <div key={i} className="animate-pulse rounded-[--sf-radius] p-3">
                <div className="flex gap-3">
                  <div className="h-8 w-8 rounded-full bg-white/10" />
                  <div className="flex-1 space-y-2">
                    <div className="h-4 w-3/4 rounded bg-white/10" />
                    <div className="h-3 w-1/4 rounded bg-white/5" />
                  </div>
                </div>
              </div>
            ))}
          </div>
        ) : notifications.length === 0 ? (
          <div className="glass flex flex-col items-center gap-4 rounded-[var(--sf-radius-lg)] px-6 py-14 text-center">
            <span className="grid size-14 place-items-center rounded-full bg-sf-accent/12 text-sf-accent-2">
              <Bell className="size-6" />
            </span>
            <h3 className="font-display text-xl font-bold text-sf-ink">
              No notifications
            </h3>
            <p className="text-sm text-sf-ink-muted">
              You'll see notifications here when someone interacts with your content.
            </p>
          </div>
        ) : (
          <div className="space-y-1">
            {notifications.map((n) => (
              <NotificationItem
                key={n.id}
                notification={n}
              />
            ))}
          </div>
        )}
      </Section>
    </Page>
  )
}
