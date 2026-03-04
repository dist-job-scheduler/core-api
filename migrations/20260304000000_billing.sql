-- +goose Up

-- Credit balance (one row per user)
CREATE TABLE user_credits (
    user_id          TEXT PRIMARY KEY REFERENCES users(id),
    balance          BIGINT NOT NULL DEFAULT 0,
    plan             TEXT NOT NULL DEFAULT 'free',
    daily_free_limit INT NOT NULL DEFAULT 500000,  -- 500k executions/day free (5 days at $1)
    refreshed_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Immutable audit ledger
CREATE TABLE credit_transactions (
    id                       TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    user_id                  TEXT NOT NULL REFERENCES users(id),
    amount                   BIGINT NOT NULL,   -- negative = consume, positive = grant
    type                     TEXT NOT NULL,     -- 'job_execution' | 'daily_grant' | 'stripe_topup'
    job_id                   TEXT REFERENCES jobs(id),
    stripe_payment_intent_id TEXT,
    description              TEXT,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_credit_tx_user ON credit_transactions(user_id, created_at DESC);

-- Stripe customer mapping
CREATE TABLE stripe_customers (
    user_id            TEXT PRIMARY KEY REFERENCES users(id),
    stripe_customer_id TEXT UNIQUE NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Backfill existing users with 500k free starter credits (~$5 worth at $1/100k)
INSERT INTO user_credits (user_id, balance, plan, refreshed_at)
SELECT id, 500000, 'free', NOW() FROM users ON CONFLICT DO NOTHING;

-- +goose Down

DROP TABLE IF EXISTS stripe_customers;
DROP INDEX IF EXISTS idx_credit_tx_user;
DROP TABLE IF EXISTS credit_transactions;
DROP TABLE IF EXISTS user_credits;
