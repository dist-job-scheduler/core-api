package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/jackc/pgx/v5/pgxpool"
)

type AttemptRepository struct {
	pool *pgxpool.Pool
}

func NewAttemptRepository(pool *pgxpool.Pool) *AttemptRepository {
	return &AttemptRepository{pool: pool}
}

func (r *AttemptRepository) CreateAttempt(ctx context.Context, a *domain.JobAttempt) (*domain.JobAttempt, error) {
	query := `
		INSERT INTO job_attempts (job_id, attempt_num, worker_id, started_at)
		VALUES ($1, $2, $3, $4)
		RETURNING id, job_id, attempt_num, worker_id, started_at,
		          completed_at, status_code, error, duration_ms`

	row := r.pool.QueryRow(ctx, query, a.JobID, a.AttemptNum, a.WorkerID, a.StartedAt)
	return scanAttempt(row)
}

func (r *AttemptRepository) CompleteAttempt(ctx context.Context, id string, startedAt time.Time, statusCode *int, errMsg *string, durationMS int64) error {
	// started_at is the partition key — including it prunes the UPDATE to the one
	// partition holding this attempt.
	_, err := r.pool.Exec(ctx, `
		UPDATE job_attempts
		SET completed_at = NOW(),
		    status_code  = $3,
		    error        = $4,
		    duration_ms  = $5
		WHERE id = $1 AND started_at = $2`,
		id, startedAt, statusCode, errMsg, durationMS,
	)
	if err != nil {
		return fmt.Errorf("complete attempt: %w", err)
	}
	return nil
}

func (r *AttemptRepository) ListByJobID(ctx context.Context, jobID string) ([]*domain.JobAttempt, error) {
	query := `
		SELECT id, job_id, attempt_num, worker_id, started_at,
		       completed_at, status_code, error, duration_ms
		FROM job_attempts
		WHERE job_id = $1
		ORDER BY started_at ASC`

	rows, err := r.pool.Query(ctx, query, jobID)
	if err != nil {
		return nil, fmt.Errorf("list attempts: %w", err)
	}
	defer rows.Close()

	var attempts []*domain.JobAttempt
	for rows.Next() {
		a, err := scanAttempt(rows)
		if err != nil {
			return nil, err
		}
		attempts = append(attempts, a)
	}
	return attempts, nil
}

func scanAttempt(row rowScanner) (*domain.JobAttempt, error) {
	var a domain.JobAttempt
	err := row.Scan(
		&a.ID, &a.JobID, &a.AttemptNum, &a.WorkerID, &a.StartedAt,
		&a.CompletedAt, &a.StatusCode, &a.Error, &a.DurationMS,
	)
	if err != nil {
		return nil, fmt.Errorf("scan attempt: %w", err)
	}
	return &a, nil
}
