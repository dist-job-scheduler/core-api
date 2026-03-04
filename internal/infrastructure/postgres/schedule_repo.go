package postgres

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/repository"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ScheduleRepository struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
}

func NewScheduleRepository(pool *pgxpool.Pool, logger *slog.Logger) *ScheduleRepository {
	return &ScheduleRepository{pool: pool, logger: logger.With("component", "schedule_repo")}
}

func (r *ScheduleRepository) Create(ctx context.Context, s *domain.Schedule) (*domain.Schedule, error) {
	query := `
		INSERT INTO schedules (
			user_id, name, cron_expr, url, method, headers, body,
			timeout_seconds, max_retries, backoff, paused, next_run_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING id, user_id, name, cron_expr, url, method, headers, body,
		          timeout_seconds, max_retries, backoff, paused,
		          next_run_at, last_run_at, created_at, updated_at`

	row := r.pool.QueryRow(ctx, query,
		s.UserID, s.Name, s.CronExpr, s.URL, s.Method, s.Headers, s.Body,
		s.TimeoutSeconds, s.MaxRetries, s.Backoff, s.Paused, s.NextRunAt,
	)

	created, err := scanSchedule(row)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, domain.ErrScheduleNameConflict
		}
		return nil, err
	}
	return created, nil
}

func (r *ScheduleRepository) GetByID(ctx context.Context, id, userID string) (*domain.Schedule, error) {
	query := `
		SELECT id, user_id, name, cron_expr, url, method, headers, body,
		       timeout_seconds, max_retries, backoff, paused,
		       next_run_at, last_run_at, created_at, updated_at
		FROM schedules
		WHERE id = $1 AND user_id = $2`

	row := r.pool.QueryRow(ctx, query, id, userID)
	return scanSchedule(row)
}

func (r *ScheduleRepository) List(ctx context.Context, input repository.ListSchedulesInput) ([]*domain.Schedule, error) {
	args := []any{input.UserID}
	where := []string{"user_id = $1"}

	if input.CursorTime != nil {
		args = append(args, *input.CursorTime, input.CursorID)
		where = append(where, fmt.Sprintf("(created_at, id) < ($%d, $%d)", len(args)-1, len(args)))
	}
	args = append(args, input.Limit)

	query := fmt.Sprintf(`
		SELECT id, user_id, name, cron_expr, url, method, headers, body,
		       timeout_seconds, max_retries, backoff, paused,
		       next_run_at, last_run_at, created_at, updated_at
		FROM schedules
		WHERE %s
		ORDER BY created_at DESC, id DESC
		LIMIT $%d`,
		strings.Join(where, " AND "), len(args))

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list schedules: %w", err)
	}
	defer rows.Close()

	var schedules []*domain.Schedule
	for rows.Next() {
		s, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		schedules = append(schedules, s)
	}
	return schedules, nil
}

func (r *ScheduleRepository) SetPaused(ctx context.Context, id, userID string, paused bool) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE schedules SET paused = $3, updated_at = NOW()
		 WHERE id = $1 AND user_id = $2 AND paused = $4`,
		id, userID, paused, !paused)
	if err != nil {
		return fmt.Errorf("set paused: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Distinguish not-found vs already-in-desired-state
		if _, err := r.GetByID(ctx, id, userID); err != nil {
			return err // ErrScheduleNotFound
		}
		if paused {
			return domain.ErrScheduleAlreadyPaused
		}
		return domain.ErrScheduleNotPaused
	}
	return nil
}

func (r *ScheduleRepository) Delete(ctx context.Context, id, userID string) error {
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM schedules WHERE id = $1 AND user_id = $2`,
		id, userID)
	if err != nil {
		return fmt.Errorf("delete schedule: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrScheduleNotFound
	}
	return nil
}

