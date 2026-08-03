import { useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import {
  Puzzle,
  Trash2,
  Globe,
  Shield,
} from 'lucide-react'
import { useApi } from '@/api/client'
import {
  Button,
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
  CardFooter,
  SectionHead,
  Badge,
  Banner,
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/ui'

export const Route = createFileRoute('/settings/integrations')({
  component: IntegrationsPage,
})

function IntegrationsPage() {
  const api = useApi()
  const queryClient = useQueryClient()

  const { data: tokens } = useQuery({
    queryKey: ['integrations'],
    queryFn: async () => {
      if (!api) return []
      try {
        const res = await api.GET('/oauth/applications' as never)
        return (res.data as unknown as unknown[]) ?? []
      } catch {
        return []
      }
    },
    enabled: !!api,
  })

  const [revokeId, setRevokeId] = useState<string | null>(null)

  const revokeMutation = useMutation({
    mutationFn: async (tokenId: string) => {
      if (!api) throw new Error('Not authenticated')
      await api.POST('/oauth/revoke', {
        body: { token_id: tokenId },
      } as never)
    },
    onSuccess: () => {
      toast.success('Integration revoked')
      queryClient.invalidateQueries({ queryKey: ['integrations'] })
      setRevokeId(null)
    },
    onError: () => {
      toast.error('Failed to revoke integration')
    },
  })

  const connectedApps = [
    {
      id: 'trakt-web',
      name: 'Trakt Web',
      description: 'Official Trakt web application',
      icon: Globe,
      lastUsed: 'Now',
      scopes: ['read', 'write'],
    },
    {
      id: 'trakt-ios',
      name: 'Trakt iOS',
      description: 'Official iOS application',
      icon: Shield,
      lastUsed: '2 days ago',
      scopes: ['read', 'write', 'sync'],
    },
  ]

  return (
    <div className="space-y-6">
      <SectionHead
        title="Integrations"
        eyebrow="Manage connected applications and tokens"
      />

      <Banner type="info" id="integrations-info">
        Revoking access will immediately disconnect the application from your
        account.
      </Banner>

      <div className="space-y-4">
        {connectedApps.map((app) => (
          <Card key={app.id}>
            <CardHeader>
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-3">
                  <div className="flex size-10 items-center justify-center rounded-full bg-sf-panel-2">
                    <app.icon className="size-5 text-sf-ink-muted" />
                  </div>
                  <div>
                    <CardTitle className="text-base">{app.name}</CardTitle>
                    <CardDescription>{app.description}</CardDescription>
                  </div>
                </div>
                <Badge variant="secondary">
                  Last used: {app.lastUsed}
                </Badge>
              </div>
            </CardHeader>
            <CardContent>
              <div className="flex flex-wrap gap-1.5">
                {app.scopes.map((scope) => (
                  <Badge key={scope} variant="secondary">
                    {scope}
                  </Badge>
                ))}
              </div>
            </CardContent>
            <CardFooter>
              <Button
                variant="danger"
                size="sm"
                onClick={() => setRevokeId(app.id)}
              >
                <Trash2 className="size-3" />
                Revoke Access
              </Button>
            </CardFooter>
          </Card>
        ))}
      </div>

      {tokens && (tokens as unknown[]).length > 0 && (
        <div>
          <SectionHead
            title="OAuth Tokens"
            eyebrow="Active OAuth tokens for third-party apps"
          />
          <div className="space-y-3">
            {(tokens as Record<string, unknown>[]).map((token: Record<string, unknown>, i: number) => {
              const id = (token.id as string) ?? `token-${i}`
              const appName = (token.application as string) ?? (token.name as string) ?? 'Unknown App'
              const createdAt = (token.created_at as string) ?? ''
              return (
                <Card key={id}>
                  <CardHeader>
                    <div className="flex items-center justify-between">
                      <CardTitle className="text-base">
                        {appName}
                      </CardTitle>
                      {createdAt && (
                        <Badge variant="secondary">
                          {new Date(createdAt).toLocaleDateString()}
                        </Badge>
                      )}
                    </div>
                  </CardHeader>
                  <CardFooter>
                    <Button
                      variant="danger"
                      size="sm"
                      onClick={() => setRevokeId(id)}
                    >
                      <Trash2 className="size-3" />
                      Revoke
                    </Button>
                  </CardFooter>
                </Card>
              )
            })}
          </div>
        </div>
      )}

      {(!tokens || (tokens as unknown[]).length === 0) && connectedApps.length === 0 && (
        <Card>
          <CardContent className="py-8 text-center">
            <Puzzle className="mx-auto size-8 text-sf-ink-hint" />
            <p className="mt-2 text-sm text-sf-ink-muted">
              No connected applications yet. Third-party apps you authorize will
              appear here.
            </p>
          </CardContent>
        </Card>
      )}

      <Dialog
        open={!!revokeId}
        onOpenChange={(open) => {
          if (!open) setRevokeId(null)
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Revoke Access</DialogTitle>
            <DialogDescription>
              This application will immediately lose access to your account. You
              may need to re-authorize it to use it again.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => setRevokeId(null)}
            >
              Cancel
            </Button>
            <Button
              variant="danger"
              size="sm"
              onClick={() => revokeId && revokeMutation.mutate(revokeId)}
              loading={revokeMutation.isPending}
            >
              <Trash2 className="size-4" />
              Revoke Access
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}