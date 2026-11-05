import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { Eye, EyeOff, Filter, Shield } from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'
import { useApi } from '@/api/client'
import {
  Banner,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  Label,
  SectionHead,
} from '@/components/ui'
import { cn } from '@/lib/utils'

export const Route = createFileRoute('/settings/filters')({
  component: FiltersPage,
})

function FiltersPage() {
  const api = useApi()
  const queryClient = useQueryClient()

  const { data: settings } = useQuery({
    queryKey: ['settings', 'preferences'],
    queryFn: async () => {
      if (!api) return null
      const res = await api.GET('/v1/users/me/settings')
      return res.data as unknown as Record<string, unknown> | null
    },
    enabled: !!api,
  })

  const [hideSpoilers, setHideSpoilers] = useState(
    (settings?.hide_spoilers as boolean) ?? false
  )
  const [showMature, setShowMature] = useState(
    (settings?.show_explicit as boolean) ?? true
  )
  const [autoPlayTrailers, setAutoPlayTrailers] = useState(
    (settings?.auto_play_trailers as boolean) ?? true
  )
  const [showAdultSuggestions, setShowAdultSuggestions] = useState(
    (settings?.show_adult_suggestions as boolean) ?? false
  )

  const updateSettingsMutation = useMutation({
    mutationFn: async (body: Record<string, unknown>) => {
      if (!api) throw new Error('Not authenticated')
      await api.PUT('/v1/users/me/settings', {
        body,
      } as never)
    },
    onSuccess: () => {
      toast.success('Filters updated')
      queryClient.invalidateQueries({ queryKey: ['settings', 'preferences'] })
    },
    onError: () => {
      toast.error('Failed to update filters')
    },
  })

  const toggleSetting = (key: string, current: boolean) => {
    const next = !current
    switch (key) {
      case 'hide_spoilers':
        setHideSpoilers(next)
        break
      case 'show_explicit':
        setShowMature(next)
        break
      case 'auto_play_trailers':
        setAutoPlayTrailers(next)
        break
      case 'show_adult_suggestions':
        setShowAdultSuggestions(next)
        break
    }
    updateSettingsMutation.mutate({ [key]: next })
  }

  return (
    <div className="space-y-6">
      <SectionHead
        title="Content Filters"
        eyebrow="Control what content you see"
      />

      <Banner type="info" id="filters-info">
        These settings affect how content is displayed across the entire
        platform.
      </Banner>

      <Card>
        <CardHeader>
          <CardTitle>Content Visibility</CardTitle>
          <CardDescription>
            Choose what types of content are visible to you
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-6">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-3">
              <EyeOff className="size-4 text-sf-ink-muted" />
              <div>
                <Label>Hide Spoilers</Label>
                <p className="mt-0.5 text-xs text-sf-ink-muted">
                  Blur content that may contain spoilers
                </p>
              </div>
            </div>
            <button
              type="button"
              role="switch"
              aria-checked={hideSpoilers}
              onClick={() => toggleSetting('hide_spoilers', hideSpoilers)}
              className={cn(
                'relative inline-flex h-6 w-11 shrink-0 cursor-pointer items-center rounded-full transition-colors',
                hideSpoilers ? 'bg-sf-accent' : 'bg-sf-panel-2'
              )}
            >
              <span
                className={cn(
                  'inline-block size-4 rounded-full bg-white shadow-sm transition-transform',
                  hideSpoilers ? 'translate-x-6' : 'translate-x-1'
                )}
              />
            </button>
          </div>

          <div className="flex items-center justify-between">
            <div className="flex items-center gap-3">
              <Shield className="size-4 text-sf-ink-muted" />
              <div>
                <Label>Show Mature Content</Label>
                <p className="mt-0.5 text-xs text-sf-ink-muted">
                  Display content rated for mature audiences
                </p>
              </div>
            </div>
            <button
              type="button"
              role="switch"
              aria-checked={showMature}
              onClick={() => toggleSetting('show_explicit', showMature)}
              className={cn(
                'relative inline-flex h-6 w-11 shrink-0 cursor-pointer items-center rounded-full transition-colors',
                showMature ? 'bg-sf-accent' : 'bg-sf-panel-2'
              )}
            >
              <span
                className={cn(
                  'inline-block size-4 rounded-full bg-white shadow-sm transition-transform',
                  showMature ? 'translate-x-6' : 'translate-x-1'
                )}
              />
            </button>
          </div>

          <div className="flex items-center justify-between">
            <div className="flex items-center gap-3">
              <Eye className="size-4 text-sf-ink-muted" />
              <div>
                <Label>Auto-Play Trailers</Label>
                <p className="mt-0.5 text-xs text-sf-ink-muted">
                  Automatically play trailers on hover
                </p>
              </div>
            </div>
            <button
              type="button"
              role="switch"
              aria-checked={autoPlayTrailers}
              onClick={() =>
                toggleSetting('auto_play_trailers', autoPlayTrailers)
              }
              className={cn(
                'relative inline-flex h-6 w-11 shrink-0 cursor-pointer items-center rounded-full transition-colors',
                autoPlayTrailers ? 'bg-sf-accent' : 'bg-sf-panel-2'
              )}
            >
              <span
                className={cn(
                  'inline-block size-4 rounded-full bg-white shadow-sm transition-transform',
                  autoPlayTrailers ? 'translate-x-6' : 'translate-x-1'
                )}
              />
            </button>
          </div>

          <div className="flex items-center justify-between">
            <div className="flex items-center gap-3">
              <Filter className="size-4 text-sf-ink-muted" />
              <div>
                <Label>Show Adult Suggestions</Label>
                <p className="mt-0.5 text-xs text-sf-ink-muted">
                  Include adult content in recommendations
                </p>
              </div>
            </div>
            <button
              type="button"
              role="switch"
              aria-checked={showAdultSuggestions}
              onClick={() =>
                toggleSetting('show_adult_suggestions', showAdultSuggestions)
              }
              className={cn(
                'relative inline-flex h-6 w-11 shrink-0 cursor-pointer items-center rounded-full transition-colors',
                showAdultSuggestions ? 'bg-sf-accent' : 'bg-sf-panel-2'
              )}
            >
              <span
                className={cn(
                  'inline-block size-4 rounded-full bg-white shadow-sm transition-transform',
                  showAdultSuggestions ? 'translate-x-6' : 'translate-x-1'
                )}
              />
            </button>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
