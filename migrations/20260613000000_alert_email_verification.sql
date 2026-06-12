-- +goose Up

-- Email alert channels require address verification before they are enabled, so
-- Fliq cannot be used to send unsolicited mail to third-party addresses. A
-- channel is created with enabled = FALSE, verified = FALSE; the confirm-token
-- endpoint flips both to TRUE. webhook/slack channels backfill as verified
-- (nothing to verify — the target is a caller-owned URL).
ALTER TABLE alert_channels
    ADD COLUMN verified BOOLEAN NOT NULL DEFAULT FALSE;

UPDATE alert_channels SET verified = TRUE WHERE type IN ('webhook', 'slack');

-- +goose Down
ALTER TABLE alert_channels DROP COLUMN verified;
