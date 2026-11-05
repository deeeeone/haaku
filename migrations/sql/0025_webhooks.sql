-- +goose Up
-- 0025_webhooks: Outbound webhook endpoints and delivery log (Phase 8: T8.1)
-- Webhook endpoint registry + HMAC delivery tracking.

CREATE TABLE IF NOT EXISTS webhook_endpoints (
    id BIGSERIAL PRIMARY KEY,
    user_id TEXT NOT NULL,
    url TEXT NOT NULL,
    events JSONB NOT NULL DEFAULT '[]'::jsonb,
    secret_hash TEXT NOT NULL DEFAULT '',
    active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_webhook_endpoints_user_id ON webhook_endpoints(user_id);
CREATE INDEX IF NOT EXISTS idx_webhook_endpoints_active ON webhook_endpoints(active);

CREATE TABLE IF NOT EXISTS webhook_deliveries (
    id BIGSERIAL PRIMARY KEY,
    endpoint_id BIGINT NOT NULL REFERENCES webhook_endpoints(id) ON DELETE CASCADE,
    event_id TEXT NOT NULL DEFAULT '',
    event_type TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending',
    attempts INT NOT NULL DEFAULT 0,
    response_code INT,
    response_body TEXT,
    duration_ms INT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_endpoint_created ON webhook_deliveries(endpoint_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_event_id ON webhook_deliveries(event_id);

-- +goose Down
DROP TABLE IF EXISTS webhook_deliveries;
DROP TABLE IF EXISTS webhook_endpoints;
