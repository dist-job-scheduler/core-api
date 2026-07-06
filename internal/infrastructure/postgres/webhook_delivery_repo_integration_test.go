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

func newDeliveryRepo(t *testing.T) (*postgres.WebhookDeliveryRepository, string, context.Context) {
	t.Helper()
	pool := testutil.SetupTestDB(t)
	testutil.TruncateAll(t, pool)
	userID := testutil.UserID()
	testutil.SeedUser(t, pool, userID)
	return postgres.NewWebhookDeliveryRepository(pool), userID, context.Background()
}

func enqueueTestDelivery(t *testing.T, repo *postgres.WebhookDeliveryRepository, ctx context.Context, userID string) *domain.WebhookDelivery {
	t.Helper()
	jobID := "job-abc"
	d, err := repo.Enqueue(ctx, &domain.WebhookDelivery{
		UserID:      userID,
		JobID:       &jobID,
		Event:       domain.WebhookEventJobCompleted,
		URL:         "https://example.com/hook",
		Headers:     map[string]string{"X-Test": "1"},
		Payload:     []byte(`{"event":"job.completed","job_id":"job-abc"}`),
		MaxAttempts: 3,
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	return d
}

// Enqueue → ClaimDue → MarkDelivered is the happy-path lifecycle, and the payload
// bytes survive the round-trip verbatim.
func TestWebhookDeliveryRepo_Lifecycle_Delivered(t *testing.T) {
	repo, userID, ctx := newDeliveryRepo(t)
	d := enqueueTestDelivery(t, repo, ctx, userID)

	if d.Status != domain.WebhookDeliveryPending || d.Attempts != 0 || d.MaxAttempts != 3 {
		t.Fatalf("enqueued row = status:%s attempts:%d max:%d, want pending/0/3", d.Status, d.Attempts, d.MaxAttempts)
	}
	if string(d.Payload) != `{"event":"job.completed","job_id":"job-abc"}` {
		t.Fatalf("payload round-trip mismatch: %q", d.Payload)
	}

	// A freshly-enqueued row is due immediately.
	claimed, err := repo.ClaimDue(ctx, 10, 60*time.Second)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(claimed) != 1 || claimed[0].ID != d.ID {
		t.Fatalf("claimed %d rows, want the enqueued one", len(claimed))
	}
	if claimed[0].Status != domain.WebhookDeliveryDelivering {
		t.Errorf("claimed status = %s, want delivering", claimed[0].Status)
	}
	if string(claimed[0].Payload) != string(d.Payload) {
		t.Errorf("claimed payload mismatch: %q", claimed[0].Payload)
	}
	if claimed[0].Headers["X-Test"] != "1" {
		t.Errorf("headers not round-tripped: %+v", claimed[0].Headers)
	}

	// A second immediate claim returns nothing — the row is in-flight, not stale.
	again, err := repo.ClaimDue(ctx, 10, 60*time.Second)
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("second claim returned %d rows, want 0 (already in-flight)", len(again))
	}

	if err := repo.MarkDelivered(ctx, d.ID, 200); err != nil {
		t.Fatalf("mark delivered: %v", err)
	}

	rows, err := repo.ListByUser(ctx, userID, 10, nil, "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("list returned %d, want 1", len(rows))
	}
	got := rows[0]
	if got.Status != domain.WebhookDeliveryDelivered {
		t.Errorf("status = %s, want delivered", got.Status)
	}
	if got.Attempts != 1 {
		t.Errorf("attempts = %d, want 1", got.Attempts)
	}
	if got.StatusCode == nil || *got.StatusCode != 200 {
		t.Errorf("status_code = %v, want 200", got.StatusCode)
	}
	if got.DeliveredAt == nil {
		t.Error("delivered_at not set")
	}
}

// Reschedule bumps attempts and defers the row past its next_retry_at, so it is
// not immediately re-claimable; MarkFailed after that is terminal.
func TestWebhookDeliveryRepo_RescheduleThenFail(t *testing.T) {
	repo, userID, ctx := newDeliveryRepo(t)
	d := enqueueTestDelivery(t, repo, ctx, userID)

	if _, err := repo.ClaimDue(ctx, 10, 60*time.Second); err != nil {
		t.Fatalf("claim: %v", err)
	}
	code := 500
	if err := repo.Reschedule(ctx, d.ID, &code, "non-2xx response: 500", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("reschedule: %v", err)
	}

	// Deferred an hour out → not due.
	due, err := repo.ClaimDue(ctx, 10, 60*time.Second)
	if err != nil {
		t.Fatalf("claim after reschedule: %v", err)
	}
	if len(due) != 0 {
		t.Fatalf("claimed %d rows, want 0 (retry is in the future)", len(due))
	}

	rows, _ := repo.ListByUser(ctx, userID, 10, nil, "")
	if rows[0].Status != domain.WebhookDeliveryPending || rows[0].Attempts != 1 {
		t.Fatalf("after reschedule: status:%s attempts:%d, want pending/1", rows[0].Status, rows[0].Attempts)
	}

	if err := repo.MarkFailed(ctx, d.ID, &code, "gave up"); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	rows, _ = repo.ListByUser(ctx, userID, 10, nil, "")
	if rows[0].Status != domain.WebhookDeliveryFailed || rows[0].Attempts != 2 {
		t.Fatalf("after fail: status:%s attempts:%d, want failed/2", rows[0].Status, rows[0].Attempts)
	}
}

// A row stuck in 'delivering' past the in-flight timeout (dispatcher crashed) is
// reclaimed — the guarantee that keeps delivery at-least-once.
func TestWebhookDeliveryRepo_ReclaimsStaleInflight(t *testing.T) {
	repo, userID, ctx := newDeliveryRepo(t)
	d := enqueueTestDelivery(t, repo, ctx, userID)

	// Claim moves it to 'delivering'.
	if _, err := repo.ClaimDue(ctx, 10, 60*time.Second); err != nil {
		t.Fatalf("claim: %v", err)
	}
	// A negative timeout makes every in-flight row already stale → reclaimed.
	reclaimed, err := repo.ClaimDue(ctx, 10, -time.Second)
	if err != nil {
		t.Fatalf("reclaim: %v", err)
	}
	if len(reclaimed) != 1 || reclaimed[0].ID != d.ID {
		t.Fatalf("reclaimed %d rows, want the stale one", len(reclaimed))
	}
}

// Keyset pagination returns newest-first and never drops or repeats a row.
func TestWebhookDeliveryRepo_KeysetPagination(t *testing.T) {
	repo, userID, ctx := newDeliveryRepo(t)
	const total = 5
	for range [total]int{} {
		enqueueTestDelivery(t, repo, ctx, userID)
	}

	page1, err := repo.ListByUser(ctx, userID, 2, nil, "")
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(page1) != 2 {
		t.Fatalf("page1 = %d, want 2", len(page1))
	}
	last := page1[len(page1)-1]
	page2, err := repo.ListByUser(ctx, userID, 2, &last.CreatedAt, last.ID)
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2) != 2 {
		t.Fatalf("page2 = %d, want 2", len(page2))
	}

	seen := map[string]bool{}
	for _, d := range append(page1, page2...) {
		if seen[d.ID] {
			t.Fatalf("duplicate row across pages: %s", d.ID)
		}
		seen[d.ID] = true
	}
	if len(seen) != 4 {
		t.Fatalf("distinct rows across 2 pages = %d, want 4", len(seen))
	}
}
