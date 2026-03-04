package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/repository"
	"github.com/robfig/cron/v3"
)

type Dispatcher struct {
	scheduleRepo repository.ScheduleRepository
	creditRepo   repository.CreditRepository
	logger       *slog.Logger
	interval     time.Duration
}

func NewDispatcher(repo repository.ScheduleRepository, credits repository.CreditRepository, logger *slog.Logger, interval time.Duration) *Dispatcher {
	return &Dispatcher{
		scheduleRepo: repo,
		creditRepo:   credits,
		logger:       logger.With("component", "dispatcher"),
		interval:     interval,
	}
}

func (d *Dispatcher) Start(ctx context.Context) {
	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()

	d.logger.Info("dispatcher started", "interval", d.interval)

	for {
		select {
		case <-ctx.Done():
			d.logger.Info("dispatcher shut down")
			return
		case <-ticker.C:
			d.dispatch(ctx)
		}
	}
}

func (d *Dispatcher) dispatch(ctx context.Context) {
	creditFilter := func(filterCtx context.Context, userID string) bool {
		ok, err := d.creditRepo.HasCredits(filterCtx, userID)
		if err != nil {
			d.logger.WarnContext(filterCtx, "credit check error in dispatcher, allowing job", "user_id", userID, "error", err)
			return true // fail open: prefer firing over silently dropping
		}
		return ok
	}

	jobs, err := d.scheduleRepo.ClaimAndFire(ctx, 100, d.computeNext, creditFilter)
	if err != nil {
		d.logger.Error("dispatcher claim and fire", "error", err)
		return
	}
	if len(jobs) > 0 {
		d.logger.Info("dispatcher fired jobs", "count", len(jobs))
	}
}

// computeNext returns the next future run time for the schedule, skipping any missed runs.
func (d *Dispatcher) computeNext(s *domain.Schedule) time.Time {
	sched, err := cron.ParseStandard(s.CronExpr)
	if err != nil {
		// Expression was validated on create; this should never happen.
		d.logger.Error("invalid cron expression in schedule", "schedule_id", s.ID, "cron_expr", s.CronExpr, "error", err)
		return time.Now().Add(time.Hour) // safe fallback
	}

	next := sched.Next(s.NextRunAt)
	now := time.Now()
	for next.Before(now) {
		next = sched.Next(next)
	}
	return next
}
