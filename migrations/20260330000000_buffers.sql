-- +goose Up

CREATE TABLE buffers (
    id              TEXT        PRIMARY KEY DEFAULT gen_random_uuid()::text,
    user_id         TEXT        NOT NULL REFERENCES users(id),
    name            TEXT        NOT NULL,
    url             TEXT        NOT NULL,
    method          TEXT        NOT NULL DEFAULT 'POST',
    headers         JSONB       NOT NULL DEFAULT '{}',
    timeout_seconds INT         NOT NULL DEFAULT 30,
    rate_limit      INT         NOT NULL DEFAULT 10,
    max_retries     INT         NOT NULL DEFAULT 3,
    backoff         TEXT        NOT NULL DEFAULT 'exponential',
    paused          BOOLEAN     NOT NULL DEFAULT FALSE,
    webhook_url     TEXT,
    webhook_headers JSONB       NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT buffers_user_name_unique UNIQUE (user_id, name)
);

CREATE INDEX idx_buffers_user ON buffers (user_id, created_at DESC, id DESC);

CREATE TABLE buffer_items (
    id              TEXT        PRIMARY KEY DEFAULT gen_random_uuid()::text,
    buffer_id       TEXT        NOT NULL REFERENCES buffers(id) ON DELETE CASCADE,
    user_id         TEXT        NOT NULL REFERENCES users(id),
    url             TEXT        NOT NULL,
    method          TEXT        NOT NULL,
    headers         JSONB       NOT NULL DEFAULT '{}',
    body            TEXT,
    timeout_seconds INT         NOT NULL DEFAULT 30,
    backoff         TEXT        NOT NULL DEFAULT 'exponential',
    status          TEXT        NOT NULL DEFAULT 'pending',
    retry_count     INT         NOT NULL DEFAULT 0,
    max_retries     INT         NOT NULL DEFAULT 3,
    scheduled_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    claimed_at      TIMESTAMPTZ,
    claimed_by      TEXT,
    heartbeat_at    TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ,
    last_error      TEXT,
    status_code     INT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_buffer_items_drainable ON buffer_items (buffer_id, created_at ASC)
    WHERE status = 'pending';

CREATE INDEX idx_buffer_items_stale ON buffer_items (heartbeat_at)
    WHERE status = 'running';

CREATE INDEX idx_buffer_items_buffer ON buffer_items (buffer_id, created_at DESC, id DESC);

-- +goose Down

DROP TABLE IF EXISTS buffer_items;
DROP TABLE IF EXISTS buffers;
