-- +goose Up

-- The buffer_item_id FK added in 20260330000001 had no ON DELETE action. Deleting
-- a buffer cascades to its buffer_items (see 20260330000000), but any item that was
-- billed has a credit_transactions row referencing it, so the cascade hits a FK
-- violation (SQLSTATE 23503) and DELETE /buffers/:id 500s for every buffer that ever
-- executed. credit_transactions is an immutable billing ledger — detach rather than
-- delete: ON DELETE SET NULL preserves the charge record and still satisfies the
-- credit_tx_one_ref CHECK (num_nonnulls(job_id, buffer_item_id) <= 1).
ALTER TABLE credit_transactions
    DROP CONSTRAINT credit_transactions_buffer_item_id_fkey,
    ADD CONSTRAINT credit_transactions_buffer_item_id_fkey
        FOREIGN KEY (buffer_item_id) REFERENCES buffer_items(id) ON DELETE SET NULL;

-- +goose Down

ALTER TABLE credit_transactions
    DROP CONSTRAINT credit_transactions_buffer_item_id_fkey,
    ADD CONSTRAINT credit_transactions_buffer_item_id_fkey
        FOREIGN KEY (buffer_item_id) REFERENCES buffer_items(id);