// ClaimAndFire atomically claims due schedules, inserts a job for each, and advances next_run_at.
// All operations happen in a single transaction — no partial state on crash.
// creditFilter is evaluated per schedule; if it returns false the job is skipped but
// next_run_at is still advanced so the schedule is not stuck.
func (r *ScheduleRepository) ClaimAndFire(ctx context.Context, limit int, computeNext func(*domain.Schedule) time.Time, creditFilter func(ctx context.Context, userID string) bool) ([]*domain.Job, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	// Claim due schedules — FOR UPDATE SKIP LOCKED prevents double-firing across replicas.
	rows, err := tx.Query(ctx, `
		SELECT id, user_id, name, cron_expr, url, method, headers, body,
		       timeout_seconds, max_retries, backoff, paused,
		       next_run_at, last_run_at, created_at, updated_at
		FROM schedules
		WHERE next_run_at <= NOW() AND NOT paused
		ORDER BY next_run_at ASC
		LIMIT $1
		FOR UPDATE SKIP LOCKED`, limit)
	if err != nil {
		return nil, fmt.Errorf("claim schedules: %w", err)
	}

	var schedules []*domain.Schedule
	for rows.Next() {
		s, scanErr := scanSchedule(rows)
		if scanErr != nil {
			rows.Close()
			return nil, scanErr
		}
		schedules = append(schedules, s)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate schedules: %w", err)
	}

	var firedJobs []*domain.Job

	for _, s := range schedules {
		next := computeNext(s)
		idempotencyKey := fmt.Sprintf("sched:%s:%d", s.ID, s.NextRunAt.Unix())

		// Check credits before inserting. creditFilter uses a separate connection
		// (not this tx), so it is non-transactional but safe for a best-effort gate.
		if !creditFilter(ctx, s.UserID) {
			r.logger.WarnContext(ctx, "skipping cron job: insufficient credits",
				"schedule_id", s.ID,
				"user_id", s.UserID,
			)
			// Still advance next_run_at so the schedule progresses.
			if _, updateErr := tx.Exec(ctx,
				`UPDATE schedules SET next_run_at = $2, last_run_at = NOW(), updated_at = NOW() WHERE id = $1`,
				s.ID, next,
			); updateErr != nil {
				return nil, fmt.Errorf("advance schedule %s: %w", s.ID, updateErr)
			}
			continue
		}

		// Insert the job — idempotency key guards against any edge-case duplicate fire.
		var j domain.Job
		scanErr := tx.QueryRow(ctx, `
			INSERT INTO jobs (
				user_id, idempotency_key, url, method, headers, body,
				timeout_seconds, status, scheduled_at, max_retries, backoff, schedule_id
			) VALUES ($1, $2, $3, $4, $5, $6, $7, 'pending', NOW(), $8, $9, $10)
			RETURNING id, user_id, idempotency_key, url, method, headers, body,
			          timeout_seconds, status, scheduled_at, retry_count,
			          max_retries, backoff, claimed_at, claimed_by,
			          heartbeat_at, completed_at, last_error, created_at, updated_at, schedule_id`,
			s.UserID, idempotencyKey, s.URL, s.Method, s.Headers, s.Body,
			s.TimeoutSeconds, s.MaxRetries, s.Backoff, s.ID,
		).Scan(
			&j.ID, &j.UserID, &j.IdempotencyKey, &j.URL, &j.Method, &j.Headers, &j.Body,
			&j.TimeoutSeconds, &j.Status, &j.ScheduledAt, &j.RetryCount,
			&j.MaxRetries, &j.Backoff, &j.ClaimedAt, &j.ClaimedBy,
			&j.HeartbeatAt, &j.CompletedAt, &j.LastError, &j.CreatedAt, &j.UpdatedAt,
			&j.ScheduleID,
		)
		if scanErr != nil {
			var pgErr *pgconn.PgError
			if errors.As(scanErr, &pgErr) && pgErr.Code == "23505" {
				// Duplicate idempotency key — should never happen with SKIP LOCKED, but handle gracefully.
				r.logger.Warn("duplicate job for schedule, skipping",
					"schedule_id", s.ID,
					"idempotency_key", idempotencyKey,
				)
				// Still advance next_run_at so the schedule progresses.
			} else {
				return nil, fmt.Errorf("insert job for schedule %s: %w", s.ID, scanErr)
			}
		} else {
			firedJobs = append(firedJobs, &j)
		}

		// Advance next_run_at and record last_run_at.
		if _, updateErr := tx.Exec(ctx,
			`UPDATE schedules SET next_run_at = $2, last_run_at = NOW(), updated_at = NOW() WHERE id = $1`,
			s.ID, next,
		); updateErr != nil {
			return nil, fmt.Errorf("advance schedule %s: %w", s.ID, updateErr)
		}
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return firedJobs, nil
}

func scanSchedule(row rowScanner) (*domain.Schedule, error) {
	var s domain.Schedule
	err := row.Scan(
		&s.ID, &s.UserID, &s.Name, &s.CronExpr, &s.URL, &s.Method, &s.Headers, &s.Body,
		&s.TimeoutSeconds, &s.MaxRetries, &s.Backoff, &s.Paused,
		&s.NextRunAt, &s.LastRunAt, &s.CreatedAt, &s.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrScheduleNotFound
		}
		return nil, fmt.Errorf("scan schedule: %w", err)
	}
	return &s, nil
}
