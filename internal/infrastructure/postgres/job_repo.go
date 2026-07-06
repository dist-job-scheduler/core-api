package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/repository"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type JobRepository struct {
	pool *pgxpool.Pool
}

func NewJobRepository(pool *pgxpool.Pool) *JobRepository {
	return &JobRepository{pool: pool}
}

func (r *JobRepository) Create(ctx context.Context, job *domain.Job) (*domain.Job, error) {
	query := `
		INSERT INTO jobs (
			user_id, idempotency_key, url, method, headers, body,
			timeout_seconds, status, scheduled_at, max_retries, backoff, schedule_id,
			webhook_url, webhook_headers, replay_of
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		RETURNING id, user_id, idempotency_key, url, method, headers, body,
		          timeout_seconds, status, scheduled_at, retry_count,
		          max_retries, backoff, claimed_at, claimed_by,
		          heartbeat_at, completed_at, last_error, created_at, updated_at, schedule_id,
		          webhook_url, webhook_headers, replay_of`

	row := r.pool.QueryRow(ctx, query,
		job.UserID,
		job.IdempotencyKey,
		job.URL,
		job.Method,
		job.Headers,
		job.Body,
		job.TimeoutSeconds,
		job.Status,
		job.ScheduledAt,
		job.MaxRetries,
		job.Backoff,
		job.ScheduleID,
		job.WebhookURL,
		job.WebhookHeaders,
		job.ReplayOf,
	)

	created, err := scanJob(row)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, domain.ErrDuplicateJob
		}
		return nil, err
	}
	return created, nil
}

func (r *JobRepository) GetByID(ctx context.Context, id, userID string) (*domain.Job, error) {
	query := `
		SELECT id, user_id, idempotency_key, url, method, headers, body,
		       timeout_seconds, status, scheduled_at, retry_count,
		       max_retries, backoff, claimed_at, claimed_by,
		       heartbeat_at, completed_at, last_error, created_at, updated_at, schedule_id,
		       webhook_url, webhook_headers, replay_of
		FROM jobs
		WHERE id = $1 AND user_id = $2`

	row := r.pool.QueryRow(ctx, query, id, userID)
	return scanJob(row)
}

func (r *JobRepository) Claim(ctx context.Context, workerID string, limit int) ([]*domain.Job, error) {
	// FOR UPDATE SKIP LOCKED prevents double-execution across workers.
	// The `hb` CTE seeds the heartbeat sidecar atomically with the claim so a
	// freshly-running job always has a heartbeat row (the reaper relies on it).
	query := `
		WITH claimed AS (
			UPDATE jobs
			SET    status       = 'running',
			       claimed_at   = NOW(),
			       claimed_by   = $1,
			       heartbeat_at = NOW(),
			       updated_at   = NOW()
			WHERE id IN (
				SELECT id FROM jobs
				WHERE  status       = 'pending'
				  AND  scheduled_at <= NOW()
				ORDER BY scheduled_at ASC
				LIMIT $2
				FOR UPDATE SKIP LOCKED
			)
			RETURNING id, user_id, idempotency_key, url, method, headers, body,
			          timeout_seconds, status, scheduled_at, retry_count,
			          max_retries, backoff, claimed_at, claimed_by,
			          heartbeat_at, completed_at, last_error, created_at, updated_at, schedule_id,
			          webhook_url, webhook_headers, replay_of
		), hb AS (
			INSERT INTO job_heartbeats (job_id, heartbeat_at)
			SELECT id, NOW() FROM claimed
			ON CONFLICT (job_id) DO UPDATE SET heartbeat_at = EXCLUDED.heartbeat_at
		)
		SELECT id, user_id, idempotency_key, url, method, headers, body,
		       timeout_seconds, status, scheduled_at, retry_count,
		       max_retries, backoff, claimed_at, claimed_by,
		       heartbeat_at, completed_at, last_error, created_at, updated_at, schedule_id,
		       webhook_url, webhook_headers, replay_of
		FROM claimed`

	rows, err := r.pool.Query(ctx, query, workerID, limit)
	if err != nil {
		return nil, fmt.Errorf("claim jobs: %w", err)
	}
	defer rows.Close()

	var jobs []*domain.Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, nil
}

