-- +goose Up
-- Durable, at-least-once webhook delivery log. Each row is one webhook that must
-- reach a customer URL; the webhook dispatcher retries it with backoff until a 2xx
-- (delivered) or the attempt budget is exhausted (failed). This replaces the old
-- fire-and-forget, at-most-once job webhook that was lost on the first failure.
CREATE TABLE webhook_deliveries (
    id            TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    user_id       TEXT NOT NULL REFERENCES users(id),
    -- job_id is nullable so the log can carry non-job events later; today every
    -- delivery is triggered by a terminal job state.
    job_id        TEXT,
    event         TEXT NOT NULL,
    url           TEXT NOT NULL,
    headers       JSONB NOT NULL DEFAULT '{}',
    -- payload is the exact JSON body that is HMAC-signed and POSTed, stored as text
    -- so the bytes are preserved verbatim (JSONB would re-order/normalise them).
    payload       TEXT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'pending',
    status_code   INT,
    attempts      INT NOT NULL DEFAULT 0,
    max_attempts  INT NOT NULL DEFAULT 10,
    next_retry_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_error    TEXT,
    delivered_at  TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Dispatcher poll: claim due pending rows and reclaim abandoned in-flight rows.
-- Partial index keeps it tiny — terminal (delivered/failed) rows drop out.
CREATE INDEX idx_webhook_deliveries_due
    ON webhook_deliveries (next_retry_at)
    WHERE status IN ('pending', 'delivering');

-- Read API: a user's recent deliveries, newest first.
CREATE INDEX idx_webhook_deliveries_user
    ON webhook_deliveries (user_id, created_at DESC, id DESC);

-- +goose Down
DROP TABLE webhook_deliveries;
