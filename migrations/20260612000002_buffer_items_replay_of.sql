-- +goose Up

-- replay_of links a buffer item created via POST /buffers/:id/items/:itemId/replay
-- back to the failed item it was cloned from. NULL for ordinary items. The clone
-- is appended to the tail of the buffer (new created_at) so it drains in order
-- after the current queue, rather than jumping ahead of in-flight work.
ALTER TABLE buffer_items ADD COLUMN replay_of TEXT REFERENCES buffer_items(id) ON DELETE SET NULL;

CREATE INDEX idx_buffer_items_replay_of ON buffer_items (replay_of) WHERE replay_of IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_buffer_items_replay_of;
ALTER TABLE buffer_items DROP COLUMN replay_of;
