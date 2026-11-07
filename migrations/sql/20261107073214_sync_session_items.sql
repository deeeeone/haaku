-- +goose Up
-- Persist the items submitted to a batch sync session (issue #6).
--
-- POST /v1/syncs used to keep only len(req.Items): the payload was thrown away,
-- so a session could never be resumed or audited and /paused · /skipped had
-- nothing to report. This table is the durable queue the engine works from and
-- the audit trail a client reads back.
--
-- Shape (chosen before the columns, per the issue):
--   * payload     the submitted item's identity and attributes as jsonb. The
--                 submitted form is stored — not only a resolved media_id —
--                 because an item we cannot resolve has no media_id yet, and
--                 that unresolved payload is exactly what a client re-submits
--                 after fixing it. jsonb keeps it queryable (payload->'ids')
--                 without inventing a column per media shape.
--   * media_type /
--     media_id    filled in when the payload resolves, so /paused, /skipped and
--                 the audit read plain columns and never re-parse jsonb.
--   * seq         the item's index in the submitted batch. It is the stable
--                 order the engine walks and the pagination tiebreak, so a
--                 resumed session continues from the lowest unprocessed seq.
--   * state       pending -> processed: the queue state. Only pending rows are
--                 ever worked, which is what makes re-running a session
--                 idempotent at the row level (the collection write is
--                 additionally ON CONFLICT DO NOTHING / upsert).
--   * outcome     added | not_found | duplicate | error, NULL while pending:
--                 the terminal per-item resolution. /paused serves
--                 not_found + error, /skipped serves duplicate.
-- Size: an item is ids + watched_at + rating — identity-sized, bounded jsonb.
-- We deliberately do not store request headers or the whole HTTP body, so this
-- table cannot grow with anything but the submitted items themselves.
--
-- Idempotency: UNIQUE (session_id, seq) makes re-submitting the same batch a
-- no-op instead of a second queue entry, so a client retrying a POST cannot
-- enqueue an item twice. ON DELETE CASCADE keeps the items with their session.
CREATE TABLE IF NOT EXISTS sync_session_items (
    id            BIGSERIAL PRIMARY KEY,
    session_id    BIGINT NOT NULL REFERENCES sync_sessions(id) ON DELETE CASCADE,
    seq           INT NOT NULL,
    payload       JSONB NOT NULL DEFAULT '{}'::jsonb,
    media_type    TEXT,
    media_id      BIGINT,
    state         TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'processed')),
    outcome       TEXT CHECK (outcome IN ('added', 'not_found', 'duplicate', 'error')),
    error_code    TEXT,
    error         TEXT,
    watched_at    TIMESTAMPTZ,
    rating        INT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at  TIMESTAMPTZ,
    CONSTRAINT uq_sync_session_items_seq UNIQUE (session_id, seq)
);

-- The queue scan: the engine claims the lowest unprocessed items of a session.
-- Partial, so the index holds only the work left to do — a completed session's
-- rows drop out of it entirely and never bloat the scan.
CREATE INDEX IF NOT EXISTS idx_sync_session_items_pending
    ON sync_session_items (session_id, seq) WHERE state = 'pending';

-- /paused and /skipped page by (outcome, seq) within one session.
CREATE INDEX IF NOT EXISTS idx_sync_session_items_outcome
    ON sync_session_items (session_id, outcome, seq);

-- The worker's "what needs processing" scan over sessions. Partial for the same
-- reason: a finished session is not queue work.
CREATE INDEX IF NOT EXISTS idx_sync_sessions_pending
    ON sync_sessions (id) WHERE status = 'pending';

-- +goose Down
DROP TABLE IF EXISTS sync_session_items;
DROP INDEX IF EXISTS idx_sync_sessions_pending;
