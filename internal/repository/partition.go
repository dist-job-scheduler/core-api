package repository

import (
	"context"
	"time"
)

// AttemptPartitionRepository manages the monthly partitions of job_attempts.
// It is consumed by the scheduler's PartitionMaintainer.
type AttemptPartitionRepository interface {
	// EnsureMonthlyPartitions creates the partition for the month containing
	// `now` plus the next `monthsAhead` months, if they don't already exist.
	// Idempotent; returns the names actually created.
	EnsureMonthlyPartitions(ctx context.Context, now time.Time, monthsAhead int) ([]string, error)

	// DropPartitionsBefore drops every monthly partition whose month starts
	// strictly before the month containing `cutoff`. This deletes data, so the
	// caller gates it behind an explicit retention policy. Returns dropped names.
	DropPartitionsBefore(ctx context.Context, cutoff time.Time) ([]string, error)
}
