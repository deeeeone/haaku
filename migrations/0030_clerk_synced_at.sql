-- +migrate Up
-- Track the last successful Clerk identity sync so the lazy on-auth refresh
-- (internal/auth/clerk.go, identityRefreshInterval = 1h) can skip users that
-- were recently synced. NULL = never synced (covers all pre-existing rows).
-- AutoMigrate adds this column automatically; this file documents it for
-- fresh installs.
ALTER TABLE users ADD COLUMN IF NOT EXISTS clerk_synced_at TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS idx_users_clerk_synced_at ON users (clerk_synced_at);

-- +migrate Down
ALTER TABLE users DROP COLUMN IF EXISTS clerk_synced_at;
