-- +goose Up
-- Local account module (issue #53): password credentials, opaque server-side
-- sessions, and one-time admin-issued tokens.
--
-- Append-only and additive. Nothing here writes the Clerk mirror columns
-- (users.clerk_sync_status / users.clerk_synced_at); those stay the property of
-- the Clerk sync path, and a Clerk deployment simply never creates rows in
-- these three tables. A local account is a users row whose id carries the
-- 'local_' namespace prefix (see internal/auth/localauth.go), which cannot
-- collide with a Clerk id because Clerk ids are always 'user_...'.

-- Password hashes for local accounts. Argon2id PHC strings
-- ($argon2id$v=19$m=...,t=...,p=...$salt$hash); one row per local user.
CREATE TABLE IF NOT EXISTS local_credentials (
    user_id             TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    password_hash       TEXT NOT NULL,
    password_updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Opaque server-side sessions. The raw token is never stored: token_hash is an
-- HMAC-SHA256 blind index keyed by HKDF-SHA256(secret.HAAKU_SECRET_KEY,
-- "haaku/local-session-token/v1"), so a database dump alone is not a set of
-- usable sessions, and a ban revokes every live session by row rather than
-- waiting for self-contained tokens to expire.
CREATE TABLE IF NOT EXISTS local_sessions (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash   TEXT NOT NULL UNIQUE,
    expires_at   TIMESTAMPTZ NOT NULL,
    revoked_at   TIMESTAMPTZ,
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ip           TEXT NOT NULL DEFAULT '',
    user_agent   TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_local_sessions_user_id ON local_sessions (user_id);
CREATE INDEX IF NOT EXISTS idx_local_sessions_expires_at ON local_sessions (expires_at);

-- One-time tokens the admin issues and hands over (password reset today). Same
-- blind index as sessions. consumed_at is claimed with a conditional UPDATE
-- (... WHERE consumed_at IS NULL) so two concurrent redemptions cannot both win.
CREATE TABLE IF NOT EXISTS local_auth_tokens (
    id          TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash  TEXT NOT NULL UNIQUE,
    purpose     TEXT NOT NULL DEFAULT 'password_reset',
    created_by  TEXT NOT NULL DEFAULT '',
    expires_at  TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_local_auth_tokens_user_id ON local_auth_tokens (user_id);

-- Local signup claims an email address, case-insensitively, and the claim must
-- be unique among local accounts. The index is partial so Clerk-mirrored rows
-- (id 'user_...') keep whatever email Clerk gives them without clashing, and
-- soft-deleted rows release their claim.
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_local_email_unique
    ON users (lower(email))
 WHERE left(id, 6) = 'local_' AND deleted_at IS NULL AND email IS NOT NULL AND email <> '';

-- +goose Down
DROP INDEX IF EXISTS idx_users_local_email_unique;
DROP TABLE IF EXISTS local_auth_tokens;
DROP TABLE IF EXISTS local_sessions;
DROP TABLE IF EXISTS local_credentials;
