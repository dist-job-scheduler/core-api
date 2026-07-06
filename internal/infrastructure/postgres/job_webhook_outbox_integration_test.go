//go:build integration

package postgres_test

import (
	"context"
	"testing"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/infrastructure/postgres"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/testutil"
	"github.com/jackc/pgx/v5/pgxpool"
)

func outboxSetup(t *testing.T) (*pgxpool.Pool, *postgres.JobRepository, *postgres.WebhookDeliveryRepository, string, context.Context) {
	t.Helper()
	pool := testutil.SetupTestDB(t)
	testutil.TruncateAll(t, pool)
	userID := testutil.UserID()
	testutil.SeedUser(t, pool, userID)
	return pool, postgres.NewJobRepository(pool), postgres.NewWebhookDeliveryRepository(pool), userID, context.Background()
}

func countDeliveries(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM webhook_deliveries").Scan(&n); err != nil {
		t.Fatalf("count deliveries: %v", err)
	}
	return n
}

// The job terminal transition and its webhook delivery commit together.
func TestJobRepo_CompleteWithWebhook_CommitsBoth(t *testing.T) {
	_, jobRepo, whRepo, userID, ctx := outboxSetup(t)

	job, err := jobRepo.Create(ctx, testutil.NewJob(testutil.WithUserID(userID)))
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	del := &domain.WebhookDelivery{
		UserID:      userID,
		JobID:       &job.ID,
		Event:       domain.WebhookEventJobCompleted,
		URL:         "https://example.com/hook",
		Payload:     []byte(`{"event":"job.completed"}`),
		MaxAttempts: 5,
	}
	if err := jobRepo.CompleteWithWebhook(ctx, job.ID, del); err != nil {
		t.Fatalf("CompleteWithWebhook: %v", err)
	}

	got, err := jobRepo.GetByID(ctx, job.ID, userID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if got.Status != domain.StatusCompleted {
		t.Errorf("job status = %s, want completed", got.Status)
	}
	rows, _ := whRepo.ListByUser(ctx, userID, 10, nil, "")
	if len(rows) != 1 {
		t.Fatalf("deliveries = %d, want 1", len(rows))
	}
	if rows[0].JobID == nil || *rows[0].JobID != job.ID {
		t.Errorf("delivery not linked to job %s: %+v", job.ID, rows[0])
	}
}

// A nil delivery (job had no webhook_url) behaves like a plain Complete.
func TestJobRepo_CompleteWithWebhook_NilDelivery(t *testing.T) {
	pool, jobRepo, _, userID, ctx := outboxSetup(t)

	job, err := jobRepo.Create(ctx, testutil.NewJob(testutil.WithUserID(userID)))
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if err := jobRepo.CompleteWithWebhook(ctx, job.ID, nil); err != nil {
		t.Fatalf("CompleteWithWebhook(nil): %v", err)
	}

	got, _ := jobRepo.GetByID(ctx, job.ID, userID)
	if got.Status != domain.StatusCompleted {
		t.Errorf("job status = %s, want completed", got.Status)
	}
	if n := countDeliveries(t, pool); n != 0 {
		t.Errorf("deliveries = %d, want 0 for a nil delivery", n)
	}
}

// Atomicity: if the delivery insert fails, the job's terminal transition rolls
// back too — a bogus user_id violates the FK, so neither write persists.
func TestJobRepo_FailWithWebhook_RollsBackOnDeliveryError(t *testing.T) {
	pool, jobRepo, _, userID, ctx := outboxSetup(t)

	job, err := jobRepo.Create(ctx, testutil.NewJob(testutil.WithUserID(userID), testutil.WithStatus(domain.StatusPending)))
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	bogus := &domain.WebhookDelivery{
		UserID:      "no-such-user", // violates webhook_deliveries.user_id FK
		JobID:       &job.ID,
		Event:       domain.WebhookEventJobFailed,
		URL:         "https://example.com/hook",
		Payload:     []byte(`{}`),
		MaxAttempts: 5,
	}
	if err := jobRepo.FailWithWebhook(ctx, job.ID, "boom", bogus); err == nil {
		t.Fatal("expected an error from the FK-violating delivery insert")
	}

	got, _ := jobRepo.GetByID(ctx, job.ID, userID)
	if got.Status != domain.StatusPending {
		t.Errorf("job status = %s, want pending — the terminal transition should have rolled back", got.Status)
	}
	if n := countDeliveries(t, pool); n != 0 {
		t.Errorf("deliveries = %d, want 0 — the insert should have rolled back", n)
	}
}
