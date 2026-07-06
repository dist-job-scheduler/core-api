package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/testutil"
)

// fakeDeliverer returns a scripted (statusCode, err) for every Deliver call and
// records how many times and with what body it was invoked.
type fakeDeliverer struct {
	mu       sync.Mutex
	code     int
	err      error
	calls    int32
	lastBody []byte
}

func (f *fakeDeliverer) Deliver(_ context.Context, _ string, _ string, _ map[string]string, body []byte) (int, error) {
	atomic.AddInt32(&f.calls, 1)
	f.mu.Lock()
	f.lastBody = body
	f.mu.Unlock()
	return f.code, f.err
}

func TestWebhookRetryDelay(t *testing.T) {
	base := 30 * time.Second
	tests := []struct {
		attemptsMade int
		want         time.Duration
	}{
		{0, 30 * time.Second},   // clamped up to 1
		{1, 30 * time.Second},   // base
		{2, 60 * time.Second},   // base*2
		{3, 120 * time.Second},  // base*4
		{4, 240 * time.Second},  // base*8
		{7, 1920 * time.Second}, // base*64 = 32m
		{20, time.Hour},         // capped
		{100, time.Hour},        // capped, no overflow
	}
	for _, tt := range tests {
		if got := webhookRetryDelay(base, tt.attemptsMade); got != tt.want {
			t.Errorf("webhookRetryDelay(30s, %d) = %v, want %v", tt.attemptsMade, got, tt.want)
		}
	}
}

// A 2xx marks the delivery delivered and never retries or fails it.
func TestWebhookDispatcher_DeliverOne_Success(t *testing.T) {
	var gotID string
	var gotCode int
	repo := &testutil.MockWebhookDeliveryRepository{
		MarkDeliveredFn: func(_ context.Context, id string, code int) error {
			gotID, gotCode = id, code
			return nil
		},
		RescheduleFn: func(_ context.Context, _ string, _ *int, _ string, _ time.Time) error {
			t.Fatal("Reschedule called on a 2xx delivery")
			return nil
		},
		MarkFailedFn: func(_ context.Context, _ string, _ *int, _ string) error {
			t.Fatal("MarkFailed called on a 2xx delivery")
			return nil
		},
	}
	d := NewWebhookDispatcher(repo, &fakeDeliverer{code: 202}, slog.Default(), time.Second)
	d.deliverOne(context.Background(), &domain.WebhookDelivery{ID: "d1", Attempts: 0, MaxAttempts: 5})

	if gotID != "d1" || gotCode != 202 {
		t.Fatalf("MarkDelivered(id=%q, code=%d), want (d1, 202)", gotID, gotCode)
	}
}

// A non-2xx with attempts left reschedules a retry, carrying the status code and a
// strictly-future next_retry_at — and does NOT mark it failed.
func TestWebhookDispatcher_DeliverOne_RetriesOnNon2xx(t *testing.T) {
	var gotCode *int
	var gotNext time.Time
	repo := &testutil.MockWebhookDeliveryRepository{
		RescheduleFn: func(_ context.Context, id string, code *int, lastErr string, next time.Time) error {
			gotCode, gotNext = code, next
			if id != "d1" {
				t.Errorf("Reschedule id = %q, want d1", id)
			}
			if lastErr == "" {
				t.Error("Reschedule lastErr is empty")
			}
			return nil
		},
		MarkFailedFn: func(_ context.Context, _ string, _ *int, _ string) error {
			t.Fatal("MarkFailed called while attempts remain")
			return nil
		},
	}
	d := NewWebhookDispatcher(repo, &fakeDeliverer{code: 500}, slog.Default(), time.Second)
	before := time.Now()
	d.deliverOne(context.Background(), &domain.WebhookDelivery{ID: "d1", Attempts: 0, MaxAttempts: 5})

	if gotCode == nil || *gotCode != 500 {
		t.Fatalf("Reschedule status code = %v, want 500", gotCode)
	}
	if !gotNext.After(before) {
		t.Fatalf("next_retry_at = %v, want strictly after %v", gotNext, before)
	}
}

// A transport error (no HTTP response) reschedules with a nil status code.
func TestWebhookDispatcher_DeliverOne_TransportErrorRetriesWithNilCode(t *testing.T) {
	sawReschedule := false
	repo := &testutil.MockWebhookDeliveryRepository{
		RescheduleFn: func(_ context.Context, _ string, code *int, lastErr string, _ time.Time) error {
			sawReschedule = true
			if code != nil {
				t.Errorf("status code = %v, want nil on transport error", *code)
			}
			if lastErr != "connection refused" {
				t.Errorf("lastErr = %q, want %q", lastErr, "connection refused")
			}
			return nil
		},
	}
	d := NewWebhookDispatcher(repo, &fakeDeliverer{code: 0, err: errors.New("connection refused")}, slog.Default(), time.Second)
	d.deliverOne(context.Background(), &domain.WebhookDelivery{ID: "d1", Attempts: 1, MaxAttempts: 5})

	if !sawReschedule {
		t.Fatal("transport error did not reschedule a retry")
	}
}

// The final failed attempt (attempts+1 == max) is terminal: MarkFailed, no retry.
func TestWebhookDispatcher_DeliverOne_FailsWhenExhausted(t *testing.T) {
	var failedID string
	repo := &testutil.MockWebhookDeliveryRepository{
		MarkFailedFn: func(_ context.Context, id string, code *int, _ string) error {
			failedID = id
			if code == nil || *code != 503 {
				t.Errorf("MarkFailed code = %v, want 503", code)
			}
			return nil
		},
		RescheduleFn: func(_ context.Context, _ string, _ *int, _ string, _ time.Time) error {
			t.Fatal("Reschedule called on the exhausting attempt")
			return nil
		},
	}
	d := NewWebhookDispatcher(repo, &fakeDeliverer{code: 503}, slog.Default(), time.Second)
	// Attempts=4, Max=5 → this attempt is the 5th → exhausted.
	d.deliverOne(context.Background(), &domain.WebhookDelivery{ID: "d1", Attempts: 4, MaxAttempts: 5})

	if failedID != "d1" {
		t.Fatalf("MarkFailed id = %q, want d1", failedID)
	}
}

// dispatchBatch claims due deliveries and delivers each exactly once.
func TestWebhookDispatcher_DispatchBatch_DeliversAllClaimed(t *testing.T) {
	var delivered int32
	repo := &testutil.MockWebhookDeliveryRepository{
		ClaimDueFn: func(_ context.Context, _ int, _ time.Duration) ([]*domain.WebhookDelivery, error) {
			return []*domain.WebhookDelivery{
				{ID: "a", MaxAttempts: 5},
				{ID: "b", MaxAttempts: 5},
				{ID: "c", MaxAttempts: 5},
			}, nil
		},
		MarkDeliveredFn: func(_ context.Context, _ string, _ int) error {
			atomic.AddInt32(&delivered, 1)
			return nil
		},
	}
	deliverer := &fakeDeliverer{code: 200}
	d := NewWebhookDispatcher(repo, deliverer, slog.Default(), time.Second)
	d.dispatchBatch(context.Background())

	if got := atomic.LoadInt32(&delivered); got != 3 {
		t.Fatalf("delivered = %d, want 3", got)
	}
	if got := atomic.LoadInt32(&deliverer.calls); got != 3 {
		t.Fatalf("Deliver calls = %d, want 3", got)
	}
}
