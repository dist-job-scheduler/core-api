-- +goose Up

-- Beta: reduce free daily limit from 500k to 100k credits/day
ALTER TABLE user_credits ALTER COLUMN daily_free_limit SET DEFAULT 100000;

-- Update existing free-plan users to the new limit
UPDATE user_credits SET daily_free_limit = 100000 WHERE plan = 'free';

-- +goose Down

ALTER TABLE user_credits ALTER COLUMN daily_free_limit SET DEFAULT 500000;
UPDATE user_credits SET daily_free_limit = 500000 WHERE plan = 'free';
