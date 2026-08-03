-- +migrate Up
-- Users table (Clerk-managed identity). See 02-architecture/data-model.md §1.

CREATE EXTENSION IF NOT EXISTS citext;

CREATE TABLE IF NOT EXISTS users (
    id               TEXT PRIMARY KEY,         -- Clerk user ID (user_...)
    username         CITEXT UNIQUE NOT NULL,
    email            TEXT,
    name             TEXT NOT NULL DEFAULT '',
    bio              TEXT,
    avatar_url       TEXT,
    location         TEXT,
    is_private       BOOLEAN NOT NULL DEFAULT false,
    is_admin         BOOLEAN NOT NULL DEFAULT false,
    is_moderator     BOOLEAN NOT NULL DEFAULT false,
    status           TEXT NOT NULL DEFAULT 'active',
    plan             TEXT NOT NULL DEFAULT 'free',
    clerk_sync_status TEXT NOT NULL DEFAULT 'pending',
    timezone         TEXT,
    language         TEXT,
    last_active_at   TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at       TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_users_deleted_at ON users (deleted_at);

-- +migrate Down
DROP TABLE IF EXISTS users;
