//go:build integration

package postgres_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/infrastructure/postgres"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Benchmarks for the scheduler's hot database paths. They exist as a regression
// guard for DB-layer optimization work (vacuum/storage tuning, the heartbeat
// sidecar, etc.): run before and after a change and compare.
//
//	DATABASE_URL=postgres://... go test -tags integration -run='^$' \
//	    -bench=. -benchmem ./internal/infrastructure/postgres/

func benchPoolUser(b *testing.B) (*pgxpool.Pool, string) {
	b.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		b.Skip("TEST_DATABASE_URL or DATABASE_URL not set, skipping benchmark")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		b.Fatalf("connect: %v", err)
	}
	b.Cleanup(pool.Close)

	userID := "bench-" + uuid.NewString()
	if _, err := pool.Exec(ctx, `INSERT INTO users (id) VALUES ($1) ON CONFLICT DO NOTHING`, userID); err != nil {
		b.Fatalf("seed user: %v", err)
	}
	return pool, userID
}

// createRunningJob inserts a job and claims it so it is in 'running' state.
func createRunningJob(b *testing.B, repo *postgres.JobRepository, userID, workerID string) *domain.Job {
	b.Helper()
	ctx := context.Background()
	job := testutil.NewJob(
		testutil.WithUserID(userID),
		testutil.WithStatus(domain.StatusPending),
		testutil.WithScheduledAt(time.Now().Add(-time.Minute)),
	)
	if _, err := repo.Create(ctx, job); err != nil {
		b.Fatalf("create job: %v", err)
	}
	claimed, err := repo.Claim(ctx, workerID, 1)
	if err != nil || len(claimed) == 0 {
		b.Fatalf("claim job: err=%v n=%d", err, len(claimed))
	}
	return claimed[0]
}

// BenchmarkUpdateHeartbeat measures the per-beat cost. This is the path the
// heartbeat-sidecar change targets — it currently rewrites the full job row.
func BenchmarkUpdateHeartbeat(b *testing.B) {
	pool, userID := benchPoolUser(b)
	repo := postgres.NewJobRepository(pool)
	job := createRunningJob(b, repo, userID, "bench-worker")
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := repo.UpdateHeartbeat(ctx, job.ID); err != nil {
			b.Fatalf("heartbeat: %v", err)
		}
	}
}

// BenchmarkAttemptCreateComplete measures the per-execution attempt write pair
// (insert then completion UPDATE) — exercises the job_attempts HOT-update path.
func BenchmarkAttemptCreateComplete(b *testing.B) {
	pool, userID := benchPoolUser(b)
	jobRepo := postgres.NewJobRepository(pool)
	attempts := postgres.NewAttemptRepository(pool)
	job := createRunningJob(b, jobRepo, userID, "bench-worker")
	ctx := context.Background()
	statusCode := 200

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a, err := attempts.CreateAttempt(ctx, &domain.JobAttempt{
			JobID: job.ID, AttemptNum: i + 1, WorkerID: "bench-worker", StartedAt: time.Now(),
		})
		if err != nil {
			b.Fatalf("create attempt: %v", err)
		}
		if err := attempts.CompleteAttempt(ctx, a.ID, a.StartedAt, &statusCode, nil, 12); err != nil {
			b.Fatalf("complete attempt: %v", err)
		}
	}
}

// BenchmarkClaimSingle measures one claim cycle (the worker's poll-loop query)
// against a backlog of due jobs.
func BenchmarkClaimSingle(b *testing.B) {
	pool, userID := benchPoolUser(b)
	repo := postgres.NewJobRepository(pool)
	ctx := context.Background()

	for i := 0; i < b.N; i++ {
		j := testutil.NewJob(
			testutil.WithUserID(userID),
			testutil.WithStatus(domain.StatusPending),
			testutil.WithScheduledAt(time.Now().Add(-time.Minute)),
		)
		if _, err := repo.Create(ctx, j); err != nil {
			b.Fatalf("seed pending: %v", err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := repo.Claim(ctx, "bench-worker", 1); err != nil {
			b.Fatalf("claim: %v", err)
		}
	}
}
