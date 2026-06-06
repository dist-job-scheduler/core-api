-- +goose Up

-- Buffer-item executions need a ledger reference too. job_id is already nullable,
-- so add a parallel nullable buffer_item_id and ensure a transaction references at
-- most one of the two (a row may reference neither, e.g. daily_grant / stripe_topup).
ALTER TABLE credit_transactions
    ADD COLUMN buffer_item_id TEXT REFERENCES buffer_items(id);

ALTER TABLE credit_transactions
    ADD CONSTRAINT credit_tx_one_ref CHECK (num_nonnulls(job_id, buffer_item_id) <= 1);

-- +goose Down

ALTER TABLE credit_transactions DROP CONSTRAINT IF EXISTS credit_tx_one_ref;
ALTER TABLE credit_transactions DROP COLUMN IF EXISTS buffer_item_id;
