package scheduler_test

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/scheduler"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/testutil"
)

func TestDispatcher_DispatchesOnInterval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var dispatchCount atomic.Int32

		schedRepo := &testutil.MockScheduleRepository{
			ClaimAndFireFn: func(ctx context.Context, limit int, computeNext func(*domain.Schedule) time.Time, creditFilter func(ctx context.Context, userID string) bool) ([]*domain.Job, error) {
				dispatchCount.Add(1)
				return nil, nil
			},
		}
		creditRepo := &testutil.MockCreditRepository{}

		dispatcher := scheduler.NewDispatcher(schedRepo, creditRepo, slog.Default(), 10*time.Second)

		ctx, cancel := context.WithCancel(t.Context())
		go dispatcher.Start(ctx)

		// First tick at 10s.
		time.Sleep(10 * time.Second)
		synctest.Wait()

		if got := dispatchCount.Load(); got != 1 {
			t.Fatalf("after 10s: dispatch count = %d, want 1", got)
		}

		// Second tick at 20s.
		time.Sleep(10 * time.Second)
		synctest.Wait()

		if got := dispatchCount.Load(); got != 2 {
			t.Fatalf("after 20s: dispatch count = %d, want 2", got)
		}

		cancel()
		synctest.Wait()
	})
}

func TestDispatcher_GracefulShutdown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var dispatchCount atomic.Int32

		schedRepo := &testutil.MockScheduleRepository{
			ClaimAndFireFn: func(ctx context.Context, limit int, computeNext func(*domain.Schedule) time.Time, creditFilter func(ctx context.Context, userID string) bool) ([]*domain.Job, error) {
				dispatchCount.Add(1)
				return nil, nil
			},
		}
		creditRepo := &testutil.MockCreditRepository{}

		dispatcher := scheduler.NewDispatcher(schedRepo, creditRepo, slog.Default(), 10*time.Second)

		ctx, cancel := context.WithCancel(t.Context())
		go dispatcher.Start(ctx)

		// Let one tick happen.
		time.Sleep(10 * time.Second)
		synctest.Wait()

		cancel()
		synctest.Wait()

		countAtShutdown := dispatchCount.Load()

		// Advance past several intervals — should not tick again.
		time.Sleep(5 * time.Minute)
		synctest.Wait()

		if got := dispatchCount.Load(); got != countAtShutdown {
			t.Fatalf("after shutdown: dispatch count = %d, want %d (no new dispatches)", got, countAtShutdown)
		}
	})
}
