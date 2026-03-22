//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/infrastructure/postgres"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/repository"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/testutil"
)

func setupJobRepo(t *testing.T) (*postgres.JobRepository, string) {
	t.Helper()
	pool := testutil.SetupTestDB(t)
	testutil.TruncateAll(t, pool)
	userID := testutil.UserID()
	testutil.SeedUser(t, pool, userID)
	return postgres.NewJobRepository(pool), userID
}

func TestJobRepo_CreateAndGetByID(t *testing.T) {
	repo, userID := setupJobRepo(t)
	ctx := context.Background()

	job := testutil.NewJob(testutil.WithUserID(userID), testutil.WithScheduledAt(time.Now().Add(time.Hour)))
	created, err := repo.Create(ctx, job)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if created.Status != domain.StatusPending {
		t.Fatalf("status = %s, want pending", created.Status)
	}

	got, err := repo.GetByID(ctx, created.ID, userID)
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if got.URL != created.URL {
		t.Fatalf("url = %s, want %s", got.URL, created.URL)
	}
	if got.Method != created.Method {
		t.Fatalf("method = %s, want %s", got.Method, created.Method)
	}
}

func TestJobRepo_CreateDuplicate(t *testing.T) {
	repo, userID := setupJobRepo(t)
	ctx := context.Background()

	job := testutil.NewJob(testutil.WithUserID(userID))
	if _, err := repo.Create(ctx, job); err != nil {
		t.Fatalf("first create: %v", err)
	}

	// Same idempotency key + user → duplicate
	dup := testutil.NewJob(testutil.WithUserID(userID))
	dup.IdempotencyKey = job.IdempotencyKey
	_, err := repo.Create(ctx, dup)
	if err == nil {
		t.Fatal("expected error on duplicate idempotency key")
	}
	if err != domain.ErrDuplicateJob {
		t.Fatalf("err = %v, want ErrDuplicateJob", err)
	}
}

func TestJobRepo_GetByID_WrongUser(t *testing.T) {
	repo, userID := setupJobRepo(t)
	ctx := context.Background()

	job := testutil.NewJob(testutil.WithUserID(userID))
	created, err := repo.Create(ctx, job)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Different user → not found (authorization at query level)
	_, err = repo.GetByID(ctx, created.ID, "other-user")
	if err != domain.ErrJobNotFound {
		t.Fatalf("err = %v, want ErrJobNotFound", err)
	}
}

func TestJobRepo_ClaimAndComplete(t *testing.T) {
	repo, userID := setupJobRepo(t)
	ctx := context.Background()

	// Create a job scheduled in the past so it's claimable
	job := testutil.NewJob(testutil.WithUserID(userID), testutil.WithScheduledAt(time.Now().Add(-time.Minute)))
	created, err := repo.Create(ctx, job)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Claim
	claimed, err := repo.Claim(ctx, "worker-1", 10)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("claimed %d jobs, want 1", len(claimed))
	}
	if claimed[0].ID != created.ID {
		t.Fatalf("claimed wrong job")
	}
	if claimed[0].Status != domain.StatusRunning {
		t.Fatalf("status = %s, want running", claimed[0].Status)
	}

	// Second claim returns nothing (job already claimed)
	claimed2, err := repo.Claim(ctx, "worker-2", 10)
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if len(claimed2) != 0 {
		t.Fatalf("second claim got %d jobs, want 0", len(claimed2))
	}

	// Complete
	if err := repo.Complete(ctx, created.ID); err != nil {
		t.Fatalf("complete: %v", err)
	}
	got, err := repo.GetByID(ctx, created.ID, userID)
	if err != nil {
		t.Fatalf("get after complete: %v", err)
	}
	if got.Status != domain.StatusCompleted {
		t.Fatalf("status = %s, want completed", got.Status)
	}
}

func TestJobRepo_FailAndReschedule(t *testing.T) {
	repo, userID := setupJobRepo(t)
	ctx := context.Background()

	job := testutil.NewJob(testutil.WithUserID(userID), testutil.WithScheduledAt(time.Now().Add(-time.Minute)), testutil.WithMaxRetries(3))
	created, err := repo.Create(ctx, job)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Claim, then reschedule
	if _, err := repo.Claim(ctx, "worker-1", 10); err != nil {
		t.Fatalf("claim: %v", err)
	}
	retryAt := time.Now().Add(30 * time.Second)
	if err := repo.Reschedule(ctx, created.ID, "timeout", retryAt); err != nil {
		t.Fatalf("reschedule: %v", err)
	}
	got, err := repo.GetByID(ctx, created.ID, userID)
	if err != nil {
		t.Fatalf("get after reschedule: %v", err)
	}
	if got.Status != domain.StatusPending {
		t.Fatalf("status = %s, want pending", got.Status)
	}
	if got.RetryCount != 1 {
		t.Fatalf("retry_count = %d, want 1", got.RetryCount)
	}

	// Now fail permanently
	if _, err := repo.Claim(ctx, "worker-1", 10); err != nil {
		t.Fatalf("claim 2: %v", err)
	}
	if err := repo.Fail(ctx, created.ID, "permanent error"); err != nil {
		t.Fatalf("fail: %v", err)
	}
	got, err = repo.GetByID(ctx, created.ID, userID)
	if err != nil {
		t.Fatalf("get after fail: %v", err)
	}
	if got.Status != domain.StatusFailed {
		t.Fatalf("status = %s, want failed", got.Status)
	}
}

