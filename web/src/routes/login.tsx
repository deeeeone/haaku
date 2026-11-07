import { createFileRoute, Link, useNavigate } from '@tanstack/react-router'
import { LogIn } from 'lucide-react'
import { type FormEvent, useState } from 'react'
import { toast } from 'sonner'
import { AuthRequestError } from '@/api/authfetch'
import { login } from '@/api/local-auth'
import { useAuth } from '@/auth/context'
import { type AuthErrorCopy, describeAuthErrorWithBody } from '@/auth/errors'
import {
  AuthNotice,
  AuthShell,
  ClerkModeNotice,
} from '@/components/auth/auth-shell'
import { Button, Input, Label } from '@/components/ui'

export const Route = createFileRoute('/login')({
  component: LoginPage,
})

function LoginPage() {
  const { mode, config, setSession } = useAuth()
  const navigate = useNavigate()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<AuthErrorCopy | null>(null)
  const [submitting, setSubmitting] = useState(false)

  if (mode === 'clerk') {
    return <ClerkModeNotice />
  }

  const firstRun = config?.first_run ?? false
  const signupOpen = (config?.signup_policy ?? 'approval') !== 'closed'

  async function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setError(null)
    setSubmitting(true)
    try {
      const result = await login({ email, password })
      if (result.session) {
        setSession({
          token: result.session.token,
          user: result.user,
          expires_at: result.session.expires_at,
        })
        toast.success('Signed in')
        navigate({ to: '/' })
      } else {
        setError({
          title: 'Awaiting approval',
          message:
            'This account is waiting for an admin to approve it before you can sign in.',
        })
      }
    } catch (err) {
      setError(
        err instanceof AuthRequestError
          ? describeAuthErrorWithBody(err)
          : {
              title: 'Something went wrong',
              message: 'The sign-in request could not be completed.',
            }
      )
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <AuthShell
      title="Sign in"
      description="Use your email and password to continue."
      footer={
        signupOpen ? (
          <>
            Need an account?{' '}
            <Link to="/signup" className="font-semibold text-sf-accent-2">
              Create one
            </Link>
          </>
        ) : (
          'Signups are closed on this instance.'
        )
      }
    >
      {firstRun && signupOpen ? (
        <AuthNotice tone="info" title="No accounts yet">
          This instance has no users. The first account you create becomes the
          owner.{' '}
          <Link to="/signup" className="font-semibold underline">
            Set it up
          </Link>
        </AuthNotice>
      ) : null}

      {error ? (
        <AuthNotice tone="error" title={error.title}>
          {error.message}
        </AuthNotice>
      ) : null}

      <form className="space-y-4" onSubmit={onSubmit}>
        <div className="space-y-2">
          <Label htmlFor="email">Email</Label>
          <Input
            id="email"
            type="email"
            autoComplete="email"
            required
            value={email}
            onChange={(e) => setEmail(e.target.value)}
          />
        </div>
        <div className="space-y-2">
          <Label htmlFor="password">Password</Label>
          <Input
            id="password"
            type="password"
            autoComplete="current-password"
            required
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </div>
        <Button type="submit" loading={submitting} className="w-full">
          <LogIn className="size-4" />
          Sign in
        </Button>
      </form>
    </AuthShell>
  )
}
