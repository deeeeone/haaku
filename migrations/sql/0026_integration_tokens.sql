-- +goose Up
-- 0026_integration_tokens: Inbound integration receiver auth tokens (Phase 8: T8.3)
-- Tokens for generic inbound event receivers.

CREATE TABLE IF NOT EXISTS integration_tokens (
    id SERIAL PRIMARY KEY,
    user_id TEXT NOT NULL,
    token TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL DEFAULT '',
    scopes JSONB NOT NULL DEFAULT '[]'::jsonb,
    active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_integration_tokens_user_id ON integration_tokens(user_id);
CREATE INDEX IF NOT EXISTS idx_integration_tokens_token ON integration_tokens(token);

-- +goose Down
DROP TABLE IF EXISTS integration_tokens;
