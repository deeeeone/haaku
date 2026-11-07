/**
 * Public barrel for the auth module.
 *
 * Deliberately Clerk-free: `client.ts` and the tests import from here, and only
 * `provider.tsx` is allowed to reach into `@clerk/react`.
 */

export {
  DEFAULT_PASSWORD_MIN_LENGTH,
  fetchAuthConfig,
} from './config'
export {
  AuthContext,
  type AuthContextValue,
  useAuth,
  useAuthConfig,
} from './context'
export {
  type AuthErrorCopy,
  type AuthErrorShape,
  describeAuthError,
  describeAuthErrorWithBody,
  readAuthError,
} from './errors'
export {
  clearSession,
  readSession,
  type StoredSession,
  subscribeSession,
  writeSession,
} from './session-store'
export type {
  AuthConfig,
  AuthMode,
  AuthUser,
  LocalLoginResult,
  LocalSession,
  LocalSignupResult,
  SignupPolicy,
} from './types'
