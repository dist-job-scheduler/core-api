-- +goose Up
-- Per-table storage tuning for the high-churn scheduler tables.
--
-- These are metadata-only changes: they take a brief ACCESS EXCLUSIVE lock but
-- do NOT rewrite the tables, and `fillfactor` only affects future writes. Safe
-- to run online. (To reclaim existing bloat, run pg_repack out-of-band once;
-- see docs/design/db-vacuum-tuning.md.)
--
-- Why: jobs/buffer_items see claim -> heartbeat (x many) -> complete/reschedule.
-- With default autovacuum_vacuum_scale_factor = 0.2, dead tuples are allowed to
-- reach 20% of the table before cleanup, which bloats these hot tables badly.
-- Lowering the scale factor cleans them far sooner; the cost_limit bump lets the
-- autovacuum worker keep up without falling behind.

ALTER TABLE jobs SET (
    fillfactor                      = 90,
    autovacuum_vacuum_scale_factor  = 0.05,
    autovacuum_vacuum_threshold     = 200,
    autovacuum_analyze_scale_factor = 0.05,
    autovacuum_analyze_threshold    = 200,
    autovacuum_vacuum_cost_limit    = 1000
);

ALTER TABLE buffer_items SET (
    fillfactor                      = 90,
    autovacuum_vacuum_scale_factor  = 0.05,
    autovacuum_vacuum_threshold     = 200,
    autovacuum_analyze_scale_factor = 0.05,
    autovacuum_analyze_threshold    = 200,
    autovacuum_vacuum_cost_limit    = 1000
);

-- job_attempts: insert, then a single completion UPDATE that touches no indexed
-- column. With page headroom that UPDATE becomes a HOT update (no index churn,
-- self-cleaning) instead of a dead tuple. Also insert-heavy, so vacuum sooner.
ALTER TABLE job_attempts SET (
    fillfactor                            = 85,
    autovacuum_vacuum_scale_factor        = 0.05,
    autovacuum_vacuum_threshold           = 200,
    autovacuum_vacuum_insert_scale_factor = 0.1,
    autovacuum_analyze_scale_factor       = 0.05,
    autovacuum_vacuum_cost_limit          = 1000
);

-- buffers: the drain loop updates (tokens, last_refill_at) on every claimed item.
-- Neither column is indexed, so headroom makes that a HOT update too.
ALTER TABLE buffers SET (
    fillfactor                      = 85,
    autovacuum_vacuum_scale_factor  = 0.1,
    autovacuum_analyze_scale_factor = 0.1
);

-- +goose Down
ALTER TABLE jobs RESET (
    fillfactor, autovacuum_vacuum_scale_factor, autovacuum_vacuum_threshold,
    autovacuum_analyze_scale_factor, autovacuum_analyze_threshold, autovacuum_vacuum_cost_limit
);
ALTER TABLE buffer_items RESET (
    fillfactor, autovacuum_vacuum_scale_factor, autovacuum_vacuum_threshold,
    autovacuum_analyze_scale_factor, autovacuum_analyze_threshold, autovacuum_vacuum_cost_limit
);
ALTER TABLE job_attempts RESET (
    fillfactor, autovacuum_vacuum_scale_factor, autovacuum_vacuum_threshold,
    autovacuum_vacuum_insert_scale_factor, autovacuum_analyze_scale_factor, autovacuum_vacuum_cost_limit
);
ALTER TABLE buffers RESET (
    fillfactor, autovacuum_vacuum_scale_factor, autovacuum_analyze_scale_factor
);
