//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/infrastructure/postgres"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/testutil"
	"github.com/jackc/pgx/v5/pgxpool"
)

// setupJobRepoPool is like setupJobRepo but also returns the pool so tests can
// inspect the job_heartbeats sidecar directly.
func setupJobRepoPool(t *testing.T) (*postgres.JobRepository, *pgxpool.Pool, string) {
	t.Helper()
	pool := testutil.SetupTestDB(t)
	testutil.TruncateAll(t, pool)
	userID := testutil.UserID()
	testutil.SeedUser(t, pool, userID)
	return postgres.NewJobRepository(pool), pool, userID
}

func heartbeatRow(t *testing.T, pool *pgxpool.Pool, jobID string) (time.Time, bool) {
	t.Helper()
	var hb time.Time
	err := pool.QueryRow(context.Background(),
		`SELECT heartbeat_at FROM job_heartbeats WHERE job_id = $1`, jobID).Scan(&hb)
	if err != nil {
		return time.Time{}, false
	}
	return hb, true
}

func claimOne(t *testing.T, repo *postgres.JobRepository, userID string) *domain.Job {
	t.Helper()
	ctx := context.Background()
	job := testutil.NewJob(testutil.WithUserID(userID), testutil.WithScheduledAt(time.Now().Add(-time.Minute)))
	if _, err := repo.Create(ctx, job); err != nil {
		t.Fatalf("create: %v", err)
	}
	claimed, err := repo.Claim(ctx, "worker-1", 10)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim: err=%v n=%d", err, len(claimed))
	}
	return claimed[0]
}

func TestHeartbeat_ClaimSeedsSidecar(t *testing.T) {
	repo, pool, userID := setupJobRepoPool(t)
	job := claimOne(t, repo, userID)
	if _, ok := heartbeatRow(t, pool, job.ID); !ok {
		t.Fatal("expected a job_heartbeats row after claim")
	}
}

// UpdateHeartbeat must advance the sidecar and NOT touch the heavy jobs row.
func TestHeartbeat_UpdateWritesSidecarOnly(t *testing.T) {
	repo, pool, userID := setupJobRepoPool(t)
	ctx := context.Background()
	job := claimOne(t, repo, userID)

	before, ok := heartbeatRow(t, pool, job.ID)
	if !ok {
		t.Fatal("no sidecar row after claim")
	}
	var jobsHBBefore time.Time
	if err := pool.QueryRow(ctx, `SELECT heartbeat_at FROM jobs WHERE id=$1`, job.ID).Scan(&jobsHBBefore); err != nil {
		t.Fatalf("read jobs.heartbeat_at: %v", err)
	}

	time.Sleep(10 * time.Millisecond)
	if err := repo.UpdateHeartbeat(ctx, job.ID); err != nil {
		t.Fatalf("update heartbeat: %v", err)
	}

	after, _ := heartbeatRow(t, pool, job.ID)
	if !after.After(before) {
		t.Fatalf("sidecar heartbeat did not advance: before=%v after=%v", before, after)
	}
	var jobsHBAfter time.Time
	if err := pool.QueryRow(ctx, `SELECT heartbeat_at FROM jobs WHERE id=$1`, job.ID).Scan(&jobsHBAfter); err != nil {
		t.Fatalf("read jobs.heartbeat_at: %v", err)
	}
	if !jobsHBAfter.Equal(jobsHBBefore) {
		t.Fatalf("jobs.heartbeat_at changed (%v -> %v) — heartbeat should not touch the jobs row", jobsHBBefore, jobsHBAfter)
	}
}

func TestHeartbeat_UpdateIgnoredWhenNotRunning(t *testing.T) {
	repo, pool, userID := setupJobRepoPool(t)
	ctx := context.Background()
	job := claimOne(t, repo, userID)
	if err := repo.Complete(ctx, job.ID); err != nil {
		t.Fatalf("complete: %v", err)
	}
	// Completing dropped the row; a stray heartbeat must not resurrect it.
	if err := repo.UpdateHeartbeat(ctx, job.ID); err != nil {
		t.Fatalf("update heartbeat: %v", err)
	}
	if _, ok := heartbeatRow(t, pool, job.ID); ok {
		t.Fatal("heartbeat row should not exist for a completed job")
	}
}

func TestHeartbeat_TerminalTransitionsDropSidecar(t *testing.T) {
	for _, tc := range []struct {
		name string
		act  func(repo *postgres.JobRepository, id string) error
	}{
		{"complete", func(r *postgres.JobRepository, id string) error { return r.Complete(context.Background(), id) }},
		{"fail", func(r *postgres.JobRepository, id string) error { return r.Fail(context.Background(), id, "boom") }},
		{"reschedule", func(r *postgres.JobRepository, id string) error {
			return r.Reschedule(context.Background(), id, "boom", time.Now().Add(time.Minute))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, pool, userID := setupJobRepoPool(t)
			job := claimOne(t, repo, userID)
			if _, ok := heartbeatRow(t, pool, job.ID); !ok {
				t.Fatal("expected sidecar row after claim")
			}
			if err := tc.act(repo, job.ID); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if _, ok := heartbeatRow(t, pool, job.ID); ok {
				t.Fatalf("%s should have dropped the sidecar row", tc.name)
			}
		})
	}
}

func TestHeartbeat_RescheduleStaleUsesSidecarAndCleansUp(t *testing.T) {
	repo, pool, userID := setupJobRepoPool(t)
	ctx := context.Background()
	job := claimOne(t, repo, userID)

	n, err := repo.RescheduleStale(ctx, time.Now().Add(time.Hour), 100) // everything is stale
	if err != nil {
		t.Fatalf("reschedule stale: %v", err)
	}
	if n != 1 {
		t.Fatalf("rescheduled %d, want 1", n)
	}
	if _, ok := heartbeatRow(t, pool, job.ID); ok {
		t.Fatal("reschedule-stale should drop the sidecar row")
	}
	got, _ := repo.GetByID(ctx, job.ID, userID)
	if got.Status != domain.StatusPending {
		t.Fatalf("status = %s, want pending", got.Status)
	}
}

// A running job whose sidecar row went missing must be treated as stale even if
// the cutoff is in the past (defensive: a lost heartbeat == a dead worker).
func TestHeartbeat_MissingSidecarRowIsStale(t *testing.T) {
	repo, pool, userID := setupJobRepoPool(t)
	ctx := context.Background()
	job := claimOne(t, repo, userID)

	if _, err := pool.Exec(ctx, `DELETE FROM job_heartbeats WHERE job_id=$1`, job.ID); err != nil {
		t.Fatalf("delete sidecar: %v", err)
	}

	// Cutoff one hour in the PAST: a live heartbeat would NOT be stale, so this
	// only reschedules because the row is missing.
	n, err := repo.RescheduleStale(ctx, time.Now().Add(-time.Hour), 100)
	if err != nil {
		t.Fatalf("reschedule stale: %v", err)
	}
	if n != 1 {
		t.Fatalf("rescheduled %d, want 1 (missing heartbeat must be treated as stale)", n)
	}
}
