package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/repository"
)

// PartitionMaintainer keeps job_attempts' monthly partitions provisioned ahead of
// time and (optionally) prunes expired ones. Creating partitions is always on;
// dropping is gated by retentionMonths (0 = never drop — attempts are billing
// records, so deletion is opt-in).
type PartitionMaintainer struct {
	repo            repository.AttemptPartitionRepository
	logger          *slog.Logger
	interval        time.Duration
	monthsAhead     int
	retentionMonths int
}

func NewPartitionMaintainer(repo repository.AttemptPartitionRepository, logger *slog.Logger, interval time.Duration, monthsAhead, retentionMonths int) *PartitionMaintainer {
	return &PartitionMaintainer{
		repo:            repo,
		logger:          logger.With("component", "partition_maintainer"),
		interval:        interval,
		monthsAhead:     monthsAhead,
		retentionMonths: retentionMonths,
	}
}

func (m *PartitionMaintainer) Start(ctx context.Context) {
	m.logger.InfoContext(ctx, "partition maintainer started",
		"interval", m.interval, "months_ahead", m.monthsAhead, "retention_months", m.retentionMonths)

	m.maintain(ctx) // run once immediately so partitions exist before the first tick

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			m.logger.InfoContext(ctx, "partition maintainer shut down")
			return
		case <-ticker.C:
			m.maintain(ctx)
		}
	}
}

func (m *PartitionMaintainer) maintain(ctx context.Context) {
	now := time.Now().UTC()

	created, err := m.repo.EnsureMonthlyPartitions(ctx, now, m.monthsAhead)
	if err != nil {
		m.logger.ErrorContext(ctx, "ensure monthly partitions", "error", err)
	} else if len(created) > 0 {
		m.logger.InfoContext(ctx, "created job_attempts partitions", "partitions", created)
	}

	if m.retentionMonths <= 0 {
		return // retention disabled — keep all history
	}
	cutoff := now.AddDate(0, -m.retentionMonths, 0)
	dropped, err := m.repo.DropPartitionsBefore(ctx, cutoff)
	if err != nil {
		m.logger.ErrorContext(ctx, "drop expired partitions", "error", err)
	} else if len(dropped) > 0 {
		// WARN: this deleted attempt history (billing records).
		m.logger.WarnContext(ctx, "dropped expired job_attempts partitions", "partitions", dropped, "retention_months", m.retentionMonths)
	}
}
