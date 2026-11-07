import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import {
  AlertTriangle,
  Eye,
  Lock,
  Save,
  Shield,
  Trash2,
  User,
} from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'
import { useApi } from '@/api/client'
import { useAuth } from '@/auth/context'
import { ClerkAccountButton } from '@/components/auth/clerk-account-button'
import {
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
import { cn } from '@/lib/utils'

export const Route = createFileRoute('/settings/')({
  component: SettingsIndexPage,
})

function SettingsIndexPage() {
  const api = useApi()
  const queryClient = useQueryClient()
  // Identity comes from the shared context: the Clerk account in clerk mode,
  // the local session user otherwise. In local mode there is no Clerk profile
  // to edit, so the "Edit in Clerk" affordances are hidden.
  const { mode, user } = useAuth()

  const { data: profile } = useQuery({
    queryKey: ['settings', 'profile'],
    queryFn: async () => {
      if (!api) return null
      const res = await api.GET('/v1/users/me')
      return res.data as unknown as Record<string, unknown> | null
    },
    enabled: !!api,
  })

  const { data: settings } = useQuery({
    queryKey: ['settings', 'preferences'],
    queryFn: async () => {
      if (!api) return null
      const res = await api.GET('/v1/users/me/settings')
      return res.data as unknown as Record<string, unknown> | null
    },
    enabled: !!api,
  })

  const [bio, setBio] = useState('')
  const [profileInitialized, setProfileInitialized] = useState(false)

  if (profile && !profileInitialized) {
    setBio((profile.bio as string) ?? '')
    setProfileInitialized(true)
  }

  const [isPrivate, setIsPrivate] = useState(
    (settings?.private as boolean) ?? false
  )
  const [showExplicit, setShowExplicit] = useState(
    (settings?.show_explicit as boolean) ?? true
  )
  const [hideSpoilers, setHideSpoilers] = useState(
    (settings?.hide_spoilers as boolean) ?? false
  )

  const [deleteOpen, setDeleteOpen] = useState(false)
  const [deleteConfirm, setDeleteConfirm] = useState('')

  const updateProfileMutation = useMutation({
    mutationFn: async () => {
      if (!api) throw new Error('Not authenticated')
      // bio/location are user-authored; name/avatar are Clerk-managed.
      await api.PUT('/v1/users/me', {
        body: { bio },
      } as never)
    },
    onSuccess: () => {
      toast.success('Profile updated')
      queryClient.invalidateQueries({ queryKey: ['settings', 'profile'] })
      queryClient.invalidateQueries({ queryKey: ['profile', 'me'] })
    },
    onError: () => {
      toast.error('Failed to update profile')
    },
  })

  const updateSettingsMutation = useMutation({
    mutationFn: async (body: Record<string, unknown>) => {
      if (!api) throw new Error('Not authenticated')
      await api.PUT('/v1/users/me/settings', {
        body,
      } as never)
    },
    onSuccess: () => {
      toast.success('Settings updated')
      queryClient.invalidateQueries({ queryKey: ['settings', 'preferences'] })
    },
    onError: () => {
      toast.error('Failed to update settings')
    },
  })

  const deleteAccountMutation = useMutation({
    mutationFn: async () => {
      if (!api) throw new Error('Not authenticated')
      await api.DELETE('/v1/users/me')
    },
    onSuccess: () => {
      toast.success('Account deleted')
      window.location.href = '/'
    },
    onError: () => {
      toast.error('Failed to delete account')
    },
  })

  const displayName =
    user?.displayName || user?.username || (profile?.name as string) || ''

  return (
    <div className="space-y-8">
      <div id="profile">
        <SectionHead title="Profile" />
        <Card>
          <CardHeader>
            <CardTitle>Avatar</CardTitle>
            <CardDescription>
              Your photo comes from your Clerk account
            </CardDescription>
          </CardHeader>
          <CardContent>
            <div className="flex items-center gap-4">
              <div className="flex size-20 items-center justify-center overflow-hidden rounded-full bg-sf-panel-2 ring-2 ring-sf-line">
                {profile?.avatar_url ? (
                  <img
                    src={profile.avatar_url as string}
                    alt="Avatar"
                    className="h-full w-full object-cover"
                  />
                ) : (
                  <User className="size-10 text-sf-ink-muted" />
                )}
              </div>
              {mode === 'clerk' && <ClerkAccountButton label="Edit in Clerk" />}
            </div>
          </CardContent>
        </Card>

        <Card className="mt-4">
          <CardHeader>
            <CardTitle>Display Name</CardTitle>
            <CardDescription>
              {mode === 'clerk'
                ? 'Managed in your Clerk account — changes appear here and on your public profile automatically'
                : 'Your account name — shown on your profile and comments'}
            </CardDescription>
          </CardHeader>
          <CardContent>
            <div className="flex items-center justify-between gap-4">
              <p className="text-sf-ink">{displayName || 'No name set'}</p>
              {mode === 'clerk' && <ClerkAccountButton label="Edit in Clerk" />}
            </div>
          </CardContent>
        </Card>

        <Card className="mt-4">
          <CardHeader>
            <CardTitle>Bio</CardTitle>
            <CardDescription>Tell others about yourself</CardDescription>
          </CardHeader>
          <CardContent>
            <Input
              value={bio}
              onChange={(e) => setBio(e.target.value)}
              placeholder="A short bio..."
            />
          </CardContent>
        </Card>

        <Card className="mt-4">
          <CardHeader>
            <CardTitle>Account Info</CardTitle>
            <CardDescription>Your account details</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div>
              <Label>Username</Label>
              <p className="mt-1 text-sm text-sf-ink-muted">
                @{(profile?.username as string) ?? user?.username ?? 'Unknown'}
              </p>
            </div>
            <div>
              <Label>Email</Label>
              <p className="mt-1 text-sm text-sf-ink-muted">
                {user?.email ?? (profile?.email as string) ?? 'No email'}
              </p>
            </div>
          </CardContent>
          <CardFooter>
            <Button
              variant="primary"
              size="sm"
              onClick={() => updateProfileMutation.mutate()}
              loading={updateProfileMutation.isPending}
            >
              <Save className="size-4" />
              Save Profile
            </Button>
          </CardFooter>
        </Card>
      </div>

      <div id="privacy">
        <SectionHead title="Privacy" />
        <Card>
          <CardHeader>
            <CardTitle>Profile Visibility</CardTitle>
            <CardDescription>
              {isPrivate
                ? 'Your profile is private — only followers can see it'
                : 'Your profile is public — anyone can see it'}
            </CardDescription>
          </CardHeader>
          <CardContent>
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-3">
                <Lock className="size-4 text-sf-ink-muted" />
                <Label>Private Profile</Label>
              </div>
              <button
                type="button"
                role="switch"
                aria-checked={isPrivate}
                onClick={() => {
                  const next = !isPrivate
                  setIsPrivate(next)
                  updateSettingsMutation.mutate({ private: next })
                }}
                className={cn(
                  'relative inline-flex h-6 w-11 shrink-0 cursor-pointer items-center rounded-full transition-colors',
                  isPrivate ? 'bg-sf-accent' : 'bg-sf-panel-2'
                )}
              >
                <span
                  className={cn(
                    'inline-block size-4 rounded-full bg-white shadow-sm transition-transform',
                    isPrivate ? 'translate-x-6' : 'translate-x-1'
                  )}
                />
              </button>
            </div>
          </CardContent>
        </Card>

        <SectionHead title="Display" className="mt-8" />
        <Card>
          <CardHeader>
            <CardTitle>Content Display</CardTitle>
            <CardDescription>
              Control how content is shown to you
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-3">
                <Eye className="size-4 text-sf-ink-muted" />
                <Label>Hide Spoilers</Label>
              </div>
              <button
                type="button"
                role="switch"
                aria-checked={hideSpoilers}
                onClick={() => {
                  const next = !hideSpoilers
                  setHideSpoilers(next)
                  updateSettingsMutation.mutate({ hide_spoilers: next })
                }}
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
                <Label>Show Mature Content</Label>
              </div>
              <button
                type="button"
                role="switch"
                aria-checked={showExplicit}
                onClick={() => {
                  const next = !showExplicit
                  setShowExplicit(next)
                  updateSettingsMutation.mutate({ show_explicit: next })
                }}
                className={cn(
                  'relative inline-flex h-6 w-11 shrink-0 cursor-pointer items-center rounded-full transition-colors',
                  showExplicit ? 'bg-sf-accent' : 'bg-sf-panel-2'
                )}
              >
                <span
                  className={cn(
                    'inline-block size-4 rounded-full bg-white shadow-sm transition-transform',
                    showExplicit ? 'translate-x-6' : 'translate-x-1'
                  )}
                />
              </button>
            </div>
          </CardContent>
        </Card>
      </div>

      <div id="danger">
        <SectionHead
          title="Danger Zone"
          eyebrow="Destructive actions — proceed with caution"
        />
        <Card className="border-sf-brand-red/30">
          <CardHeader>
            <CardTitle className="text-[#ff7a82]">
              <AlertTriangle className="mr-2 inline size-5" />
              Delete Account
            </CardTitle>
            <CardDescription>
              Permanently delete your account, all data, and all associated
              content. This action cannot be undone.
            </CardDescription>
          </CardHeader>
          <CardFooter>
            <Button
              variant="danger"
              size="sm"
              onClick={() => setDeleteOpen(true)}
            >
              <Trash2 className="size-4" />
              Delete Account
            </Button>
          </CardFooter>
        </Card>
      </div>

      <Dialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete Account</DialogTitle>
            <DialogDescription>
              This action is permanent and cannot be undone. All your data will
              be permanently removed.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <Banner type="error" id="delete-warning">
              Type your username to confirm deletion
            </Banner>
            <Input
              placeholder={
                (profile?.username as string) ?? user?.username ?? 'username'
              }
              value={deleteConfirm}
              onChange={(e) => setDeleteConfirm(e.target.value)}
            />
          </div>
          <DialogFooter>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => setDeleteOpen(false)}
            >
              Cancel
            </Button>
            <Button
              variant="danger"
              size="sm"
              disabled={
                deleteConfirm !==
                ((profile?.username as string) ?? user?.username)
              }
              onClick={() => deleteAccountMutation.mutate()}
              loading={deleteAccountMutation.isPending}
            >
              Delete Forever
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
