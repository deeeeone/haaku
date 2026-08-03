-- +migrate Up
-- Batch sync sessions (T3.9). Track import progress for history/ratings syncs.

CREATE TABLE IF NOT EXISTS sync_sessions (
    id              BIGSERIAL PRIMARY KEY,
    user_id         TEXT NOT NULL,
    kind            TEXT NOT NULL CHECK (kind IN ('history', 'ratings')),
    status          TEXT NOT NULL DEFAULT 'pending'
                    CHECK (status IN ('pending', 'processing', 'completed', 'failed')),
    total_items     INT NOT NULL DEFAULT 0,
    processed_items INT NOT NULL DEFAULT 0,
    errors          JSONB DEFAULT '[]'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_sync_sessions_user ON sync_sessions (user_id);

-- +migrate Down
DROP TABLE IF EXISTS sync_sessions;
