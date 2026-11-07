import { CheckCircle2, ShieldCheck } from 'lucide-react'
import type { ReactNode } from 'react'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui'

/** Centred single-purpose layout for the login / signup / reset screens. */
export function AuthShell({
  title,
  description,
  children,
  footer,
}: {
  title: string
  description?: ReactNode
  children: ReactNode
  footer?: ReactNode
}) {
  return (
    <div className="w-full max-w-md">
      <div className="mb-6 flex items-center justify-center gap-2.5">
        <span className="grid size-9 place-items-center rounded-full bg-linear-to-br from-sf-accent to-sf-accent-2 shadow-[0_6px_20px_rgba(140,107,255,0.4)]">
          <svg
            viewBox="0 0 24 24"
            className="size-4 fill-white"
            aria-hidden="true"
          >
            <path d="M8 5l10 7-10 7z" />
          </svg>
        </span>
        <span className="font-display text-lg font-bold tracking-tight text-sf-ink">
          haaku
        </span>
      </div>
      <Card spotlight>
        <CardHeader>
          <CardTitle className="text-xl">{title}</CardTitle>
          {description ? (
            <CardDescription>{description}</CardDescription>
          ) : null}
        </CardHeader>
        <CardContent className="space-y-4">{children}</CardContent>
      </Card>
      {footer ? (
        <div className="mt-5 text-center text-sm text-sf-ink-muted">
          {footer}
        </div>
      ) : null}
    </div>
  )
}

/** Inline error / info panel used by the auth screens. */
export function AuthNotice({
  tone,
  title,
  children,
}: {
  tone: 'error' | 'info'
  title: string
  children: ReactNode
}) {
  const styles =
    tone === 'error'
      ? 'border-sf-brand-red/30 bg-sf-brand-red/10 text-[#ff7a82]'
      : 'border-sf-accent/30 bg-sf-accent/10 text-[#b9a6ff]'
  return (
    <div
      role={tone === 'error' ? 'alert' : 'status'}
      className={`rounded-2xl border px-4 py-3 text-sm ${styles}`}
    >
      <p className="font-semibold">{title}</p>
      <p className="mt-1 opacity-90">{children}</p>
    </div>
  )
}

/**
 * "Awaiting admin approval" state — deliberately NOT an error. A signup that
 * returns `status:"pending"` and a null session lands here.
 */
export function AuthPending({ onBack }: { onBack: () => void }) {
  return (
    <AuthShell title="Account created">
      <div className="flex flex-col items-center gap-3 text-center">
        <CheckCircle2 className="size-10 text-sf-accent-2" aria-hidden />
        <p className="text-sm text-sf-ink">An admin must approve it first.</p>
        <p className="text-sm text-sf-ink-muted">
          You can sign in as soon as your account has been approved. Nothing
          else is needed from you.
        </p>
      </div>
      <button
        type="button"
        onClick={onBack}
        className="w-full rounded-full border border-sf-line py-2.5 text-sm font-semibold text-sf-ink transition-colors hover:bg-sf-panel"
      >
        Back to sign in
      </button>
    </AuthShell>
  )
}

/** Rendered when a local-only screen is reached in clerk mode. */
export function ClerkModeNotice() {
  return (
    <AuthShell title="Local sign-in is off">
      <div className="flex flex-col items-center gap-3 text-center">
        <ShieldCheck className="size-10 text-sf-ink-muted" aria-hidden />
        <p className="text-sm text-sf-ink-muted">
          This instance uses Clerk for identity. Local accounts are not enabled.
        </p>
      </div>
      <a
        href="/"
        className="block w-full rounded-full bg-linear-to-r from-sf-accent to-sf-accent-2 py-2.5 text-center text-sm font-semibold text-white"
      >
        Go home
      </a>
    </AuthShell>
  )
}