func (r *JobRepository) UpdateHeartbeat(ctx context.Context, jobID string) error {
	// Writes only the tiny sidecar row (a HOT update — no index churn, no rewrite
	// of the heavy jobs row). Guarded so we never heartbeat a job that has left
	// 'running'; upsert so a missing row self-heals. See migration 20260614000001.
	_, err := r.pool.Exec(ctx,
		`INSERT INTO job_heartbeats (job_id, heartbeat_at)
		 SELECT $1, NOW()
		 WHERE EXISTS (SELECT 1 FROM jobs WHERE id = $1 AND status = 'running')
		 ON CONFLICT (job_id) DO UPDATE SET heartbeat_at = EXCLUDED.heartbeat_at`, jobID)
	return err
}

func (r *JobRepository) Complete(ctx context.Context, jobID string) error {
	return completeJob(ctx, r.pool, jobID)
}

func (r *JobRepository) Fail(ctx context.Context, jobID string, lastError string) error {
	return failJob(ctx, r.pool, jobID, lastError)
}

// CompleteWithWebhook marks the job completed and, when delivery is non-nil, inserts
// the webhook delivery row in the SAME transaction. This closes the outbox gap: a
// crash can never leave a terminal job without its outbound delivery — the
// at-least-once guarantee begins at commit, not after a second write. A nil
// delivery (job had no webhook_url) behaves exactly like Complete.
func (r *JobRepository) CompleteWithWebhook(ctx context.Context, jobID string, delivery *domain.WebhookDelivery) error {
	return r.terminalWithWebhook(ctx, delivery, func(q querier) error {
		return completeJob(ctx, q, jobID)
	})
}

// FailWithWebhook is CompleteWithWebhook for a permanent failure.
func (r *JobRepository) FailWithWebhook(ctx context.Context, jobID string, lastError string, delivery *domain.WebhookDelivery) error {
	return r.terminalWithWebhook(ctx, delivery, func(q querier) error {
		return failJob(ctx, q, jobID, lastError)
	})
}

