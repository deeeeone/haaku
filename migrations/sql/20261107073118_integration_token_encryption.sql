-- +goose Up
-- 20261107073118_integration_token_encryption: keep integration tokens out of
-- plaintext in a database dump (issue #16 §2).
--
-- An integration token is a bearer credential the inbound receiver looks up on
-- every request (WHERE token = $1 today), so the stored value must stay
-- findable. A random-nonce AES-256-GCM ciphertext cannot be matched with an
-- equality query, so the application now stores TWO things per row:
--
--   * token     -- the AES-256-GCM ciphertext (enc:v1:<base64url(nonce||sealed)>),
--                  row-bound via AAD, used to recover the credential for
--                  display/rotation;
--   * token_hash -- HMAC-SHA256(the raw token) under an HKDF-derived lookup key,
--                  the deterministic blind index the receiver matches on.
--
-- This migration owns only the columns and the index. Existing plaintext rows
-- are converted by the bounded, idempotent startup backfill
-- (internal/handlers/integrations, wired from cmd/api's migrate path); the
-- migration never rewrites rows, so it stays a schema-only, fast operation.
--
-- Lock implications (stated because the unique index moves):
--   * ADD COLUMN with a constant DEFAULT is metadata-only on PostgreSQL 11+
--     (no table rewrite); it takes ACCESS EXCLUSIVE for the moment the catalog
--     row is written.
--   * The partial UNIQUE index build takes a SHARE lock on the table for its
--     duration — reads continue, writes wait. The table holds one row per user
--     credential (a handful, not a bulk table), so the block is milliseconds.
--     A CONCURRENTLY build would avoid even that pause but cannot run inside
--     Goose's per-migration transaction, so it is deliberately not used here.
--   * DROP CONSTRAINT / DROP INDEX each take ACCESS EXCLUSIVE briefly.

ALTER TABLE integration_tokens
    ADD COLUMN IF NOT EXISTS token_hash TEXT NOT NULL DEFAULT '';

-- Uniqueness now lives on the blind index, not the (now ciphertext) token
-- column. The partial predicate is load-bearing: a not-yet-backfilled legacy
-- row has token_hash = '', and several such rows must coexist until the
-- backfill converts them. Only real hashes are constrained.
CREATE UNIQUE INDEX IF NOT EXISTS uq_integration_tokens_token_hash
    ON integration_tokens (token_hash)
    WHERE token_hash <> '';

-- The old UNIQUE on the plaintext column is meaningless once the column holds
-- ciphertext (every ciphertext is distinct by its random nonce), so it would
-- still ACCEPT a duplicate token while claiming to forbid one. Swap it for the
-- blind-index unique above, which is the real "a token must not collide"
-- guarantee.
ALTER TABLE integration_tokens
    DROP CONSTRAINT IF EXISTS integration_tokens_token_key;

-- The plaintext lookup index is dead weight now that lookups use token_hash.
DROP INDEX IF EXISTS idx_integration_tokens_token;

-- +goose Down
-- Forward-only in substance: the Down restores the old index shape but cannot
-- un-encrypt the rows the backfill converted (that requires the pre-migration
-- dump). It exists so a schema-only rollback is possible before the backfill
-- has run.
DROP INDEX IF EXISTS uq_integration_tokens_token_hash;
CREATE INDEX IF NOT EXISTS idx_integration_tokens_token ON integration_tokens (token);
ALTER TABLE integration_tokens
    ADD CONSTRAINT integration_tokens_token_key UNIQUE (token);
ALTER TABLE integration_tokens DROP COLUMN IF EXISTS token_hash;
