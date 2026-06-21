//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/infrastructure/postgres"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/testutil"
)

func TestPartition_EnsureCreatesFutureMonths(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	repo := postgres.NewAttemptPartitionRepository(pool)

	// Far future so we never collide with the migration's pre-created set.
	now := time.Date(2099, 1, 15, 0, 0, 0, 0, time.UTC)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DROP TABLE IF EXISTS job_attempts_2099_01, job_attempts_2099_02`)
	})

	created, err := repo.EnsureMonthlyPartitions(ctx, now, 1) // current + 1 ahead = 2
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if len(created) != 2 {
		t.Fatalf("created %v, want 2 (2099_01, 2099_02)", created)
	}
	for _, name := range []string{"job_attempts_2099_01", "job_attempts_2099_02"} {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT to_regclass('public.'||$1) IS NOT NULL`, name).Scan(&exists); err != nil || !exists {
			t.Fatalf("partition %s should exist (exists=%v err=%v)", name, exists, err)
		}
	}

	// Idempotent: a second call creates nothing.
	again, err := repo.EnsureMonthlyPartitions(ctx, now, 1)
	if err != nil || len(again) != 0 {
		t.Fatalf("second ensure should be a no-op, got %v err=%v", again, err)
	}
}

func TestPartition_DropPartitionsBefore(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	repo := postgres.NewAttemptPartitionRepository(pool)

	// A partition older than every migration-created one (which start 2026-01).
	if _, err := pool.Exec(ctx,
		`CREATE TABLE IF NOT EXISTS job_attempts_2025_12 PARTITION OF job_attempts
		 FOR VALUES FROM ('2025-12-01') TO ('2026-01-01')`); err != nil {
		t.Fatalf("create 2025_12: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DROP TABLE IF EXISTS job_attempts_2025_12`) })

	// Cutoff = 2026-01: only months strictly before 2026-01 are dropped, i.e.
	// just our 2025_12 — the real 2026_* partitions are kept.
	dropped, err := repo.DropPartitionsBefore(ctx, time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("drop: %v", err)
	}
	found := false
	for _, n := range dropped {
		if n == "job_attempts_2025_12" {
			found = true
		}
		if n == "job_attempts_2026_06" {
			t.Fatalf("dropped a current-era partition %s — cutoff should have kept it", n)
		}
	}
	if !found {
		t.Fatalf("expected job_attempts_2025_12 to be dropped, got %v", dropped)
	}
	var stillThere bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.job_attempts_2026_06') IS NOT NULL`).Scan(&stillThere); err != nil || !stillThere {
		t.Fatalf("job_attempts_2026_06 should still exist (exists=%v err=%v)", stillThere, err)
	}
}
