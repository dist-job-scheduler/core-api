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

func TestJobAttempts_IsRangePartitioned(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	var relkind string
	if err := pool.QueryRow(ctx,
		`SELECT relkind::text FROM pg_class WHERE relname = 'job_attempts'`).Scan(&relkind); err != nil {
		t.Fatalf("query relkind: %v", err)
	}
	if relkind != "p" { // 'p' = partitioned table
		t.Fatalf("job_attempts relkind = %q, want 'p' (partitioned)", relkind)
	}
}

// An attempt's row lands in the monthly partition for its started_at, and the
// completion UPDATE (which now passes started_at) finds and updates it.
func TestJobAttempts_RowRoutesToMonthlyPartition(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	testutil.TruncateAll(t, pool)
	userID := testutil.UserID()
	testutil.SeedUser(t, pool, userID)
	ctx := context.Background()

	jobRepo := postgres.NewJobRepository(pool)
	attempts := postgres.NewAttemptRepository(pool)

	job, err := jobRepo.Create(ctx, testutil.NewJob(testutil.WithUserID(userID)))
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	started := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC) // -> job_attempts_2026_03
	a, err := attempts.CreateAttempt(ctx, &domain.JobAttempt{
		JobID: job.ID, AttemptNum: 1, WorkerID: "w1", StartedAt: started,
	})
	if err != nil {
		t.Fatalf("create attempt: %v", err)
	}

	var part string
	if err := pool.QueryRow(ctx,
		`SELECT tableoid::regclass::text FROM job_attempts WHERE id = $1 AND started_at = $2`,
		a.ID, a.StartedAt).Scan(&part); err != nil {
		t.Fatalf("locate partition: %v", err)
	}
	if part != "job_attempts_2026_03" {
		t.Fatalf("row landed in %q, want job_attempts_2026_03", part)
	}

	sc := 200
	if err := attempts.CompleteAttempt(ctx, a.ID, a.StartedAt, &sc, nil, 42); err != nil {
		t.Fatalf("complete attempt: %v", err)
	}
	got, err := attempts.ListByJobID(ctx, job.ID)
	if err != nil || len(got) != 1 || got[0].CompletedAt == nil {
		t.Fatalf("expected 1 completed attempt, got %+v err=%v", got, err)
	}
}

// Retention: dropping an old partition removes its rows with no DELETE/vacuum.
func TestJobAttempts_DropPartitionRetention(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	testutil.TruncateAll(t, pool)
	userID := testutil.UserID()
	testutil.SeedUser(t, pool, userID)
	ctx := context.Background()

	jobRepo := postgres.NewJobRepository(pool)
	attempts := postgres.NewAttemptRepository(pool)
	job, err := jobRepo.Create(ctx, testutil.NewJob(testutil.WithUserID(userID)))
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	// A dedicated far-future partition so we never disturb the migration's set.
	if _, err := pool.Exec(ctx,
		`CREATE TABLE IF NOT EXISTS job_attempts_test_2031_01 PARTITION OF job_attempts
		 FOR VALUES FROM ('2031-01-01') TO ('2031-02-01')`); err != nil {
		t.Fatalf("create test partition: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DROP TABLE IF EXISTS job_attempts_test_2031_01`) })

	if _, err := attempts.CreateAttempt(ctx, &domain.JobAttempt{
		JobID: job.ID, AttemptNum: 1, WorkerID: "w1", StartedAt: time.Date(2031, 1, 10, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("create attempt: %v", err)
	}
	if got, _ := attempts.ListByJobID(ctx, job.ID); len(got) != 1 {
		t.Fatalf("expected 1 attempt before drop, got %d", len(got))
	}

	// Retention = drop the whole partition (instant, no dead tuples).
	if _, err := pool.Exec(ctx, `DROP TABLE job_attempts_test_2031_01`); err != nil {
		t.Fatalf("drop partition: %v", err)
	}
	if got, _ := attempts.ListByJobID(ctx, job.ID); len(got) != 0 {
		t.Fatalf("expected 0 attempts after dropping the partition, got %d", len(got))
	}
}
