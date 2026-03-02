-- +goose Up
CREATE TABLE api_tokens (
    id           TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    user_id      TEXT NOT NULL REFERENCES users(id),
    name         TEXT NOT NULL,
    token_hash   TEXT UNIQUE NOT NULL,   -- SHA-256 of the raw token
    prefix       TEXT NOT NULL,          -- "fliq_sk_XXXXXXXX" for display
    last_used_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_api_tokens_user ON api_tokens(user_id);

-- +goose Down
DROP INDEX idx_api_tokens_user;
DROP TABLE api_tokens;
