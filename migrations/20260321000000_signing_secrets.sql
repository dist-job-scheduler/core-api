-- +goose Up
CREATE TABLE user_signing_secrets (
    id         TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    user_id    TEXT NOT NULL REFERENCES users(id),
    secret     TEXT NOT NULL,
    is_active  BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- At most one active secret per user, enforced at the DB level.
CREATE UNIQUE INDEX idx_signing_secrets_active_user
    ON user_signing_secrets(user_id) WHERE is_active = TRUE;

-- +goose Down
DROP TABLE user_signing_secrets;
