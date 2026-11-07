import { useClerk } from '@clerk/react'
import { Button } from '@/components/ui'

/**
 * "Edit in Clerk" affordance.
 *
 * `useClerk` throws outside a ClerkProvider, so this lives in its own component
 * that is only ever mounted in clerk mode (where the provider exists). Local
 * mode renders nothing instead of an inert button.
 */
export function ClerkAccountButton({ label }: { label: string }) {
  const { openUserProfile } = useClerk()
  return (
    <Button variant="glass" size="sm" onClick={() => openUserProfile()}>
      {label}
    </Button>
  )
}
