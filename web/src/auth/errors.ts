/**
 * Maps the documented auth error codes onto user-facing copy.
 *
 * Deliberate rule (backend enumeration protection): a 401 from login means
 * "unknown email OR wrong password" and MUST read identically for both — the
 * copy below never distinguishes them.
 *
 * The API's error envelope is `{"error":{"code":..,"message":..}}`.
 */

export interface AuthErrorShape {
  status: number
  code?: string
  retryAfterSeconds?: number
}

export interface AuthErrorCopy {
  title: string
  message: string
}

function parseRetryAfter(value: string | null): number | undefined {
  if (!value) return undefined
  const seconds = Number.parseInt(value, 10)
  return Number.isFinite(seconds) && seconds >= 0 ? seconds : undefined
}

/** Extract the machine code + Retry-After from a fetch Response. */
export async function readAuthError(
  response: Response
): Promise<AuthErrorShape> {
  const body = (await response.json().catch(() => null)) as {
    error?: { code?: string }
  } | null
  return {
    status: response.status,
    code: body?.error?.code,
    retryAfterSeconds: parseRetryAfter(response.headers.get('retry-after')),
  }
}

export function describeAuthError(error: AuthErrorShape): AuthErrorCopy {
  switch (error.status) {
    case 401:
      // Identical for unknown email and wrong password on purpose.
      return {
        title: 'Sign in failed',
        message: 'Incorrect email or password.',
      }
    case 403:
      switch (error.code) {
        case 'account_pending':
          return {
            title: 'Awaiting approval',
            message:
              'This account is waiting for an admin to approve it. Try again once it has been approved.',
          }
        case 'account_banned':
          return {
            title: 'Account locked',
            message:
              'This account has been locked. Contact an administrator if you think this is a mistake.',
          }
        case 'signup_closed':
          return {
            title: 'Signups are closed',
            message:
              'This instance is not accepting new accounts. Ask an admin to create one for you.',
          }
        default:
          return {
            title: 'Not allowed',
            message: 'You do not have access to do that.',
          }
      }
    case 409:
      return {
        title: 'Email already registered',
        message: 'An account with that email already exists. Try signing in.',
      }
    case 429:
      return {
        title: 'Too many attempts',
        message: error.retryAfterSeconds
          ? `Too many attempts. Try again in ${error.retryAfterSeconds} seconds.`
          : 'Too many attempts. Try again shortly.',
      }
    case 400:
      return {
        title: 'Check the form',
        message: 'Some of the details were rejected. Please review and retry.',
      }
    default:
      return {
        title: 'Something went wrong',
        message: 'The request could not be completed. Please try again.',
      }
  }
}

/**
 * The one case where the server's own message is safe and useful to surface:
 * a 400 from signup/password-reset carries a specific validation reason
 * (e.g. "password must be at least 10 characters"). For every ambiguous code we
 * prefer our own neutral copy above.
 */
export function describeAuthErrorWithBody(
  error: AuthErrorShape & { serverMessage?: string }
): AuthErrorCopy {
  if (error.status === 400 && error.serverMessage) {
    return { title: 'Check the form', message: error.serverMessage }
  }
  return describeAuthError(error)
}
