import { useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import {
  Webhook,
  Trash2,
  Copy,
  Check,
  RefreshCw,
  Send,
  Globe,
  Activity,
} from 'lucide-react'
import { useApi } from '@/api/client'
import {
  Button,
  Input,
  Card,
  CardHeader,
  CardTitle,
  CardContent,
  CardFooter,
  SectionHead,
  Label,
  Badge,
  Banner,
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/ui'
import { cn } from '@/lib/utils'

export const Route = createFileRoute('/settings/webhooks')({
  component: WebhooksPage,
})

const WEBHOOK_EVENTS = [
  'history.added',
  'watchlist.updated',
  'scrobble',
  'checkin',
  'rating',
  'favorite',
  'list.created',
  'list.updated',
  'collection.added',
  'collection.removed',
]

function WebhooksPage() {
  const api = useApi()
  const queryClient = useQueryClient()

  const { data: webhooks, isLoading } = useQuery({
    queryKey: ['webhooks'],
    queryFn: async () => {
      if (!api) return []
      const res = await api.GET('/v1/webhooks')
      return (res.data as unknown as unknown[]) ?? []
    },
    enabled: !!api,
  })

  const [dialogOpen, setDialogOpen] = useState(false)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [formUrl, setFormUrl] = useState('')
  const [formEvents, setFormEvents] = useState<string[]>(['history.added'])
  const [formActive, setFormActive] = useState(true)
  const [generatedSecret, setGeneratedSecret] = useState('')
  const [deleteId, setDeleteId] = useState<string | null>(null)
  const [deliveryLogId, setDeliveryLogId] = useState<string | null>(null)
  const [copiedId, setCopiedId] = useState<string | null>(null)

  const createMutation = useMutation({
    mutationFn: async () => {
      if (!api) throw new Error('Not authenticated')
      const res = await api.POST('/v1/webhooks', {
        body: { url: formUrl, events: formEvents, active: formActive },
      } as never)
      return (res.data as unknown) as Record<string, unknown>
    },
    onSuccess: (data) => {
      const secret = (data?.secret as string) ?? ''
      setGeneratedSecret(secret)
      toast.success('Webhook created')
      queryClient.invalidateQueries({ queryKey: ['webhooks'] })
    },
    onError: () => {
      toast.error('Failed to create webhook')
    },
  })

  const updateMutation = useMutation({
    mutationFn: async (id: string) => {
      if (!api) throw new Error('Not authenticated')
      await api.PUT('/v1/webhooks/{id}', {
        params: { path: { id } },
        body: { url: formUrl, events: formEvents, active: formActive },
      } as never)
    },
    onSuccess: () => {
      toast.success('Webhook updated')
      queryClient.invalidateQueries({ queryKey: ['webhooks'] })
      setDialogOpen(false)
      setEditingId(null)
    },
    onError: () => {
      toast.error('Failed to update webhook')
    },
  })

  const deleteMutation = useMutation({
    mutationFn: async (id: string) => {
      if (!api) throw new Error('Not authenticated')
      await api.DELETE('/v1/webhooks/{id}', {
        params: { path: { id } },
      } as never)
    },
    onSuccess: () => {
      toast.success('Webhook deleted')
      queryClient.invalidateQueries({ queryKey: ['webhooks'] })
      setDeleteId(null)
    },
    onError: () => {
      toast.error('Failed to delete webhook')
    },
  })

  const testPingMutation = useMutation({
    mutationFn: async (id: string) => {
      if (!api) throw new Error('Not authenticated')
      await api.POST('/v1/webhooks/{id}/test', {
        params: { path: { id } },
      } as never)
    },
    onSuccess: () => {
      toast.success('Test ping sent')
    },
    onError: () => {
      toast.error('Failed to send test ping')
    },
  })

  const rotateSecretMutation = useMutation({
    mutationFn: async (id: string) => {
      if (!api) throw new Error('Not authenticated')
      const res = await api.POST('/v1/webhooks/{id}/rotate-secret', {
        params: { path: { id } },
      } as never)
      return (res.data as unknown) as Record<string, unknown>
    },
    onSuccess: (data) => {
      const newSecret = (data?.secret as string) ?? ''
      setGeneratedSecret(newSecret)
      toast.success('Secret rotated — new secret shown')
      queryClient.invalidateQueries({ queryKey: ['webhooks'] })
    },
    onError: () => {
      toast.error('Failed to rotate secret')
    },
  })

  const { data: deliveries } = useQuery({
    queryKey: ['webhooks', 'deliveries', deliveryLogId],
    queryFn: async () => {
      if (!api || !deliveryLogId) return []
      const res = await api.GET('/v1/webhooks/{id}/deliveries', {
        params: { path: { id: deliveryLogId } },
      } as never)
      return (res.data as unknown as unknown[]) ?? []
    },
    enabled: !!api && !!deliveryLogId,
  })

  const handleCopy = (text: string, id: string) => {
    navigator.clipboard.writeText(text).then(() => {
      setCopiedId(id)
      toast.success('Copied to clipboard')
      setTimeout(() => setCopiedId(null), 2000)
    })
  }

  const openCreateDialog = () => {
    setEditingId(null)
    setFormUrl('')
    setFormEvents(['history.added'])
    setFormActive(true)
    setGeneratedSecret('')
    setDialogOpen(true)
  }

  const toggleEvent = (event: string) => {
    setFormEvents((prev) =>
      prev.includes(event) ? prev.filter((e) => e !== event) : [...prev, event]
    )
  }

  return (
    <div className="space-y-6">
      <SectionHead
        title="Webhooks"
        eyebrow="Manage webhook endpoints for real-time event notifications"
        action="Create Webhook"
        onAction={openCreateDialog}
      />

      {isLoading ? (
        <div className="space-y-3">
          {[...Array(2)].map((_, i) => (
            <Card key={i}>
              <CardContent>
                <div className="h-20 animate-pulse rounded bg-sf-panel-2" />
              </CardContent>
            </Card>
          ))}
        </div>
      ) : webhooks && (webhooks as unknown[]).length > 0 ? (
        <div className="space-y-3">
          {(webhooks as Record<string, unknown>[]).map((webhook: Record<string, unknown>) => {
            const id = (webhook.id as string) ?? ''
            const url = (webhook.url as string) ?? ''
            const events = (webhook.events as string[]) ?? []
            const active = (webhook.active as boolean) ?? true

            return (
              <Card key={id}>
                <CardHeader>
                  <div className="flex items-center justify-between">
                    <CardTitle className="flex items-center gap-2">
                      <Webhook className="size-4 text-sf-ink-muted" />
                      <span className="truncate text-sm">{url}</span>
                    </CardTitle>
                    <Badge variant={active ? 'default' : 'secondary'}>
                      {active ? 'Active' : 'Disabled'}
                    </Badge>
                  </div>
                </CardHeader>
                <CardContent>
                  <div className="flex flex-wrap gap-1.5">
                    {events.map((event: string) => (
                      <Badge key={event} variant="secondary">
                        {event}
                      </Badge>
                    ))}
                  </div>
                </CardContent>
                <CardFooter className="flex-wrap gap-2">
                  <Button
                    variant="glass"
                    size="sm"
                    onClick={() => {
                      setEditingId(id)
                      setFormUrl(url)
                      setFormEvents(events)
                      setFormActive(active)
                      setGeneratedSecret('')
                      setDialogOpen(true)
                    }}
                  >
                    Edit
                  </Button>
                  <Button
                    variant="glass"
                    size="sm"
                    onClick={() => testPingMutation.mutate(id)}
                    loading={testPingMutation.isPending && testPingMutation.variables === id}
                  >
                    <Send className="size-3" />
                    Test Ping
                  </Button>
                  <Button
                    variant="glass"
                    size="sm"
                    onClick={() =>
                      setDeliveryLogId(deliveryLogId === id ? null : id)
                    }
                  >
                    <Activity className="size-3" />
                    {deliveryLogId === id ? 'Hide Log' : 'Delivery Log'}
                  </Button>
                  <Button
                    variant="glass"
                    size="sm"
                    onClick={() => rotateSecretMutation.mutate(id)}
                    loading={rotateSecretMutation.isPending && rotateSecretMutation.variables === id}
                  >
                    <RefreshCw className="size-3" />
                    Rotate Secret
                  </Button>
                  <Button
                    variant="danger"
                    size="sm"
                    onClick={() => setDeleteId(id)}
                  >
                    <Trash2 className="size-3" />
                    Delete
                  </Button>
                </CardFooter>

                {deliveryLogId === id && (
                  <CardContent className="mt-4 border-t border-sf-line pt-4">
                    <h4 className="mb-2 text-sm font-semibold text-sf-ink">
                      Delivery Log
                    </h4>
                    {deliveries && (deliveries as unknown[]).length > 0 ? (
                      <div className="overflow-x-auto">
                        <table className="w-full text-xs">
                          <thead>
                            <tr className="text-sf-ink-muted">
                              <th className="pb-2 pr-3 text-left">Event Type</th>
                              <th className="pb-2 pr-3 text-left">Status</th>
                              <th className="pb-2 pr-3 text-left">Attempts</th>
                              <th className="pb-2 pr-3 text-left">Response</th>
                              <th className="pb-2 pr-3 text-left">Duration</th>
                              <th className="pb-2 text-left">Time</th>
                            </tr>
                          </thead>
                          <tbody>
                            {(deliveries as Record<string, unknown>[]).map(
                              (d: Record<string, unknown>, i: number) => (
                                <tr key={i} className="border-t border-sf-line/50">
                                  <td className="py-2 pr-3 text-sf-ink">
                                    {(d.event_type as string) ?? '-'}
                                  </td>
                                  <td className="py-2 pr-3">
                                    <Badge
                                      variant={
                                        d.status === 'success'
                                          ? 'default'
                                          : 'destructive'
                                      }
                                    >
                                      {(d.status as string) ?? '-'}
                                    </Badge>
                                  </td>
                                  <td className="py-2 pr-3 text-sf-ink-muted">
                                    {(d.attempts as number) ?? '-'}
                                  </td>
                                  <td className="py-2 pr-3 text-sf-ink-muted">
                                    {(d.response_code as number) ?? '-'}
                                  </td>
                                  <td className="py-2 pr-3 text-sf-ink-muted">
                                    {(d.duration_ms as number)
                                      ? `${d.duration_ms}ms`
                                      : '-'}
                                  </td>
                                  <td className="py-2 text-sf-ink-muted">
                                    {d.timestamp
                                      ? new Date(
                                          d.timestamp as string
                                        ).toLocaleString()
                                      : '-'}
                                  </td>
                                </tr>
                              )
                            )}
                          </tbody>
                        </table>
                      </div>
                    ) : (
                      <p className="text-xs text-sf-ink-muted">
                        No deliveries recorded yet.
                      </p>
                    )}
                  </CardContent>
                )}
              </Card>
            )
          })}
        </div>
      ) : (
        <Card>
          <CardContent className="py-8 text-center">
            <Globe className="mx-auto size-8 text-sf-ink-hint" />
            <p className="mt-2 text-sm text-sf-ink-muted">
              No webhook endpoints yet. Create one to receive real-time event
              notifications.
            </p>
          </CardContent>
        </Card>
      )}

      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>
              {editingId ? 'Edit Webhook' : 'Create Webhook'}
            </DialogTitle>
            <DialogDescription>
              Configure a webhook endpoint to receive event notifications.
            </DialogDescription>
          </DialogHeader>

          {generatedSecret ? (
            <div className="space-y-4">
              <Banner type="info" id="webhook-secret-shown">
                Secret will only be shown ONCE. Store it securely.
              </Banner>
              <div className="rounded-lg bg-sf-panel-2 p-4">
                <p className="break-all font-mono text-sm text-sf-accent">
                  {generatedSecret}
                </p>
              </div>
              <Button
                variant="primary"
                size="sm"
                className="w-full"
                onClick={() => handleCopy(generatedSecret, 'webhook-secret')}
              >
                {copiedId === 'webhook-secret' ? (
                  <Check className="size-4" />
                ) : (
                  <Copy className="size-4" />
                )}
                Copy to Clipboard
              </Button>
              <Button
                variant="ghost"
                size="sm"
                className="w-full"
                onClick={() => {
                  setDialogOpen(false)
                  setGeneratedSecret('')
                  setEditingId(null)
                }}
              >
                Done
              </Button>
            </div>
          ) : (
            <div className="space-y-4">
              <div>
                <Label>URL</Label>
                <Input
                  placeholder="https://example.com/webhook"
                  value={formUrl}
                  onChange={(e) => setFormUrl(e.target.value)}
                  className="mt-1.5"
                />
              </div>

              <div>
                <Label>Events</Label>
                <div className="mt-1.5 flex flex-wrap gap-1.5">
                  {WEBHOOK_EVENTS.map((event) => (
                    <button
                      key={event}
                      type="button"
                      onClick={() => toggleEvent(event)}
                      className={cn(
                        'rounded-full px-3 py-1 text-xs font-medium transition-colors',
                        formEvents.includes(event)
                          ? 'bg-sf-accent text-white'
                          : 'bg-sf-panel-2 text-sf-ink-muted hover:bg-sf-panel'
                      )}
                    >
                      {event}
                    </button>
                  ))}
                </div>
              </div>

              <div className="flex items-center justify-between">
                <Label>Active</Label>
                <button
                  type="button"
                  role="switch"
                  aria-checked={formActive}
                  onClick={() => setFormActive(!formActive)}
                  className={cn(
                    'relative inline-flex h-6 w-11 shrink-0 cursor-pointer items-center rounded-full transition-colors',
                    formActive ? 'bg-sf-accent' : 'bg-sf-panel-2'
                  )}
                >
                  <span
                    className={cn(
                      'inline-block size-4 rounded-full bg-white shadow-sm transition-transform',
                      formActive ? 'translate-x-6' : 'translate-x-1'
                    )}
                  />
                </button>
              </div>

              <DialogFooter>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => {
                    setDialogOpen(false)
                    setEditingId(null)
                  }}
                >
                  Cancel
                </Button>
                <Button
                  variant="primary"
                  size="sm"
                  disabled={!formUrl.trim() || formEvents.length === 0}
                  onClick={() =>
                    editingId
                      ? updateMutation.mutate(editingId)
                      : createMutation.mutate()
                  }
                  loading={
                    editingId
                      ? updateMutation.isPending
                      : createMutation.isPending
                  }
                >
                  {editingId ? 'Update' : 'Create'}
                </Button>
              </DialogFooter>
            </div>
          )}
        </DialogContent>
      </Dialog>

      <Dialog
        open={!!deleteId}
        onOpenChange={(open) => {
          if (!open) setDeleteId(null)
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete Webhook</DialogTitle>
            <DialogDescription>
              This action cannot be undone. The endpoint will stop receiving
              events.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => setDeleteId(null)}
            >
              Cancel
            </Button>
            <Button
              variant="danger"
              size="sm"
              onClick={() => deleteId && deleteMutation.mutate(deleteId)}
              loading={deleteMutation.isPending}
            >
              <Trash2 className="size-4" />
              Delete Webhook
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}