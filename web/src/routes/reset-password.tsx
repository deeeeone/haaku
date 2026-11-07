import { createFileRoute, Link } from '@tanstack/react-router'
import { KeyRound } from 'lucide-react'
import { type FormEvent, useState } from 'react'
import { AuthRequestError } from '@/api/authfetch'
import { consumePasswordReset } from '@/api/local-auth'
import { DEFAULT_PASSWORD_MIN_LENGTH, useAuth } from '@/auth/context'
import { type AuthErrorCopy, describeAuthErrorWithBody } from '@/auth/errors'
import {
  AuthNotice,
  AuthShell,
  ClerkModeNotice,
} from '@/components/auth/auth-shell'
import { Button, Input, Label } from '@/components/ui'

export const Route = createFileRoute('/reset-password')({
  validateSearch: (search: Record<string, unknown>) => ({
    token: typeof search.token === 'string' ? search.token : '',
  }),
  component: ResetPasswordPage,
})

function ResetPasswordPage() {
  const { mode, config } = useAuth()
  const { token } = Route.useSearch()
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [error, setError] = useState<AuthErrorCopy | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [done, setDone] = useState(false)

  if (mode === 'clerk') {
    return <ClerkModeNotice />
  }

  const minLength = config?.password_min_length ?? DEFAULT_PASSWORD_MIN_LENGTH

  async function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setError(null)

    if (!token) {
      setError({
        title: 'Invalid link',
        message:
          'This reset link is missing its token. Ask an admin to issue a new one.',
      })
      return
    }
    if (password.length < minLength) {
      setError({
        title: 'Check the form',
        message: `Password must be at least ${minLength} characters.`,
      })
      return
    }
    if (password !== confirm) {
      setError({ title: 'Check the form', message: 'Passwords do not match.' })
      return
    }

    setSubmitting(true)
    try {
      await consumePasswordReset(token, password)
      setDone(true)
    } catch (err) {
      if (err instanceof AuthRequestError && err.status === 400) {
        setError({
          title: 'Link invalid or expired',
          message:
            'This reset link has already been used or has expired. Ask an admin for a new one.',
        })
      } else {
        setError(
          err instanceof AuthRequestError
            ? describeAuthErrorWithBody(err)
            : {
                title: 'Something went wrong',
                message: 'The reset request could not be completed.',
              }
        )
      }
    } finally {
      setSubmitting(false)
    }
  }

  if (done) {
    return (
      <AuthShell
        title="Password updated"
        description="You can now sign in with your new password."
        footer={
          <Link to="/login" className="font-semibold text-sf-accent-2">
            Go to sign in
          </Link>
        }
      >
        <AuthNotice tone="info" title="All set">
          Your password has been changed and any previous sessions have been
          signed out.
        </AuthNotice>
      </AuthShell>
    )
  }

  return (
    <AuthShell
      title="Set a new password"
      description="Enter a new password for your account."
      footer={
        <Link to="/login" className="font-semibold text-sf-accent-2">
          Back to sign in
        </Link>
      }
    >
      {error ? (
        <AuthNotice tone="error" title={error.title}>
          {error.message}
        </AuthNotice>
      ) : null}

      <form className="space-y-4" onSubmit={onSubmit}>
        <div className="space-y-2">
          <Label htmlFor="new-password">New password</Label>
          <Input
            id="new-password"
            type="password"
            autoComplete="new-password"
            required
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
          <p className="pl-4 text-xs text-sf-ink-hint">
            At least {minLength} characters.
          </p>
        </div>
        <div className="space-y-2">
          <Label htmlFor="confirm-password">Confirm password</Label>
          <Input
            id="confirm-password"
            type="password"
            autoComplete="new-password"
            required
            value={confirm}
            onChange={(e) => setConfirm(e.target.value)}
          />
        </div>
        <Button type="submit" loading={submitting} className="w-full">
          <KeyRound className="size-4" />
          Update password
        </Button>
      </form>
    </AuthShell>
  )
}
