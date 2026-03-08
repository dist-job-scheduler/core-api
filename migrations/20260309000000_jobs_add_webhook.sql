-- +goose Up
ALTER TABLE jobs ADD COLUMN webhook_url TEXT;
ALTER TABLE jobs ADD COLUMN webhook_headers JSONB NOT NULL DEFAULT '{}';
ALTER TABLE schedules ADD COLUMN webhook_url TEXT;
ALTER TABLE schedules ADD COLUMN webhook_headers JSONB NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE schedules DROP COLUMN webhook_headers;
ALTER TABLE schedules DROP COLUMN webhook_url;
ALTER TABLE jobs DROP COLUMN webhook_headers;
ALTER TABLE jobs DROP COLUMN webhook_url;
