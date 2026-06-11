package usecase

import (
	"context"
	"fmt"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/repository"
)

const (
	defaultStatsDays = 30
	maxStatsDays     = 365
)

type StatsUsecase struct {
	stats   repository.StatsRepository
	credits repository.CreditRepository
}

func NewStatsUsecase(stats repository.StatsRepository, credits repository.CreditRepository) *StatsUsecase {
	return &StatsUsecase{stats: stats, credits: credits}
}

// clampDays normalizes a requested window to [1, maxStatsDays], defaulting to
// defaultStatsDays when days <= 0.
func clampDays(days int) int {
	if days <= 0 {
		return defaultStatsDays
	}
	if days > maxStatsDays {
		return maxStatsDays
	}
	return days
}

func (u *StatsUsecase) GetJobStats(ctx context.Context, userID string, days int) (domain.JobStats, error) {
	days = clampDays(days)
	since := time.Now().Add(-time.Duration(days) * 24 * time.Hour)

	s, err := u.stats.JobStats(ctx, userID, since)
	if err != nil {
		return domain.JobStats{}, fmt.Errorf("job stats: %w", err)
	}
	s.SinceDays = days
	return s, nil
}

func (u *StatsUsecase) GetUsage(ctx context.Context, userID string, days int) (domain.UsageSummary, error) {
	days = clampDays(days)
	since := time.Now().Add(-time.Duration(days) * 24 * time.Hour)

	buckets, err := u.stats.UsageSeries(ctx, userID, since)
	if err != nil {
		return domain.UsageSummary{}, fmt.Errorf("usage series: %w", err)
	}

	summary := domain.UsageSummary{Buckets: buckets, SinceDays: days}
	for _, b := range buckets {
		summary.TotalJobExecutions += b.JobExecutions
		summary.TotalBufferExecutions += b.BufferExecutions
	}

	// Current credit position is best-effort context for the usage view; a
	// missing balance row should not fail the whole report.
	if bal, err := u.credits.GetBalance(ctx, userID); err == nil {
		summary.Balance = bal.Balance
		summary.Plan = bal.Plan
	}

	return summary, nil
}
