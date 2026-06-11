-- +goose Up

-- Alert channels: user-configured destinations that receive a notification
-- when a job or buffer item exhausts its retries (permanent failure).
-- type is the delivery mechanism ('webhook' = generic JSON POST, 'slack' =
-- Slack incoming-webhook formatted message). target is the destination URL.
CREATE TABLE alert_channels (
    id         TEXT        PRIMARY KEY DEFAULT gen_random_uuid()::text,
    user_id    TEXT        NOT NULL REFERENCES users(id),
    type       TEXT        NOT NULL,
    target     TEXT        NOT NULL,
    name       TEXT        NOT NULL DEFAULT '',
    enabled    BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_alert_channels_user ON alert_channels (user_id, created_at DESC, id DESC);

-- The scheduler looks up a user's enabled channels on every permanent failure.
CREATE INDEX idx_alert_channels_enabled ON alert_channels (user_id) WHERE enabled;

-- +goose Down
DROP TABLE alert_channels;
