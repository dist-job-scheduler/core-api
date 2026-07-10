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

func TestJobRepo_ListJobs_SearchFilters(t *testing.T) {
	repo, userID := setupJobRepo(t)
	ctx := context.Background()

	base := time.Now().Truncate(time.Second)
	// Three jobs with distinct urls, methods, and scheduled_at.
	seed := []struct {
		url    string
		method string
		at     time.Time
	}{
		{"https://api.example.com/orders", "POST", base.Add(1 * time.Hour)},
		{"https://api.example.com/users", "GET", base.Add(2 * time.Hour)},
		{"https://hooks.other.io/notify", "POST", base.Add(3 * time.Hour)},
	}
	for i, s := range seed {
		job := testutil.NewJob(
			testutil.WithUserID(userID),
			testutil.WithURL(s.url),
			testutil.WithMethod(s.method),
			testutil.WithScheduledAt(s.at),
		)
		if _, err := repo.Create(ctx, job); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}

	// URL substring search (case-insensitive) matches the two example.com jobs.
	jobs, err := repo.ListJobs(ctx, repository.ListJobsInput{UserID: userID, URLSearch: "EXAMPLE.com", Limit: 10})
	if err != nil {
		t.Fatalf("list url search: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("url search got %d jobs, want 2", len(jobs))
	}

	// Method filter matches the two POST jobs.
	jobs, err = repo.ListJobs(ctx, repository.ListJobsInput{UserID: userID, Method: "POST", Limit: 10})
	if err != nil {
		t.Fatalf("list method: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("method filter got %d jobs, want 2", len(jobs))
	}

	// Combined method + URL search narrows to the single orders job.
	jobs, err = repo.ListJobs(ctx, repository.ListJobsInput{UserID: userID, Method: "POST", URLSearch: "orders", Limit: 10})
	if err != nil {
		t.Fatalf("list method+url: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("method+url got %d jobs, want 1", len(jobs))
	}
	if jobs[0].URL != "https://api.example.com/orders" {
		t.Fatalf("got url %s, want orders job", jobs[0].URL)
	}

	// Time range: only jobs scheduled within (base+90m, base+150m] → the users job.
	after := base.Add(90 * time.Minute)
	before := base.Add(150 * time.Minute)
	jobs, err = repo.ListJobs(ctx, repository.ListJobsInput{
		UserID:          userID,
		ScheduledAfter:  &after,
		ScheduledBefore: &before,
		Limit:           10,
	})
	if err != nil {
		t.Fatalf("list time range: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("time range got %d jobs, want 1", len(jobs))
	}
	if jobs[0].URL != "https://api.example.com/users" {
		t.Fatalf("got url %s, want users job", jobs[0].URL)
	}
}

// TestJobRepo_ListJobs_ErrorSearch covers the last_error substring filter — the
// debugging path behind the dashboard's Failures view ("show me everything that
// failed with this upstream message").
func TestJobRepo_ListJobs_ErrorSearch(t *testing.T) {
	repo, userID := setupJobRepo(t)
	ctx := context.Background()

	seed := []struct {
		url     string
		failErr string // empty = leave job pending with no last_error
	}{
		{"https://api.example.com/a", "dial tcp: connection refused"},
		{"https://api.example.com/b", "context deadline exceeded (timeout)"},
		{"https://api.example.com/c", ""},
	}
	for i, s := range seed {
		job := testutil.NewJob(testutil.WithUserID(userID), testutil.WithURL(s.url))
		created, err := repo.Create(ctx, job)
		if err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
		if s.failErr != "" {
			if err := repo.Fail(ctx, created.ID, s.failErr); err != nil {
				t.Fatalf("fail %d: %v", i, err)
			}
		}
	}

	// Case-insensitive substring match on last_error finds the one refused job.
	jobs, err := repo.ListJobs(ctx, repository.ListJobsInput{UserID: userID, ErrorSearch: "Connection Refused", Limit: 10})
	if err != nil {
		t.Fatalf("list error search: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("error search got %d jobs, want 1", len(jobs))
	}
	if jobs[0].URL != "https://api.example.com/a" {
		t.Fatalf("got url %s, want the refused job", jobs[0].URL)
	}

	// A term present in no last_error returns nothing (the pending job has a NULL
	// last_error and must never match an ILIKE filter).
	jobs, err = repo.ListJobs(ctx, repository.ListJobsInput{UserID: userID, ErrorSearch: "no-such-error", Limit: 10})
	if err != nil {
		t.Fatalf("list error search miss: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("error search miss got %d jobs, want 0", len(jobs))
	}

	// Combining error search with a URL search narrows with AND semantics.
	jobs, err = repo.ListJobs(ctx, repository.ListJobsInput{
		UserID:      userID,
		URLSearch:   "/b",
		ErrorSearch: "timeout",
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("list url+error: %v", err)
	}
	if len(jobs) != 1 || jobs[0].URL != "https://api.example.com/b" {
		t.Fatalf("url+error search got %d jobs, want the timeout job", len(jobs))
	}
}

// TestJobRepo_ListJobs_SearchEscapesWildcards ensures a search term containing
// LIKE metacharacters is matched literally rather than as a wildcard.
func TestJobRepo_ListJobs_SearchEscapesWildcards(t *testing.T) {
	repo, userID := setupJobRepo(t)
	ctx := context.Background()

	for _, u := range []string{"https://x.io/a%b", "https://x.io/azzb"} {
		job := testutil.NewJob(testutil.WithUserID(userID), testutil.WithURL(u))
		if _, err := repo.Create(ctx, job); err != nil {
			t.Fatalf("create %s: %v", u, err)
		}
	}

	// "a%b" must match only the literal "a%b" url, not "azzb".
	jobs, err := repo.ListJobs(ctx, repository.ListJobsInput{UserID: userID, URLSearch: "a%b", Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("escaped search got %d jobs, want 1", len(jobs))
	}
	if jobs[0].URL != "https://x.io/a%b" {
		t.Fatalf("got url %s, want literal a%%b job", jobs[0].URL)
	}
}
