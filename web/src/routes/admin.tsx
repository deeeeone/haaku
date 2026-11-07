import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import {
  Ban,
  Check,
  Copy,
  KeyRound,
  LockOpen,
  ShieldAlert,
  ShieldCheck,
  UserCheck,
} from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'
import {
  type AdminAction,
  type AdminResetToken,
  type AdminUser,
  issuePasswordReset,
  listUsers,
  userAction,
} from '@/api/admin'
import { ApiError } from '@/api/fetch'
import { useAuth } from '@/auth/context'
import { AuthNotice, AuthShell } from '@/components/auth/auth-shell'
import {
  Badge,
  Button,
  Card,
  CardContent,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  Label,
  SectionHead,
} from '@/components/ui'
import { formatDate } from '@/lib/utils'

export const Route = createFileRoute('/admin')({
  component: AdminPage,
})

const PAGE_SIZE = 25
const STATUS_FILTERS = ['', 'pending', 'active', 'banned', 'locked'] as const

function statusVariant(status: string) {
  if (status === 'active') return 'default' as const
  if (status === 'pending') return 'outline' as const
  return 'destructive' as const
}

function AdminPage() {
  const { mode, isAdmin, getToken } = useAuth()

  if (mode !== 'local') {
    return (
      <AuthShell title="Admin">
        <AuthNotice tone="info" title="Local mode only">
          The admin panel manages local accounts, which this instance does not
          use.
        </AuthNotice>
      </AuthShell>
    )
  }
  if (!isAdmin) {
    return (
      <AuthShell title="Admin">
        <AuthNotice tone="error" title="Not authorized">
          You need administrator access to view this page.
        </AuthNotice>
      </AuthShell>
    )
  }
  return <AdminPanel getToken={getToken} />
}

function AdminPanel({ getToken }: { getToken: () => Promise<string | null> }) {
  const queryClient = useQueryClient()
  const [status, setStatus] = useState('')
  const [offset, setOffset] = useState(0)
  const [resetResult, setResetResult] = useState<AdminResetToken | null>(null)

  const usersQuery = useQuery({
    queryKey: ['admin', 'users', { status, offset }],
    queryFn: async () =>
      listUsers(await getToken(), {
        limit: PAGE_SIZE,
        offset,
        status: status || undefined,
      }),
    retry: false,
  })

  const actionMutation = useMutation({
    mutationFn: async (vars: { id: string; action: AdminAction }) =>
      userAction(await getToken(), vars.id, vars.action),
    onSuccess: (updated) => {
      queryClient.invalidateQueries({ queryKey: ['admin', 'users'] })
      toast.success(`Account is now ${updated.status}`)
    },
    onError: (error) => {
      toast.error(adminErrorMessage(error))
    },
  })

  const resetMutation = useMutation({
    mutationFn: async (id: string) => issuePasswordReset(await getToken(), id),
    onSuccess: (data) => setResetResult(data),
    onError: (error) => toast.error(adminErrorMessage(error)),
  })

  const forbidden =
    usersQuery.error instanceof ApiError && usersQuery.error.status === 403

  if (forbidden) {
    return (
      <AuthShell title="Admin">
        <AuthNotice tone="error" title="Not authorized">
          The server refused this request — administrator access is required.
        </AuthNotice>
      </AuthShell>
    )
  }

  const data = usersQuery.data
  const users = data?.users ?? []
  const total = data?.total ?? 0

  return (
    <div className="space-y-6 px-4 py-8 lg:px-8">
      <SectionHead title="Admin — users" />

      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-2">
          <Label htmlFor="status-filter">Status</Label>
          <select
            id="status-filter"
            value={status}
            onChange={(e) => {
              setStatus(e.target.value)
              setOffset(0)
            }}
            className="glass rounded-full px-3 py-2 text-sm text-sf-ink outline-none"
          >
            {STATUS_FILTERS.map((value) => (
              <option key={value || 'all'} value={value}>
                {value || 'All'}
              </option>
            ))}
          </select>
        </div>
        <p className="text-sm text-sf-ink-muted">
          {total} {total === 1 ? 'account' : 'accounts'}
        </p>
      </div>

      <Card spotlight={false}>
        <CardContent className="px-0 pt-2">
          {usersQuery.isLoading ? (
            <p className="px-6 py-4 text-sm text-sf-ink-muted">Loading…</p>
          ) : usersQuery.isError ? (
            <p className="px-6 py-4 text-sm text-[#ff7a82]">
              Could not load users. {adminErrorMessage(usersQuery.error)}
            </p>
          ) : users.length === 0 ? (
            <p className="px-6 py-4 text-sm text-sf-ink-muted">
              No accounts match this filter.
            </p>
          ) : (
            <div className="overflow-x-auto">
              <UsersTable
                users={users}
                busy={actionMutation.isPending}
                onAction={(id, action) => actionMutation.mutate({ id, action })}
                onReset={(id) => resetMutation.mutate(id)}
              />
            </div>
          )}
        </CardContent>
      </Card>

      <div className="flex items-center justify-between">
        <Button
          variant="glass"
          size="sm"
          disabled={offset === 0}
          onClick={() => setOffset((o) => Math.max(0, o - PAGE_SIZE))}
        >
          Previous
        </Button>
        <Button
          variant="glass"
          size="sm"
          disabled={offset + PAGE_SIZE >= total}
          onClick={() => setOffset((o) => o + PAGE_SIZE)}
        >
          Next
        </Button>
      </div>

      <ResetTokenDialog
        result={resetResult}
        onClose={() => setResetResult(null)}
      />
    </div>
  )
}

