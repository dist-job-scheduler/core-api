-- +goose Up
-- Heartbeat sidecar: move the every-10s heartbeat write off the heavy `jobs` row.
--
-- A running job's heartbeat used to rewrite the whole jobs row (url/headers/body,
-- ~1.3KB) every 10s, and because heartbeat_at was indexed (idx_jobs_stale) the
-- update could never be HOT. That is the dominant source of dead tuples / WAL on
-- this table (measured: ~24MB dead heap+index per hour per 50 running jobs).
--
-- This table holds ONE tiny row per *currently running* job. It has no secondary
-- index, so the heartbeat UPDATE is a HOT update (self-cleaning, no index churn),
-- and the table stays tiny because rows are deleted when a job leaves 'running'.
CREATE TABLE job_heartbeats (
    job_id       TEXT PRIMARY KEY REFERENCES jobs(id) ON DELETE CASCADE,
    heartbeat_at TIMESTAMPTZ NOT NULL
) WITH (
    fillfactor                      = 70,   -- headroom so updates stay HOT
    autovacuum_vacuum_scale_factor  = 0.05,
    autovacuum_vacuum_threshold     = 50,
    autovacuum_analyze_scale_factor = 0.1
);

-- Keep in-flight jobs alive across the deploy: seed their heartbeat so the reaper
-- doesn't immediately treat every running job as crashed.
INSERT INTO job_heartbeats (job_id, heartbeat_at)
SELECT id, COALESCE(heartbeat_at, NOW()) FROM jobs WHERE status = 'running'
ON CONFLICT (job_id) DO NOTHING;

-- The reaper now derives staleness from job_heartbeats, so this index is dead
-- weight (and dropping it stops claim from maintaining it).
DROP INDEX IF EXISTS idx_jobs_stale;

-- +goose Down
CREATE INDEX idx_jobs_stale ON jobs (heartbeat_at) WHERE status = 'running';
DROP TABLE job_heartbeats;
