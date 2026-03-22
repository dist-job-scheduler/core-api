package scheduler_test

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/scheduler"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/testutil"
)

func TestReaper_ReapsOnInterval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var rescheduleCount, failCount atomic.Int32

		repo := &testutil.MockJobRepository{
			ReschedulesStaleFn: func(ctx context.Context, staleCutoff time.Time, limit int) (int, error) {
				rescheduleCount.Add(1)
				return 0, nil
			},
			FailStaleFn: func(ctx context.Context, staleCutoff time.Time, limit int) (int, error) {
				failCount.Add(1)
				return 0, nil
			},
		}

		reaper := scheduler.NewReaper(repo, slog.Default(), 30*time.Second, 30*time.Second)

		ctx, cancel := context.WithCancel(t.Context())
		go reaper.Start(ctx)

		// First tick at 30s.
		time.Sleep(30 * time.Second)
		synctest.Wait()

		if got := rescheduleCount.Load(); got != 1 {
			t.Fatalf("after 30s: reschedule count = %d, want 1", got)
		}
		if got := failCount.Load(); got != 1 {
			t.Fatalf("after 30s: fail count = %d, want 1", got)
		}

		// Second tick at 60s.
		time.Sleep(30 * time.Second)
		synctest.Wait()

		if got := rescheduleCount.Load(); got != 2 {
			t.Fatalf("after 60s: reschedule count = %d, want 2", got)
		}
		if got := failCount.Load(); got != 2 {
			t.Fatalf("after 60s: fail count = %d, want 2", got)
		}

		cancel()
		synctest.Wait()
	})
}

func TestReaper_GracefulShutdown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var reapCount atomic.Int32

		repo := &testutil.MockJobRepository{
			ReschedulesStaleFn: func(ctx context.Context, staleCutoff time.Time, limit int) (int, error) {
				reapCount.Add(1)
				return 0, nil
			},
			FailStaleFn: func(ctx context.Context, staleCutoff time.Time, limit int) (int, error) {
				return 0, nil
			},
		}

		reaper := scheduler.NewReaper(repo, slog.Default(), 30*time.Second, 30*time.Second)

		ctx, cancel := context.WithCancel(t.Context())
		go reaper.Start(ctx)

		// Let one tick happen.
		time.Sleep(30 * time.Second)
		synctest.Wait()

		// Cancel and verify no more reaps happen.
		cancel()
		synctest.Wait()

		countAtShutdown := reapCount.Load()

		// Advance well past another interval — should not tick again.
		time.Sleep(5 * time.Minute)
		synctest.Wait()

		if got := reapCount.Load(); got != countAtShutdown {
			t.Fatalf("after shutdown: reap count = %d, want %d (no new reaps)", got, countAtShutdown)
		}
	})
}