function UsersTable({
  users,
  busy,
  onAction,
  onReset,
}: {
  users: AdminUser[]
  busy: boolean
  onAction: (id: string, action: AdminAction) => void
  onReset: (id: string) => void
}) {
  return (
    <table className="w-full min-w-[900px] border-collapse text-left text-sm">
      <thead>
        <tr className="text-xs uppercase tracking-wide text-sf-ink-hint">
          <th className="px-6 py-2 font-medium">ID</th>
          <th className="px-3 py-2 font-medium">Email</th>
          <th className="px-3 py-2 font-medium">Username</th>
          <th className="px-3 py-2 font-medium">Status</th>
          <th className="px-3 py-2 font-medium">Admin</th>
          <th className="px-3 py-2 font-medium">Created</th>
          <th className="px-3 py-2 font-medium">Last active</th>
          <th className="px-6 py-2 font-medium">Actions</th>
        </tr>
      </thead>
      <tbody>
        {users.map((user) => (
          <tr key={user.id} className="border-t border-sf-line">
            <td className="px-6 py-3 font-mono text-xs text-sf-ink-muted">
              {user.id}
            </td>
            <td className="px-3 py-3 text-sf-ink">{user.email ?? '—'}</td>
            <td className="px-3 py-3 text-sf-ink">{user.username ?? '—'}</td>
            <td className="px-3 py-3">
              <Badge variant={statusVariant(user.status)}>{user.status}</Badge>
            </td>
            <td className="px-3 py-3">
              {user.is_admin ? (
                <Badge variant="secondary">admin</Badge>
              ) : (
                <span className="text-sf-ink-faint">—</span>
              )}
            </td>
            <td className="px-3 py-3 text-sf-ink-muted">
              {user.created_at ? formatDate(user.created_at) : '—'}
            </td>
            <td className="px-3 py-3 text-sf-ink-muted">
              {user.last_active_at ? formatDate(user.last_active_at) : '—'}
            </td>
            <td className="px-6 py-3">
              <div className="flex flex-wrap gap-2">
                {user.status === 'pending' && (
                  <Button
                    size="sm"
                    variant="glass"
                    disabled={busy}
                    onClick={() => onAction(user.id, 'approve')}
                  >
                    <UserCheck className="size-3" />
                    Approve
                  </Button>
                )}
                {user.status === 'active' && (
                  <Button
                    size="sm"
                    variant="danger"
                    disabled={busy}
                    onClick={() => onAction(user.id, 'ban')}
                  >
                    <Ban className="size-3" />
                    Ban
                  </Button>
                )}
                {(user.status === 'banned' || user.status === 'locked') && (
                  <Button
                    size="sm"
                    variant="glass"
                    disabled={busy}
                    onClick={() => onAction(user.id, 'unlock')}
                  >
                    <LockOpen className="size-3" />
                    Unlock
                  </Button>
                )}
                <Button
                  size="sm"
                  variant="glass"
                  disabled={busy}
                  onClick={() => onReset(user.id)}
                >
                  <KeyRound className="size-3" />
                  Password reset
                </Button>
              </div>
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}

/**
 * Shows the one-time reset token and its `url_path`. It is displayed ONCE: the
 * token is stored hashed server-side and cannot be recovered, so the copy
 * affordance and the warning are the whole point of this dialog.
 */
function ResetTokenDialog({
  result,
  onClose,
}: {
  result: AdminResetToken | null
  onClose: () => void
}) {
  return (
    <Dialog open={result !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <ShieldCheck className="size-4 text-sf-accent-2" />
            Password reset link
          </DialogTitle>
          <DialogDescription>
            Hand this to the user. <strong>It is shown only once</strong> and
            cannot be recovered afterwards.
          </DialogDescription>
        </DialogHeader>

        {result ? (
          <div className="space-y-4">
            <CopyField label="Reset link" value={result.url_path} />
            <CopyField label="Token" value={result.token} />
            <div className="flex items-start gap-2 rounded-2xl border border-sf-brand-red/30 bg-sf-brand-red/10 px-4 py-3 text-sm text-[#ff7a82]">
              <ShieldAlert className="mt-0.5 size-4 shrink-0" />
              <p>
                This value is not recoverable. Copy it now — closing this dialog
                discards it, and you will need to reissue a new link.
              </p>
            </div>
          </div>
        ) : null}
      </DialogContent>
    </Dialog>
  )
}

function CopyField({ label, value }: { label: string; value: string }) {
  const [copied, setCopied] = useState(false)

  async function copy() {
    try {
      await navigator.clipboard.writeText(value)
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    } catch {
      toast.error('Could not copy to the clipboard')
    }
  }

  return (
    <div className="space-y-2">
      <Label>{label}</Label>
      <div className="flex items-center gap-2">
        <code className="glass min-w-0 flex-1 truncate rounded-2xl px-3 py-2 text-xs text-sf-ink">
          {value}
        </code>
        <Button size="sm" variant="glass" onClick={copy}>
          {copied ? <Check className="size-3" /> : <Copy className="size-3" />}
          {copied ? 'Copied' : 'Copy'}
        </Button>
      </div>
    </div>
  )
}

function adminErrorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.status === 403) return 'Administrator access is required.'
    if (error.status === 401) return 'Your session has expired — sign in again.'
    return error.message
  }
  return 'The request could not be completed.'
}
