-- +goose Up
-- Heartbeat sidecar for buffer_items — mirror of job_heartbeats (migration
-- 20260614000001). buffer_items has the identical claim -> heartbeat -> drain
-- shape, so the every-10s heartbeat rewrote the whole item row (url/headers/body)
-- and could never be HOT because heartbeat_at was indexed (idx_buffer_items_stale).
CREATE TABLE buffer_item_heartbeats (
    buffer_item_id TEXT PRIMARY KEY REFERENCES buffer_items(id) ON DELETE CASCADE,
    heartbeat_at   TIMESTAMPTZ NOT NULL
) WITH (
    fillfactor                      = 70,
    autovacuum_vacuum_scale_factor  = 0.05,
    autovacuum_vacuum_threshold     = 50,
    autovacuum_analyze_scale_factor = 0.1
);

-- Keep in-flight items alive across the deploy.
INSERT INTO buffer_item_heartbeats (buffer_item_id, heartbeat_at)
SELECT id, COALESCE(heartbeat_at, NOW()) FROM buffer_items WHERE status = 'running'
ON CONFLICT (buffer_item_id) DO NOTHING;

DROP INDEX IF EXISTS idx_buffer_items_stale;

-- +goose Down
CREATE INDEX idx_buffer_items_stale ON buffer_items (heartbeat_at) WHERE status = 'running';
DROP TABLE buffer_item_heartbeats;
