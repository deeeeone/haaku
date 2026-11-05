import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import {
  Bell,
  Film,
  Heart,
  ListPlus,
  MessageCircle,
  Save,
  Star,
  Tv,
  UserPlus,
} from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'
import { useApi } from '@/api/client'
import {
  Banner,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
  Label,
  SectionHead,
} from '@/components/ui'
import { cn } from '@/lib/utils'

export const Route = createFileRoute('/settings/notifications')({
  component: NotificationsPage,
})

interface NotificationToggle {
  key: string
  label: string
  description: string
  icon: React.ElementType
}

const NOTIFICATION_TYPES: NotificationToggle[] = [
  {
    key: 'follows',
    label: 'New Followers',
    description: 'When someone follows you',
    icon: UserPlus,
  },
  {
    key: 'comments',
    label: 'Comments',
    description: 'When someone comments on your lists or activity',
    icon: MessageCircle,
  },
  {
    key: 'likes',
    label: 'Likes',
    description: 'When someone likes your lists or comments',
    icon: Heart,
  },
  {
    key: 'list_shares',
    label: 'List Shares',
    description: 'When someone shares your lists',
    icon: ListPlus,
  },
  {
    key: 'ratings',
    label: 'Ratings',
    description: 'When there are new ratings on your content',
    icon: Star,
  },
  {
    key: 'new_episodes',
    label: 'New Episodes',
    description: 'When new episodes air for shows you watch',
    icon: Tv,
  },
  {
    key: 'movie_releases',
    label: 'Movie Releases',
    description: 'When movies on your watchlist are released',
    icon: Film,
  },
  {
    key: 'recommendations',
    label: 'Recommendations',
    description: 'Weekly personalized recommendations',
    icon: Bell,
  },
]

function NotificationsPage() {
  const api = useApi()
  const queryClient = useQueryClient()

  const { data: notifSettings } = useQuery({
    queryKey: ['notifications', 'settings'],
    queryFn: async () => {
      if (!api) return null
      const res = await api.GET('/v1/notifications/settings')
      return res.data as unknown as Record<string, unknown> | null
    },
    enabled: !!api,
  })

  const [toggles, setToggles] = useState<Record<string, boolean>>({})
  const [initialized, setInitialized] = useState(false)

  if (notifSettings && !initialized) {
    const initial: Record<string, boolean> = {}
    for (const nt of NOTIFICATION_TYPES) {
      const val = (notifSettings as Record<string, unknown>)[nt.key]
      initial[nt.key] = val !== undefined ? (val as boolean) : true
    }
    setToggles(initial)
    setInitialized(true)
  }

  const updateMutation = useMutation({
    mutationFn: async (body: Record<string, unknown>) => {
      if (!api) throw new Error('Not authenticated')
      await api.PUT('/v1/users/me/settings', {
        body: { notification_preferences: body },
      } as never)
    },
    onSuccess: () => {
      toast.success('Notification preferences updated')
      queryClient.invalidateQueries({
        queryKey: ['notifications', 'settings'],
      })
    },
    onError: () => {
      toast.error('Failed to update notification preferences')
    },
  })

  const handleToggle = (key: string) => {
    const next = { ...toggles, [key]: !toggles[key] }
    setToggles(next)
    updateMutation.mutate(next)
  }

  const handleSaveAll = () => {
    updateMutation.mutate(toggles)
  }

  return (
    <div className="space-y-6">
      <SectionHead
        title="Notification Preferences"
        eyebrow="Choose which notifications you want to receive"
      />

      <Banner type="info" id="notifications-info">
        You'll receive these notifications in-app and via email (if configured).
      </Banner>

      <Card>
        <CardHeader>
          <CardTitle>Notification Types</CardTitle>
          <CardDescription>
            Toggle individual notification categories on or off
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          {NOTIFICATION_TYPES.map((nt) => (
            <div
              key={nt.key}
              className="flex items-center justify-between border-b border-sf-line/50 pb-4 last:border-0 last:pb-0"
            >
              <div className="flex items-center gap-3">
                <div className="flex size-9 items-center justify-center rounded-full bg-sf-panel-2">
                  <nt.icon className="size-4 text-sf-ink-muted" />
                </div>
                <div>
                  <Label>{nt.label}</Label>
                  <p className="mt-0.5 text-xs text-sf-ink-faint">
                    {nt.description}
                  </p>
                </div>
              </div>
              <button
                type="button"
                role="switch"
                aria-checked={toggles[nt.key] ?? true}
                onClick={() => handleToggle(nt.key)}
                className={cn(
                  'relative inline-flex h-6 w-11 shrink-0 cursor-pointer items-center rounded-full transition-colors',
                  (toggles[nt.key] ?? true) ? 'bg-sf-accent' : 'bg-sf-panel-2'
                )}
              >
                <span
                  className={cn(
                    'inline-block size-4 rounded-full bg-white shadow-sm transition-transform',
                    (toggles[nt.key] ?? true)
                      ? 'translate-x-6'
                      : 'translate-x-1'
                  )}
                />
              </button>
            </div>
          ))}
        </CardContent>
        <CardFooter>
          <Button
            variant="primary"
            size="sm"
            onClick={handleSaveAll}
            loading={updateMutation.isPending}
          >
            <Save className="size-4" />
            Save Preferences
          </Button>
        </CardFooter>
      </Card>
    </div>
  )
}
