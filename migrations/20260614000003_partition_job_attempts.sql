-- +goose Up
-- Range-partition job_attempts by month (on started_at) so retention is a cheap
-- DROP TABLE of an old partition instead of a bloat-generating DELETE, and so
-- autovacuum/scans stay bounded as history grows forever.
--
-- job_attempts has no inbound FKs, so it converts cleanly. The partition key
-- (started_at) must be part of every unique key, hence PK (id, started_at) and
-- UNIQUE (job_id, attempt_num, started_at). CompleteAttempt now passes started_at
-- so the UPDATE prunes to a single partition.
--
-- NOTE: the data copy below holds a lock for its duration. job_attempts is small
-- today; on a large table run this during a low-traffic window (or migrate with
-- pg_partman/online tooling). See docs/design/db-vacuum-tuning.md.

ALTER TABLE job_attempts RENAME TO job_attempts_old;

-- A table rename keeps its constraint/index names, which would collide with the
-- new table's. Free them (data stays; the copy below is a seq scan).
ALTER TABLE job_attempts_old DROP CONSTRAINT IF EXISTS job_attempts_pkey;
ALTER TABLE job_attempts_old DROP CONSTRAINT IF EXISTS job_attempts_job_id_fkey;
DROP INDEX IF EXISTS idx_attempts_job_id;

CREATE TABLE job_attempts (
    id           TEXT        NOT NULL DEFAULT gen_random_uuid()::text,
    job_id       TEXT        NOT NULL REFERENCES jobs(id),
    attempt_num  INT         NOT NULL,
    worker_id    TEXT        NOT NULL,
    started_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,
    status_code  INT,
    error        TEXT,
    duration_ms  BIGINT,
    PRIMARY KEY (id, started_at),
    UNIQUE (job_id, attempt_num, started_at)
) PARTITION BY RANGE (started_at);

-- Partitioned index (propagates to every partition).
CREATE INDEX idx_attempts_job_id ON job_attempts (job_id);

-- Monthly partitions covering existing data and ~1.5 years forward, plus a
-- DEFAULT catch-all so an insert never fails if partition rotation lags.
-- (Ongoing rotation — create next month, drop expired — is a periodic op;
-- see the retention runbook in the design doc.)
-- +goose StatementBegin
DO $$
DECLARE
    d date := date '2026-01-01';
BEGIN
    WHILE d < date '2028-01-01' LOOP
        EXECUTE format(
            'CREATE TABLE %I PARTITION OF job_attempts FOR VALUES FROM (%L) TO (%L) '
            || 'WITH (fillfactor=85, autovacuum_vacuum_scale_factor=0.05, '
            || 'autovacuum_vacuum_insert_scale_factor=0.1, autovacuum_analyze_scale_factor=0.05)',
            'job_attempts_' || to_char(d, 'YYYY_MM'), d, (d + interval '1 month')::date);
        d := (d + interval '1 month')::date;
    END LOOP;
END $$;
-- +goose StatementEnd

CREATE TABLE job_attempts_default PARTITION OF job_attempts DEFAULT
    WITH (fillfactor = 85, autovacuum_vacuum_scale_factor = 0.05);

INSERT INTO job_attempts (id, job_id, attempt_num, worker_id, started_at, completed_at, status_code, error, duration_ms)
SELECT id, job_id, attempt_num, worker_id, started_at, completed_at, status_code, error, duration_ms
FROM job_attempts_old;

DROP TABLE job_attempts_old;

-- +goose Down
ALTER TABLE job_attempts RENAME TO job_attempts_part;

CREATE TABLE job_attempts (
    id           TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    job_id       TEXT        NOT NULL REFERENCES jobs(id),
    attempt_num  INT         NOT NULL,
    worker_id    TEXT        NOT NULL,
    started_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,
    status_code  INT,
    error        TEXT,
    duration_ms  BIGINT,
    UNIQUE (job_id, attempt_num)
) WITH (fillfactor = 85, autovacuum_vacuum_scale_factor = 0.05, autovacuum_vacuum_insert_scale_factor = 0.1, autovacuum_analyze_scale_factor = 0.05);
CREATE INDEX idx_attempts_job_id ON job_attempts (job_id);

INSERT INTO job_attempts (id, job_id, attempt_num, worker_id, started_at, completed_at, status_code, error, duration_ms)
SELECT id, job_id, attempt_num, worker_id, started_at, completed_at, status_code, error, duration_ms
FROM job_attempts_part;

DROP TABLE job_attempts_part;
