/**
 * Typed client for the admin endpoints (PR #73 contract).
 *
 * These routes exist only in local mode (`RegisterLocalAuthRoutes` returns early
 * in clerk mode), so the panel is local-only. Callers must expect `403` for a
 * non-admin and `401` for an anonymous request rather than assuming success.
 */

import { authFetch } from '@/api/authfetch'

/** A row in the admin users table. */
export interface AdminUser {
  id: string
  email?: string | null
  username?: string | null
  status: string
  is_admin: boolean
  created_at?: string | null
  last_active_at?: string | null
}

export interface AdminUserList {
  users: AdminUser[]
  total: number
  limit: number
  offset: number
}

export interface AdminResetToken {
  user_id: string
  token: string
  expires_at: string
  url_path: string
  delivery: string
}

export type AdminAction = 'ban' | 'unlock' | 'approve'

export interface AdminListParams {
  limit?: number
  offset?: number
  status?: string
}

function query(params: AdminListParams): string {
  const search = new URLSearchParams()
  if (params.limit != null) search.set('limit', String(params.limit))
  if (params.offset != null) search.set('offset', String(params.offset))
  if (params.status) search.set('status', params.status)
  const qs = search.toString()
  return qs ? `?${qs}` : ''
}

/** `GET /v1/admin/users` → `{users[],total,limit,offset}`. */
export function listUsers(
  token: string | null,
  params: AdminListParams = {}
): Promise<AdminUserList> {
  return authFetch<AdminUserList>(`v1/admin/users${query(params)}`, { token })
}

/** `POST /v1/admin/users/{id}/{ban|unlock|approve}` → the updated user. */
export function userAction(
  token: string | null,
  id: string,
  action: AdminAction
): Promise<AdminUser> {
  return authFetch<AdminUser>(
    `v1/admin/users/${encodeURIComponent(id)}/${action}`,
    { method: 'POST', token }
  )
}

/**
 * `POST /v1/admin/users/{id}/password-reset` → a one-time token.
 * `url_path` and `token` are shown to the admin ONCE and are not recoverable.
 */
export function issuePasswordReset(
  token: string | null,
  id: string
): Promise<AdminResetToken> {
  return authFetch<AdminResetToken>(
    `v1/admin/users/${encodeURIComponent(id)}/password-reset`,
    { method: 'POST', token }
  )
}
