//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/infrastructure/postgres"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/testutil"
)

// seedCompletedAttempt creates a job and one completed attempt with the given
// outcome, so JobStats has a row to aggregate.
func seedCompletedAttempt(t *testing.T, jobRepo *postgres.JobRepository, attemptRepo *postgres.AttemptRepository, userID string, statusCode int, errMsg *string, durationMS int64) {
	t.Helper()
	ctx := context.Background()

	job, err := jobRepo.Create(ctx, testutil.NewJob(testutil.WithUserID(userID)))
	if err != nil {
		t.Fatalf("seed job: %v", err)
	}
	attempt, err := attemptRepo.CreateAttempt(ctx, &domain.JobAttempt{
		JobID:      job.ID,
		AttemptNum: 1,
		WorkerID:   "worker-test",
		StartedAt:  time.Now(),
	})
	if err != nil {
		t.Fatalf("seed attempt: %v", err)
	}
	sc := statusCode
	if err := attemptRepo.CompleteAttempt(ctx, attempt.ID, &sc, errMsg, durationMS); err != nil {
		t.Fatalf("complete attempt: %v", err)
	}
}

func TestStatsRepo_JobStats(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	testutil.TruncateAll(t, pool)
	userID := testutil.UserID()
	testutil.SeedUser(t, pool, userID)

	jobRepo := postgres.NewJobRepository(pool)
	attemptRepo := postgres.NewAttemptRepository(pool)
	statsRepo := postgres.NewStatsRepository(pool)
	ctx := context.Background()

	// 3 successes (2xx, no error), 1 failure (5xx), 1 failure (transport error).
	errMsg := "boom"
	seedCompletedAttempt(t, jobRepo, attemptRepo, userID, 200, nil, 100)
	seedCompletedAttempt(t, jobRepo, attemptRepo, userID, 201, nil, 200)
	seedCompletedAttempt(t, jobRepo, attemptRepo, userID, 204, nil, 300)
	seedCompletedAttempt(t, jobRepo, attemptRepo, userID, 503, &errMsg, 50)
	seedCompletedAttempt(t, jobRepo, attemptRepo, userID, 0, &errMsg, 10)

	s, err := statsRepo.JobStats(ctx, userID, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("job stats: %v", err)
	}
	if s.TotalExecutions != 5 {
		t.Errorf("total = %d, want 5", s.TotalExecutions)
	}
	if s.Succeeded != 3 {
		t.Errorf("succeeded = %d, want 3", s.Succeeded)
	}
	if s.Failed != 2 {
		t.Errorf("failed = %d, want 2", s.Failed)
	}
	if s.SuccessRate < 0.59 || s.SuccessRate > 0.61 {
		t.Errorf("success_rate = %f, want ~0.6", s.SuccessRate)
	}
	if s.AvgDurationMS <= 0 || s.P95DurationMS <= 0 {
		t.Errorf("durations should be positive: avg=%f p95=%f", s.AvgDurationMS, s.P95DurationMS)
	}
}

func TestStatsRepo_JobStats_RespectsWindow(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	testutil.TruncateAll(t, pool)
	userID := testutil.UserID()
	testutil.SeedUser(t, pool, userID)

	jobRepo := postgres.NewJobRepository(pool)
	attemptRepo := postgres.NewAttemptRepository(pool)
	statsRepo := postgres.NewStatsRepository(pool)
	ctx := context.Background()

	seedCompletedAttempt(t, jobRepo, attemptRepo, userID, 200, nil, 100)

	// A cutoff in the future excludes the just-created attempt.
	s, err := statsRepo.JobStats(ctx, userID, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("job stats: %v", err)
	}
	if s.TotalExecutions != 0 {
		t.Errorf("total = %d, want 0 (outside window)", s.TotalExecutions)
	}
}

func TestStatsRepo_UsageSeries(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	testutil.TruncateAll(t, pool)
	userID := testutil.UserID()
	testutil.SeedUser(t, pool, userID)

	creditRepo := postgres.NewCreditRepository(pool)
	statsRepo := postgres.NewStatsRepository(pool)
	ctx := context.Background()

	// 2 job executions + 1 buffer execution, all today.
	job1, _ := postgres.NewJobRepository(pool).Create(ctx, testutil.NewJob(testutil.WithUserID(userID)))
	job2, _ := postgres.NewJobRepository(pool).Create(ctx, testutil.NewJob(testutil.WithUserID(userID)))
	if err := creditRepo.Deduct(ctx, userID, job1.ID); err != nil {
		t.Fatalf("deduct job1: %v", err)
	}
	if err := creditRepo.Deduct(ctx, userID, job2.ID); err != nil {
		t.Fatalf("deduct job2: %v", err)
	}
	itemID := seedBufferItem(t, pool, userID)
	if err := creditRepo.DeductForBufferItem(ctx, userID, itemID); err != nil {
		t.Fatalf("deduct buffer item: %v", err)
	}

	buckets, err := statsRepo.UsageSeries(ctx, userID, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("usage series: %v", err)
	}

	var totalJobs, totalBuffers int64
	for _, b := range buckets {
		totalJobs += b.JobExecutions
		totalBuffers += b.BufferExecutions
	}
	if totalJobs != 2 {
		t.Errorf("job executions = %d, want 2", totalJobs)
	}
	if totalBuffers != 1 {
		t.Errorf("buffer executions = %d, want 1", totalBuffers)
	}
}
