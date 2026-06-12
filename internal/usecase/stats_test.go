package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/testutil"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/usecase"
)

func TestGetJobStats_DefaultsAndSinceDays(t *testing.T) {
	t.Parallel()

	var gotSince time.Time
	repo := &testutil.MockStatsRepository{
		JobStatsFn: func(_ context.Context, _ string, since time.Time) (domain.JobStats, error) {
			gotSince = since
			return domain.JobStats{TotalExecutions: 10, Succeeded: 7, Failed: 3, SuccessRate: 0.7}, nil
		},
	}
	uc := usecase.NewStatsUsecase(repo, &testutil.MockCreditRepository{})

	// days <= 0 should default to 30.
	got, err := uc.GetJobStats(context.Background(), "user-1", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.SinceDays != 30 {
		t.Errorf("SinceDays = %d, want 30", got.SinceDays)
	}
	if d := time.Since(gotSince); d < 29*24*time.Hour || d > 31*24*time.Hour {
		t.Errorf("since cutoff ~%v ago, want ~30 days", d)
	}
}

func TestGetJobStats_ClampsToMax(t *testing.T) {
	t.Parallel()

	repo := &testutil.MockStatsRepository{
		JobStatsFn: func(_ context.Context, _ string, _ time.Time) (domain.JobStats, error) {
			return domain.JobStats{}, nil
		},
	}
	uc := usecase.NewStatsUsecase(repo, &testutil.MockCreditRepository{})

	got, err := uc.GetJobStats(context.Background(), "user-1", 100000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.SinceDays != 365 {
		t.Errorf("SinceDays = %d, want clamped to 365", got.SinceDays)
	}
}

func TestGetUsage_SumsTotalsAndAttachesBalance(t *testing.T) {
	t.Parallel()

	repo := &testutil.MockStatsRepository{
		UsageSeriesFn: func(_ context.Context, _ string, _ time.Time) ([]domain.UsageBucket, error) {
			return []domain.UsageBucket{
				{Date: "2026-06-10", JobExecutions: 3, BufferExecutions: 5},
				{Date: "2026-06-11", JobExecutions: 2, BufferExecutions: 1},
			}, nil
		},
	}
	credits := &testutil.MockCreditRepository{
		GetBalanceFn: func(_ context.Context, uid string) (*domain.CreditBalance, error) {
			return &domain.CreditBalance{UserID: uid, Balance: 4242, Plan: domain.PlanPaid}, nil
		},
	}
	uc := usecase.NewStatsUsecase(repo, credits)

	got, err := uc.GetUsage(context.Background(), "user-1", 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.TotalJobExecutions != 5 {
		t.Errorf("TotalJobExecutions = %d, want 5", got.TotalJobExecutions)
	}
	if got.TotalBufferExecutions != 6 {
		t.Errorf("TotalBufferExecutions = %d, want 6", got.TotalBufferExecutions)
	}
	if got.Balance != 4242 || got.Plan != domain.PlanPaid {
		t.Errorf("balance/plan = %d/%s, want 4242/paid", got.Balance, got.Plan)
	}
	if got.SinceDays != 7 {
		t.Errorf("SinceDays = %d, want 7", got.SinceDays)
	}
}

func TestGetUsage_BalanceErrorTolerated(t *testing.T) {
	t.Parallel()

	repo := &testutil.MockStatsRepository{
		UsageSeriesFn: func(_ context.Context, _ string, _ time.Time) ([]domain.UsageBucket, error) {
			return []domain.UsageBucket{{Date: "2026-06-11", JobExecutions: 1}}, nil
		},
	}
	credits := &testutil.MockCreditRepository{
		GetBalanceFn: func(_ context.Context, _ string) (*domain.CreditBalance, error) {
			return nil, errors.New("no balance row")
		},
	}
	uc := usecase.NewStatsUsecase(repo, credits)

	got, err := uc.GetUsage(context.Background(), "user-1", 7)
	if err != nil {
		t.Fatalf("usage should not fail when balance is unavailable: %v", err)
	}
	if got.TotalJobExecutions != 1 {
		t.Errorf("TotalJobExecutions = %d, want 1", got.TotalJobExecutions)
	}
	if got.Balance != 0 {
		t.Errorf("Balance = %d, want 0 when unavailable", got.Balance)
	}
}

func TestGetBufferStats_OwnershipChecked(t *testing.T) {
	t.Parallel()

	bufRepo := &testutil.MockBufferRepository{
		GetByIDFn: func(_ context.Context, _, _ string) (*domain.Buffer, error) {
			return nil, domain.ErrBufferNotFound
		},
		BufferStatsFn: func(_ context.Context, _ string) (domain.BufferStats, error) {
			t.Fatal("BufferStats must not be called when ownership check fails")
			return domain.BufferStats{}, nil
		},
	}
	uc := usecase.NewBufferUsecase(bufRepo, &testutil.MockCreditRepository{})

	_, err := uc.GetBufferStats(context.Background(), "buf-1", "user-1")
	if !errors.Is(err, domain.ErrBufferNotFound) {
		t.Errorf("err = %v, want ErrBufferNotFound", err)
	}
}

func TestGetBufferStats_HappyPath(t *testing.T) {
	t.Parallel()

	bufRepo := &testutil.MockBufferRepository{
		GetByIDFn: func(_ context.Context, _, _ string) (*domain.Buffer, error) {
			return &domain.Buffer{ID: "buf-1", UserID: "user-1"}, nil
		},
		BufferStatsFn: func(_ context.Context, _ string) (domain.BufferStats, error) {
			return domain.BufferStats{Completed: 9, Failed: 1, Total: 10, SuccessRate: 0.9}, nil
		},
	}
	uc := usecase.NewBufferUsecase(bufRepo, &testutil.MockCreditRepository{})

	got, err := uc.GetBufferStats(context.Background(), "buf-1", "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Total != 10 || got.SuccessRate != 0.9 {
		t.Errorf("stats = %+v, want total 10 / rate 0.9", got)
	}
}
