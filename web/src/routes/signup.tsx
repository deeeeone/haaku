import { createFileRoute, Link, useNavigate } from '@tanstack/react-router'
import { UserPlus } from 'lucide-react'
import { type FormEvent, useState } from 'react'
import { toast } from 'sonner'
import { AuthRequestError } from '@/api/authfetch'
import { signup } from '@/api/local-auth'
import { DEFAULT_PASSWORD_MIN_LENGTH, useAuth } from '@/auth/context'
import { type AuthErrorCopy, describeAuthErrorWithBody } from '@/auth/errors'
import {
  AuthNotice,
  AuthPending,
  AuthShell,
  ClerkModeNotice,
} from '@/components/auth/auth-shell'
import { Button, Input, Label } from '@/components/ui'

export const Route = createFileRoute('/signup')({
  component: SignupPage,
})

function SignupPage() {
  const { mode, config, setSession } = useAuth()
  const navigate = useNavigate()
  const [email, setEmail] = useState('')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<AuthErrorCopy | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [pending, setPending] = useState(false)

  if (mode === 'clerk') {
    return <ClerkModeNotice />
  }

  if (pending) {
    return <AuthPending onBack={() => navigate({ to: '/login' })} />
  }

  const firstRun = config?.first_run ?? false
  const policy = config?.signup_policy ?? 'approval'
  const minLength = config?.password_min_length ?? DEFAULT_PASSWORD_MIN_LENGTH

  // A first account is always allowed (it becomes the owner); otherwise obey
  // the instance policy.
  if (policy === 'closed' && !firstRun) {
    return (
      <AuthShell
        title="Signups are closed"
        footer={
          <Link to="/login" className="font-semibold text-sf-accent-2">
            Back to sign in
          </Link>
        }
      >
        <AuthNotice tone="error" title="Signups are closed">
          This instance is not accepting new accounts. Ask an admin to create
          one for you.
        </AuthNotice>
      </AuthShell>
    )
  }

  async function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setError(null)

    if (password.length < minLength) {
      setError({
        title: 'Check the form',
        message: `Password must be at least ${minLength} characters.`,
      })
      return
    }

    setSubmitting(true)
    try {
      const result = await signup({
        email,
        password,
        username: username.trim() || undefined,
      })
      if (result.session) {
        setSession({
          token: result.session.token,
          user: result.user,
          expires_at: result.session.expires_at,
        })
        toast.success(
          result.user.is_admin ? 'Owner account created' : 'Account created'
        )
        navigate({ to: '/' })
      } else {
        // status:"pending", session:null — awaiting approval, not an error.
        setPending(true)
      }
    } catch (err) {
      setError(
        err instanceof AuthRequestError
          ? describeAuthErrorWithBody(err)
          : {
              title: 'Something went wrong',
              message: 'The sign-up request could not be completed.',
            }
      )
    } finally {
      setSubmitting(false)
    }
  }

  const description = firstRun
    ? 'You are the first user on this instance, so this account becomes the owner and admin.'
    : policy === 'approval'
      ? 'New accounts need an admin to approve them before they can sign in.'
      : 'Create an account to get started.'

  return (
    <AuthShell
      title={firstRun ? 'Create the owner account' : 'Create an account'}
      description={description}
      footer={
        <>
          Already have an account?{' '}
          <Link to="/login" className="font-semibold text-sf-accent-2">
            Sign in
          </Link>
        </>
      }
    >
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
          <Label htmlFor="username">Username (optional)</Label>
          <Input
            id="username"
            autoComplete="username"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
          />
        </div>
        <div className="space-y-2">
          <Label htmlFor="password">Password</Label>
          <Input
            id="password"
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
        <Button type="submit" loading={submitting} className="w-full">
          <UserPlus className="size-4" />
          {firstRun ? 'Create owner account' : 'Create account'}
        </Button>
      </form>
    </AuthShell>
  )
}