// terminalWithWebhook runs the job's terminal transition and (optionally) the
// delivery insert inside one transaction, so both commit or neither does.
func (r *JobRepository) terminalWithWebhook(ctx context.Context, delivery *domain.WebhookDelivery, terminal func(querier) error) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := terminal(tx); err != nil {
		return err
	}
	if delivery != nil {
		if _, err := insertWebhookDelivery(ctx, tx, delivery); err != nil {
			return fmt.Errorf("enqueue webhook delivery: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

// completeJob leaves 'running' → drop the heartbeat sidecar row so it stays tiny.
func completeJob(ctx context.Context, q querier, jobID string) error {
	_, err := q.Exec(ctx,
		`WITH done AS (
			UPDATE jobs SET status = 'completed', completed_at = NOW(), updated_at = NOW()
			WHERE id = $1 RETURNING id
		 )
		 DELETE FROM job_heartbeats WHERE job_id IN (SELECT id FROM done)`, jobID)
	return err
}

func failJob(ctx context.Context, q querier, jobID, lastError string) error {
	_, err := q.Exec(ctx,
		`WITH done AS (
			UPDATE jobs SET status = 'failed', last_error = $2, updated_at = NOW()
			WHERE id = $1 RETURNING id
		 )
		 DELETE FROM job_heartbeats WHERE job_id IN (SELECT id FROM done)`, jobID, lastError)
	return err
}

func (r *JobRepository) Reschedule(ctx context.Context, jobID string, lastError string, retryAt time.Time) error {
	// make sure that retry_count is not over-incremented due to multiple workers trying to re-schedule same jobs
	_, err := r.pool.Exec(ctx,
		`WITH done AS (
			UPDATE jobs
			SET    status       = 'pending',
			       retry_count  = retry_count + 1,
			       last_error   = $2,
			       scheduled_at = $3,
			       claimed_at   = NULL,
			       claimed_by   = NULL,
			       heartbeat_at = NULL,
			       updated_at   = NOW()
			WHERE id = $1 RETURNING id
		 )
		 DELETE FROM job_heartbeats WHERE job_id IN (SELECT id FROM done)`, jobID, lastError, retryAt)
	return err
}

func (r *JobRepository) RescheduleStale(ctx context.Context, staleCutoff time.Time, limit int) (int, error) {
	// Staleness comes from the sidecar. A running job whose heartbeat row is
	// missing (h.heartbeat_at IS NULL) is treated as stale too — defensive.
	var n int
	err := r.pool.QueryRow(ctx, `
		WITH stale AS (
			SELECT j.id FROM jobs j
			LEFT JOIN job_heartbeats h ON h.job_id = j.id
			WHERE  j.status      = 'running'
			  AND  (h.heartbeat_at IS NULL OR h.heartbeat_at < $1)
			  AND  j.retry_count  < j.max_retries
			ORDER BY h.heartbeat_at ASC NULLS FIRST
			LIMIT $2
			FOR UPDATE OF j SKIP LOCKED
		), upd AS (
			UPDATE jobs
			SET    status       = 'pending',
			       retry_count  = retry_count + 1,
			       last_error   = 'worker timeout',
			       claimed_at   = NULL,
			       claimed_by   = NULL,
			       heartbeat_at = NULL,
			       updated_at   = NOW()
			WHERE id IN (SELECT id FROM stale)
			RETURNING id
		), del AS (
			DELETE FROM job_heartbeats WHERE job_id IN (SELECT id FROM upd)
		)
		SELECT count(*) FROM upd`, staleCutoff, limit).Scan(&n)
	return n, err
}

func (r *JobRepository) FailStale(ctx context.Context, staleCutoff time.Time, limit int) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `
		WITH stale AS (
			SELECT j.id FROM jobs j
			LEFT JOIN job_heartbeats h ON h.job_id = j.id
			WHERE  j.status      = 'running'
			  AND  (h.heartbeat_at IS NULL OR h.heartbeat_at < $1)
			  AND  j.retry_count  >= j.max_retries
			ORDER BY h.heartbeat_at ASC NULLS FIRST
			LIMIT $2
			FOR UPDATE OF j SKIP LOCKED
		), upd AS (
			UPDATE jobs
			SET    status      = 'failed',
			       last_error  = 'worker timeout: max retries exceeded',
			       updated_at  = NOW()
			WHERE id IN (SELECT id FROM stale)
			RETURNING id
		), del AS (
			DELETE FROM job_heartbeats WHERE job_id IN (SELECT id FROM upd)
		)
		SELECT count(*) FROM upd`, staleCutoff, limit).Scan(&n)
	return n, err
}

func (r *JobRepository) Cancel(ctx context.Context, jobID, userID string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE jobs SET status = 'cancelled', updated_at = NOW()
		WHERE id = $1 AND user_id = $2 AND status = 'pending'`,
		jobID, userID)
	if err != nil {
		return fmt.Errorf("cancel job: %w", err)
	}
	if tag.RowsAffected() == 0 {
		if _, err := r.GetByID(ctx, jobID, userID); err != nil {
			return err // ErrJobNotFound
		}
		return domain.ErrJobNotCancellable
	}
	return nil
}

func (r *JobRepository) ListJobs(ctx context.Context, input repository.ListJobsInput) ([]*domain.Job, error) {
	args := []any{input.UserID}
	where := []string{"user_id = $1"}

	if input.Status != "" {
		args = append(args, input.Status)
		where = append(where, fmt.Sprintf("status = $%d", len(args)))
	}
	if input.Method != "" {
		args = append(args, input.Method)
		where = append(where, fmt.Sprintf("method = $%d", len(args)))
	}
	if input.URLSearch != "" {
		args = append(args, "%"+escapeLike(input.URLSearch)+"%")
		where = append(where, fmt.Sprintf("url ILIKE $%d", len(args)))
	}
	if input.ScheduledAfter != nil {
		args = append(args, *input.ScheduledAfter)
		where = append(where, fmt.Sprintf("scheduled_at >= $%d", len(args)))
	}
	if input.ScheduledBefore != nil {
		args = append(args, *input.ScheduledBefore)
		where = append(where, fmt.Sprintf("scheduled_at <= $%d", len(args)))
	}
	if input.CursorTime != nil {
		args = append(args, *input.CursorTime, input.CursorID)
		where = append(where, fmt.Sprintf("(scheduled_at, id) < ($%d, $%d)", len(args)-1, len(args)))
	}
	args = append(args, input.Limit)

	query := fmt.Sprintf(`
		SELECT id, user_id, idempotency_key, url, method, headers, body,
		       timeout_seconds, status, scheduled_at, retry_count,
		       max_retries, backoff, claimed_at, claimed_by,
		       heartbeat_at, completed_at, last_error, created_at, updated_at, schedule_id,
		       webhook_url, webhook_headers, replay_of
		FROM jobs
		WHERE %s
		ORDER BY scheduled_at DESC, id DESC
		LIMIT $%d`,
		strings.Join(where, " AND "), len(args))

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	defer rows.Close()

	var jobs []*domain.Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, nil
}

// escapeLike escapes the LIKE/ILIKE wildcard metacharacters so that a
// user-supplied search term is matched literally (the surrounding %...% is the
// only wildcarding we want). Backslash is the default ILIKE escape character.
func escapeLike(s string) string {
	return likeEscaper.Replace(s)
}

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// pgx.Row and pgx.Rows both implement this.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanJob is a private helper — avoids repeating Scan calls across multiple queries.
func scanJob(row rowScanner) (*domain.Job, error) {
	var j domain.Job
	err := row.Scan(
		&j.ID, &j.UserID, &j.IdempotencyKey, &j.URL, &j.Method, &j.Headers, &j.Body,
		&j.TimeoutSeconds, &j.Status, &j.ScheduledAt, &j.RetryCount,
		&j.MaxRetries, &j.Backoff, &j.ClaimedAt, &j.ClaimedBy,
		&j.HeartbeatAt, &j.CompletedAt, &j.LastError, &j.CreatedAt, &j.UpdatedAt,
		&j.ScheduleID,
		&j.WebhookURL, &j.WebhookHeaders, &j.ReplayOf,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrJobNotFound
		}
		return nil, fmt.Errorf("scan job: %w", err)
	}
	return &j, nil
}

func (r *JobRepository) ListByScheduleID(ctx context.Context, scheduleID string, limit int, cursorTime *time.Time, cursorID string) ([]*domain.Job, error) {
	args := []any{scheduleID}
	where := []string{"schedule_id = $1"}

	if cursorTime != nil {
		args = append(args, *cursorTime, cursorID)
		where = append(where, fmt.Sprintf("(scheduled_at, id) < ($%d, $%d)", len(args)-1, len(args)))
	}
	args = append(args, limit)

	query := fmt.Sprintf(`
		SELECT id, user_id, idempotency_key, url, method, headers, body,
		       timeout_seconds, status, scheduled_at, retry_count,
		       max_retries, backoff, claimed_at, claimed_by,
		       heartbeat_at, completed_at, last_error, created_at, updated_at, schedule_id,
		       webhook_url, webhook_headers, replay_of
		FROM jobs
		WHERE %s
		ORDER BY scheduled_at DESC, id DESC
		LIMIT $%d`,
		strings.Join(where, " AND "), len(args))

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list jobs by schedule id: %w", err)
	}
	defer rows.Close()

	var jobs []*domain.Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, nil
}
