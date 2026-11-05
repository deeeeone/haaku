import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import {
  Check,
  Copy,
  Eye,
  EyeOff,
  Key,
  Plus,
  Shield,
  Trash2,
} from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'
import { useApi } from '@/api/client'
import {
  Badge,
  Banner,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  Input,
  Label,
  SectionHead,
} from '@/components/ui'

export const Route = createFileRoute('/settings/apikeys')({
  component: ApiKeysPage,
})

function ApiKeysPage() {
  const api = useApi()
  const queryClient = useQueryClient()

  const { data: keys, isLoading } = useQuery({
    queryKey: ['apikeys'],
    queryFn: async () => {
      if (!api) return []
      const res = await api.GET('/v1/users/me/api_keys')
      return (res.data as unknown as unknown[]) ?? []
    },
    enabled: !!api,
  })

  const [createOpen, setCreateOpen] = useState(false)
  const [newKeyName, setNewKeyName] = useState('')
  const [generatedKey, setGeneratedKey] = useState('')
  const [revokeId, setRevokeId] = useState<string | null>(null)
  const [copiedId, setCopiedId] = useState<string | null>(null)
  const [revealedKeys, setRevealedKeys] = useState<Set<string>>(new Set())

  const createMutation = useMutation({
    mutationFn: async () => {
      if (!api) throw new Error('Not authenticated')
      const res = await api.POST('/v1/users/me/api_keys', {
        body: { name: newKeyName },
      } as never)
      return res.data as unknown as Record<string, unknown>
    },
    onSuccess: (data) => {
      setGeneratedKey((data?.key as string) ?? (data?.token as string) ?? '')
      queryClient.invalidateQueries({ queryKey: ['apikeys'] })
    },
    onError: () => {
      toast.error('Failed to create API key')
    },
  })

  const revokeMutation = useMutation({
    mutationFn: async (keyId: string) => {
      if (!api) throw new Error('Not authenticated')
      await api.DELETE('/v1/users/me/api_keys/{key_id}', {
        params: { path: { key_id: keyId } },
      } as never)
    },
    onSuccess: () => {
      toast.success('API key revoked')
      queryClient.invalidateQueries({ queryKey: ['apikeys'] })
      setRevokeId(null)
    },
    onError: () => {
      toast.error('Failed to revoke API key')
    },
  })

  const handleCopy = (text: string, id: string) => {
    navigator.clipboard.writeText(text).then(() => {
      setCopiedId(id)
      toast.success('Copied to clipboard')
      setTimeout(() => setCopiedId(null), 2000)
    })
  }

  const maskKey = (key: string): string => {
    if (key.length <= 8) return key
    return `${key.slice(0, 4)}****${key.slice(-4)}`
  }

  return (
    <div className="space-y-6">
      <SectionHead
        title="API Keys"
        eyebrow="Manage your API keys for programmatic access"
        action="Generate New Key"
        onAction={() => {
          setNewKeyName('')
          setGeneratedKey('')
          setCreateOpen(true)
        }}
      />

      <Banner type="info" id="apikey-warning">
        Store your API keys securely. They cannot be retrieved after creation.
      </Banner>

      {isLoading ? (
        <div className="space-y-3">
          {[...Array(2)].map((_, i) => (
            <Card key={i}>
              <CardContent>
                <div className="h-16 animate-pulse rounded bg-sf-panel-2" />
              </CardContent>
            </Card>
          ))}
        </div>
      ) : keys && (keys as unknown[]).length > 0 ? (
        <div className="space-y-3">
          {(keys as Record<string, unknown>[]).map(
            (key: Record<string, unknown>) => {
              const id = (key.id as string) ?? ''
              const name = (key.name as string) ?? 'Unnamed Key'
              const maskedKey =
                (key.key as string) ?? (key.masked_key as string) ?? ''
              const createdAt = (key.created_at as string) ?? ''
              const isRevealed = revealedKeys.has(id)

              return (
                <Card key={id}>
                  <CardHeader>
                    <div className="flex items-center justify-between">
                      <CardTitle className="flex items-center gap-2">
                        <Key className="size-4 text-sf-ink-muted" />
                        {name}
                      </CardTitle>
                      {createdAt && (
                        <Badge variant="secondary">
                          {new Date(createdAt).toLocaleDateString()}
                        </Badge>
                      )}
                    </div>
                    <CardDescription className="font-mono">
                      {isRevealed ? maskedKey : maskKey(maskedKey)}
                    </CardDescription>
                  </CardHeader>
                  <CardFooter className="gap-2">
                    <Button
                      variant="glass"
                      size="sm"
                      onClick={() => handleCopy(maskedKey, id)}
                    >
                      {copiedId === id ? (
                        <Check className="size-3" />
                      ) : (
                        <Copy className="size-3" />
                      )}
                      Copy
                    </Button>
                    <Button
                      variant="glass"
                      size="sm"
                      onClick={() => {
                        const next = new Set(revealedKeys)
                        if (isRevealed) next.delete(id)
                        else next.add(id)
                        setRevealedKeys(next)
                      }}
                    >
                      {isRevealed ? (
                        <EyeOff className="size-3" />
                      ) : (
                        <Eye className="size-3" />
                      )}
                      {isRevealed ? 'Hide' : 'Reveal'}
                    </Button>
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
            }
          )}
        </div>
      ) : (
        <Card>
          <CardContent className="py-8 text-center">
            <Shield className="mx-auto size-8 text-sf-ink-hint" />
            <p className="mt-2 text-sm text-sf-ink-muted">
              No API keys yet. Generate one to access the API programmatically.
            </p>
          </CardContent>
        </Card>
      )}

      {/* Create API Key Dialog */}
      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Generate New API Key</DialogTitle>
            <DialogDescription>
              Give your key a name to remember its purpose.
            </DialogDescription>
          </DialogHeader>

          {generatedKey ? (
            <div className="space-y-4">
              <Banner type="info" id="key-shown-once">
                Copy this key now — it will only be shown once!
              </Banner>
              <div className="rounded-lg bg-sf-panel-2 p-4">
                <p className="break-all font-mono text-sm text-sf-accent">
                  {generatedKey}
                </p>
              </div>
              <Button
                variant="primary"
                size="sm"
                className="w-full"
                onClick={() => handleCopy(generatedKey, 'generated')}
              >
                {copiedId === 'generated' ? (
                  <Check className="size-4" />
                ) : (
                  <Copy className="size-4" />
                )}
                Copy to Clipboard
              </Button>
            </div>
          ) : (
            <div className="space-y-4">
              <div>
                <Label>Key Name</Label>
                <Input
                  placeholder="e.g. My App, Home Server"
                  value={newKeyName}
                  onChange={(e) => setNewKeyName(e.target.value)}
                  className="mt-1.5"
                />
              </div>
              <DialogFooter>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => setCreateOpen(false)}
                >
                  Cancel
                </Button>
                <Button
                  variant="primary"
                  size="sm"
                  disabled={!newKeyName.trim()}
                  onClick={() => createMutation.mutate()}
                  loading={createMutation.isPending}
                >
                  <Plus className="size-4" />
                  Generate
                </Button>
              </DialogFooter>
            </div>
          )}
        </DialogContent>
      </Dialog>

      {/* Revoke Confirmation Dialog */}
      <Dialog
        open={!!revokeId}
        onOpenChange={(open) => {
          if (!open) setRevokeId(null)
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Revoke API Key</DialogTitle>
            <DialogDescription>
              This action cannot be undone. Any applications using this key will
              stop working.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="ghost" size="sm" onClick={() => setRevokeId(null)}>
              Cancel
            </Button>
            <Button
              variant="danger"
              size="sm"
              onClick={() => revokeId && revokeMutation.mutate(revokeId)}
              loading={revokeMutation.isPending}
            >
              <Trash2 className="size-4" />
              Revoke Key
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
