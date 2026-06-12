package repository

import (
	"context"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
)

// StatsRepository serves account-level analytics derived from existing tables
// (job_attempts and credit_transactions). All reads are scoped to a user.
type StatsRepository interface {
	// JobStats aggregates completed job attempts for the user since the cutoff.
	JobStats(ctx context.Context, userID string, since time.Time) (domain.JobStats, error)

	// UsageSeries returns per-UTC-day execution counts (job vs buffer) for the
	// user since the cutoff, ordered by date ascending.
	UsageSeries(ctx context.Context, userID string, since time.Time) ([]domain.UsageBucket, error)
}
