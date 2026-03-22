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

func setupAttemptRepo(t *testing.T) (*postgres.AttemptRepository, *postgres.JobRepository, string) {
	t.Helper()
	pool := testutil.SetupTestDB(t)
	testutil.TruncateAll(t, pool)
	userID := testutil.UserID()
	testutil.SeedUser(t, pool, userID)
	return postgres.NewAttemptRepository(pool), postgres.NewJobRepository(pool), userID
}

func createTestJob(t *testing.T, jobRepo *postgres.JobRepository, userID string) *domain.Job {
	t.Helper()
	job := testutil.NewJob(testutil.WithUserID(userID), testutil.WithScheduledAt(time.Now().Add(-time.Minute)))
	created, err := jobRepo.Create(context.Background(), job)
	if err != nil {
		t.Fatalf("create test job: %v", err)
	}
	return created
}

func TestAttemptRepo_CreateAndComplete(t *testing.T) {
	attemptRepo, jobRepo, userID := setupAttemptRepo(t)
	ctx := context.Background()
	job := createTestJob(t, jobRepo, userID)

	attempt, err := attemptRepo.CreateAttempt(ctx, &domain.JobAttempt{
		JobID:      job.ID,
		AttemptNum: 1,
		WorkerID:   "worker-test",
		StartedAt:  time.Now(),
	})
	if err != nil {
		t.Fatalf("create attempt: %v", err)
	}
	if attempt.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if attempt.CompletedAt != nil {
		t.Fatal("expected nil completed_at for open attempt")
	}

	// Complete the attempt
	statusCode := 200
	var durationMS int64 = 150
	if err := attemptRepo.CompleteAttempt(ctx, attempt.ID, &statusCode, nil, durationMS); err != nil {
		t.Fatalf("complete attempt: %v", err)
	}
}

func TestAttemptRepo_DuplicateAttemptNum(t *testing.T) {
	attemptRepo, jobRepo, userID := setupAttemptRepo(t)
	ctx := context.Background()
	job := createTestJob(t, jobRepo, userID)

	a := &domain.JobAttempt{JobID: job.ID, AttemptNum: 1, WorkerID: "worker-1", StartedAt: time.Now()}
	if _, err := attemptRepo.CreateAttempt(ctx, a); err != nil {
		t.Fatalf("first create: %v", err)
	}

	// Duplicate (job_id, attempt_num) should fail due to UNIQUE constraint
	_, err := attemptRepo.CreateAttempt(ctx, a)
	if err == nil {
		t.Fatal("expected error on duplicate attempt_num")
	}
}

func TestAttemptRepo_ListByJobID(t *testing.T) {
	attemptRepo, jobRepo, userID := setupAttemptRepo(t)
	ctx := context.Background()
	job := createTestJob(t, jobRepo, userID)

	// Create 3 attempts
	for i := 1; i <= 3; i++ {
		a := &domain.JobAttempt{JobID: job.ID, AttemptNum: i, WorkerID: "worker-1", StartedAt: time.Now().Add(time.Duration(i) * time.Second)}
		if _, err := attemptRepo.CreateAttempt(ctx, a); err != nil {
			t.Fatalf("create attempt %d: %v", i, err)
		}
	}

	attempts, err := attemptRepo.ListByJobID(ctx, job.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(attempts) != 3 {
		t.Fatalf("got %d attempts, want 3", len(attempts))
	}
	// Verify ordering (ASC by started_at)
	for i := 1; i < len(attempts); i++ {
		if attempts[i].StartedAt.Before(attempts[i-1].StartedAt) {
			t.Fatal("attempts not ordered by started_at ASC")
		}
	}
}
