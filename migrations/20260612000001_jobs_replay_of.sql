-- +goose Up

-- replay_of links a job created via POST /jobs/:id/replay back to the failed
-- job it was cloned from, for dead-letter lineage. NULL for ordinary jobs.
-- ON DELETE SET NULL is defensive: jobs are not row-deleted today (DELETE =
-- cancel), but a future hard-delete must not orphan the FK.
ALTER TABLE jobs ADD COLUMN replay_of TEXT REFERENCES jobs(id) ON DELETE SET NULL;

CREATE INDEX idx_jobs_replay_of ON jobs (replay_of) WHERE replay_of IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_jobs_replay_of;
ALTER TABLE jobs DROP COLUMN replay_of;
