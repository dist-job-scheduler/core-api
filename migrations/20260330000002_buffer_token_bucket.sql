-- +goose Up

-- Per-buffer token bucket for true per-second rate limiting. `tokens` accrues at
-- `rate_limit` tokens/second up to a capacity of `rate_limit` (burst), refilled
-- lazily on each claim from `last_refill_at`. Starting at 0 avoids a startup
-- burst; an idle buffer accrues up to `rate_limit` before its next item.
ALTER TABLE buffers
    ADD COLUMN tokens         DOUBLE PRECISION NOT NULL DEFAULT 0,
    ADD COLUMN last_refill_at TIMESTAMPTZ      NOT NULL DEFAULT NOW();

-- +goose Down

ALTER TABLE buffers
    DROP COLUMN IF EXISTS tokens,
    DROP COLUMN IF EXISTS last_refill_at;
