import { UserButton } from '@clerk/react'
import { LogOut } from 'lucide-react'
import { useAuth } from '@/auth/context'
import { ProfilePill } from '@/design-kit-components/shared/profile-pill'
import { Dropdown } from '@/design-kit-components/ui/dropdown'

/**
 * Mode-aware account control in the topbar.
 *
 * clerk mode renders Clerk's `<UserButton>` exactly as before; local mode
 * renders a design-kit menu with an explicit sign-out that calls the logout
 * endpoint and discards the token. Clerk's components are never mounted in
 * local mode (they need a ClerkProvider that isn't there).
 */
export function UserMenu() {
  const { mode, user, isAdmin, signOut } = useAuth()

  if (mode === 'clerk') {
    return (
      <UserButton
        appearance={{
          // Theme Clerk's account popout to the app's design tokens.
          variables: {
            colorPrimary: 'var(--sf-accent)',
            colorBackground: 'var(--sf-bg-2)',
            colorForeground: 'var(--sf-ink)',
            colorInput: 'var(--sf-panel-2)',
            borderRadius: '0.75rem',
          },
          elements: {
            avatarBox: 'size-9',
          },
        }}
      />
    )
  }

  const name = user?.displayName || user?.username || user?.email || 'Account'
  const initials = name.slice(0, 2).toUpperCase()

  return (
    <Dropdown
      trigger={
        <ProfilePill
          name={name}
          subtitle={isAdmin ? 'Admin' : 'Member'}
          initials={initials}
        />
      }
      items={[
        {
          label: 'Sign out',
          icon: <LogOut className="size-4" />,
          danger: true,
          onSelect: () => {
            void signOut()
          },
        },
      ]}
    />
  )
}
