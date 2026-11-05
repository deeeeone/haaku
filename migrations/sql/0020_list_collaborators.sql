-- +goose Up
-- List collaborators table (T4.3). See 02-architecture/data-model.md.

CREATE TABLE IF NOT EXISTS list_collaborators (
    list_id      BIGINT NOT NULL,
    user_id      TEXT NOT NULL,
    role         TEXT NOT NULL CHECK (role IN ('editor', 'viewer')),
    invited_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    accepted_at  TIMESTAMPTZ,
    PRIMARY KEY (list_id, user_id)
);

-- +goose Down
DROP TABLE IF EXISTS list_collaborators;
