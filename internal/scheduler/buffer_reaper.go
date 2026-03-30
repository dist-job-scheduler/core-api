package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/metrics"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/repository"
)

// BufferReaper recovers buffer items stuck in "running" due to crashed drainers.
type BufferReaper struct {
	repo             repository.BufferRepository
	logger           *slog.Logger
	interval         time.Duration
	heartbeatTimeout time.Duration
}

func NewBufferReaper(repo repository.BufferRepository, logger *slog.Logger, interval time.Duration, heartbeatTimeout time.Duration) *BufferReaper {
	return &BufferReaper{
		repo:             repo,
		logger:           logger.With("component", "buffer_reaper"),
		interval:         interval,
		heartbeatTimeout: heartbeatTimeout,
	}
}

func (r *BufferReaper) Start(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	r.logger.InfoContext(ctx, "buffer reaper started", "interval", r.interval, "heartbeat_timeout", r.heartbeatTimeout)

	for {
		select {
		case <-ctx.Done():
			r.logger.InfoContext(ctx, "buffer reaper shut down")
			return
		case <-ticker.C:
			r.reap(ctx)
		}
	}
}

func (r *BufferReaper) reap(ctx context.Context) {
	start := time.Now()
	defer func() {
		metrics.BufferReaperCycleDuration.Observe(time.Since(start).Seconds())
	}()

	staleCutoff := time.Now().Add(-r.heartbeatTimeout)

	rescheduled, err := r.repo.RescheduleStaleItems(ctx, staleCutoff, 100)
	if err != nil {
		r.logger.ErrorContext(ctx, "reschedule stale buffer items", "error", err)
	} else if rescheduled > 0 {
		metrics.BufferReaperRescuedTotal.WithLabelValues("rescheduled").Add(float64(rescheduled))
		r.logger.InfoContext(ctx, "rescheduled stale buffer items", "count", rescheduled)
	}

	failed, err := r.repo.FailStaleItems(ctx, staleCutoff, 100)
	if err != nil {
		r.logger.ErrorContext(ctx, "fail stale buffer items", "error", err)
	} else if failed > 0 {
		metrics.BufferReaperRescuedTotal.WithLabelValues("failed").Add(float64(failed))
		r.logger.InfoContext(ctx, "permanently failed stale buffer items", "count", failed)
	}
}