func TestJobRepo_Cancel(t *testing.T) {
	repo, userID := setupJobRepo(t)
	ctx := context.Background()

	job := testutil.NewJob(testutil.WithUserID(userID))
	created, err := repo.Create(ctx, job)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Cancel pending job
	if err := repo.Cancel(ctx, created.ID, userID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	got, err := repo.GetByID(ctx, created.ID, userID)
	if err != nil {
		t.Fatalf("get after cancel: %v", err)
	}
	if got.Status != domain.StatusCancelled {
		t.Fatalf("status = %s, want cancelled", got.Status)
	}

	// Cancel already cancelled → ErrJobNotCancellable
	err = repo.Cancel(ctx, created.ID, userID)
	if err != domain.ErrJobNotCancellable {
		t.Fatalf("err = %v, want ErrJobNotCancellable", err)
	}
}

func TestJobRepo_Cancel_NotFound(t *testing.T) {
	repo, _ := setupJobRepo(t)
	err := repo.Cancel(context.Background(), "nonexistent", "no-user")
	if err != domain.ErrJobNotFound {
		t.Fatalf("err = %v, want ErrJobNotFound", err)
	}
}

func TestJobRepo_RescheduleStale(t *testing.T) {
	repo, userID := setupJobRepo(t)
	ctx := context.Background()

	// Create and claim a job (makes it running with heartbeat_at = NOW())
	job := testutil.NewJob(testutil.WithUserID(userID), testutil.WithScheduledAt(time.Now().Add(-time.Minute)), testutil.WithMaxRetries(3))
	created, err := repo.Create(ctx, job)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := repo.Claim(ctx, "worker-1", 10); err != nil {
		t.Fatalf("claim: %v", err)
	}

	// With staleCutoff in the future, the job's heartbeat is "stale"
	staleCutoff := time.Now().Add(time.Hour)
	count, err := repo.RescheduleStale(ctx, staleCutoff, 100)
	if err != nil {
		t.Fatalf("reschedule stale: %v", err)
	}
	if count != 1 {
		t.Fatalf("rescheduled %d, want 1", count)
	}

	got, err := repo.GetByID(ctx, created.ID, userID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != domain.StatusPending {
		t.Fatalf("status = %s, want pending", got.Status)
	}
}

func TestJobRepo_FailStale(t *testing.T) {
	repo, userID := setupJobRepo(t)
	ctx := context.Background()

	// Job with 0 max retries — should be failed by reaper
	job := testutil.NewJob(testutil.WithUserID(userID), testutil.WithScheduledAt(time.Now().Add(-time.Minute)), testutil.WithMaxRetries(0))
	created, err := repo.Create(ctx, job)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := repo.Claim(ctx, "worker-1", 10); err != nil {
		t.Fatalf("claim: %v", err)
	}

	staleCutoff := time.Now().Add(time.Hour)
	count, err := repo.FailStale(ctx, staleCutoff, 100)
	if err != nil {
		t.Fatalf("fail stale: %v", err)
	}
	if count != 1 {
		t.Fatalf("failed %d, want 1", count)
	}

	got, err := repo.GetByID(ctx, created.ID, userID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != domain.StatusFailed {
		t.Fatalf("status = %s, want failed", got.Status)
	}
}

func TestJobRepo_ListJobs(t *testing.T) {
	repo, userID := setupJobRepo(t)
	ctx := context.Background()

	// Create 3 jobs
	for i := 0; i < 3; i++ {
		job := testutil.NewJob(testutil.WithUserID(userID), testutil.WithScheduledAt(time.Now().Add(time.Duration(i)*time.Minute)))
		if _, err := repo.Create(ctx, job); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}

	jobs, err := repo.ListJobs(ctx, repository.ListJobsInput{UserID: userID, Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(jobs) != 3 {
		t.Fatalf("got %d jobs, want 3", len(jobs))
	}

	// Filter by status
	jobs, err = repo.ListJobs(ctx, repository.ListJobsInput{UserID: userID, Status: domain.StatusPending, Limit: 10})
	if err != nil {
		t.Fatalf("list filtered: %v", err)
	}
	if len(jobs) != 3 {
		t.Fatalf("filtered got %d jobs, want 3", len(jobs))
	}
}
