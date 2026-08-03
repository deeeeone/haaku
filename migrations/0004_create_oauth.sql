-- +migrate Up
-- OAuth2 tables: clients, authorization grants, and tokens.
-- See internal/database/oauth_models.go for GORM models.

CREATE TABLE IF NOT EXISTS oauth_clients (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name          TEXT NOT NULL,
    redirect_uris JSONB NOT NULL DEFAULT '[]',
    secret_hash   TEXT NOT NULL,
    user_id       TEXT NOT NULL,            -- Clerk user ID
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_oauth_clients_user_id ON oauth_clients (user_id);

CREATE TABLE IF NOT EXISTS oauth_grants (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id             UUID NOT NULL REFERENCES oauth_clients(id) ON DELETE CASCADE,
    user_id               TEXT NOT NULL,       -- Clerk user ID
    code_hash             TEXT NOT NULL,
    code_challenge        TEXT,
    code_challenge_method TEXT DEFAULT 'S256',
    redirect_uri          TEXT NOT NULL,
    scopes                TEXT[] NOT NULL DEFAULT '{}',
    expires_at            TIMESTAMPTZ NOT NULL,
    used                  BOOLEAN NOT NULL DEFAULT false,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_oauth_grants_code_hash ON oauth_grants (code_hash);

CREATE TABLE IF NOT EXISTS oauth_tokens (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    grant_id              UUID REFERENCES oauth_grants(id) ON DELETE SET NULL,
    client_id             UUID NOT NULL REFERENCES oauth_clients(id) ON DELETE CASCADE,
    user_id               TEXT NOT NULL,       -- Clerk user ID
    token_hash            TEXT NOT NULL,
    refresh_token_hash    TEXT,
    refresh_token_family  TEXT,                -- group refresh tokens for reuse detection
    scopes                TEXT[] NOT NULL DEFAULT '{}',
    expires_at            TIMESTAMPTZ NOT NULL,
    refresh_expires_at    TIMESTAMPTZ,
    revoked               BOOLEAN NOT NULL DEFAULT false,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_oauth_tokens_token_hash ON oauth_tokens (token_hash);
CREATE INDEX IF NOT EXISTS idx_oauth_tokens_refresh_hash ON oauth_tokens (refresh_token_hash);
CREATE INDEX IF NOT EXISTS idx_oauth_tokens_user_id ON oauth_tokens (user_id);

-- +migrate Down
DROP TABLE IF EXISTS oauth_tokens;
DROP TABLE IF EXISTS oauth_grants;
DROP TABLE IF EXISTS oauth_clients;
