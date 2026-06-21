-- +goose Up

-- low_balance_notified is the hysteresis latch for proactive low-balance alerts.
-- It is set TRUE in the same transaction that takes the balance below the
-- warning threshold, so concurrent workers fire the warning at most once per
-- downward crossing. It is reset to FALSE on top-up and on the daily free
-- refresh, re-arming the warning for the next time the user runs low.
ALTER TABLE user_credits
    ADD COLUMN low_balance_notified BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose Down
ALTER TABLE user_credits DROP COLUMN low_balance_notified;
