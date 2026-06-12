package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/jackc/pgx/v5/pgxpool"
)

type StatsRepository struct {
	pool *pgxpool.Pool
}

func NewStatsRepository(pool *pgxpool.Pool) *StatsRepository {
	return &StatsRepository{pool: pool}
}

// JobStats aggregates completed attempts for the user's jobs since the cutoff.
// An attempt counts as a success when it returned a 2xx with no transport error;
// everything else (non-2xx, timeout, connection error) is a failure.
func (r *StatsRepository) JobStats(ctx context.Context, userID string, since time.Time) (domain.JobStats, error) {
	var s domain.JobStats
	err := r.pool.QueryRow(ctx, `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE ja.error IS NULL AND ja.status_code BETWEEN 200 AND 299),
			COALESCE(AVG(ja.duration_ms), 0),
			COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY ja.duration_ms), 0)
		FROM job_attempts ja
		JOIN jobs j ON j.id = ja.job_id
		WHERE j.user_id = $1
		  AND ja.started_at >= $2
		  AND ja.completed_at IS NOT NULL`,
		userID, since,
	).Scan(&s.TotalExecutions, &s.Succeeded, &s.AvgDurationMS, &s.P95DurationMS)
	if err != nil {
		return domain.JobStats{}, fmt.Errorf("job stats: %w", err)
	}

	s.Failed = s.TotalExecutions - s.Succeeded
	if s.TotalExecutions > 0 {
		s.SuccessRate = float64(s.Succeeded) / float64(s.TotalExecutions)
	}
	return s, nil
}

// UsageSeries returns per-UTC-day execution counts split by execution type. Only
// days with activity appear; callers that need a dense series fill the gaps.
func (r *StatsRepository) UsageSeries(ctx context.Context, userID string, since time.Time) ([]domain.UsageBucket, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			to_char(DATE(created_at AT TIME ZONE 'UTC'), 'YYYY-MM-DD') AS d,
			COUNT(*) FILTER (WHERE type = $3) AS jobs,
			COUNT(*) FILTER (WHERE type = $4) AS buffers
		FROM credit_transactions
		WHERE user_id = $1
		  AND created_at >= $2
		  AND type IN ($3, $4)
		GROUP BY d
		ORDER BY d ASC`,
		userID, since, domain.CreditTxJobExecution, domain.CreditTxBufferExecution,
	)
	if err != nil {
		return nil, fmt.Errorf("usage series: %w", err)
	}
	defer rows.Close()

	var buckets []domain.UsageBucket
	for rows.Next() {
		var b domain.UsageBucket
		if err := rows.Scan(&b.Date, &b.JobExecutions, &b.BufferExecutions); err != nil {
			return nil, fmt.Errorf("scan usage bucket: %w", err)
		}
		buckets = append(buckets, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate usage series: %w", err)
	}
	return buckets, nil
}
