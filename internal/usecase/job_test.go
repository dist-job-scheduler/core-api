package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/repository"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/testutil"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/usecase"
)

func TestCreateJob_HappyPath(t *testing.T) {
	t.Parallel()

	jobRepo := &testutil.MockJobRepository{
		CreateFn: func(_ context.Context, j *domain.Job) (*domain.Job, error) {
			j.ID = "job-123"
			j.CreatedAt = time.Now().UTC()
			return j, nil
		},
	}
	creditRepo := &testutil.MockCreditRepository{
		HasCreditsFn: func(_ context.Context, _ string) (bool, error) {
			return true, nil
		},
	}
	uc := usecase.NewJobUsecase(jobRepo, &testutil.MockAttemptRepository{}, creditRepo)

	got, err := uc.CreateJob(context.Background(), usecase.CreateJobInput{
		UserID:      "user-1",
		URL:         "https://example.com/hook",
		Method:      "POST",
		ScheduledAt: time.Now().Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != "job-123" {
		t.Fatalf("expected job ID job-123, got %s", got.ID)
	}
	if got.Status != domain.StatusPending {
		t.Fatalf("expected status pending, got %s", got.Status)
	}
}

func TestCreateJob_NoCredits(t *testing.T) {
	t.Parallel()

	creditRepo := &testutil.MockCreditRepository{
		HasCreditsFn: func(_ context.Context, _ string) (bool, error) {
			return false, nil
		},
	}
	uc := usecase.NewJobUsecase(&testutil.MockJobRepository{}, &testutil.MockAttemptRepository{}, creditRepo)

	_, err := uc.CreateJob(context.Background(), usecase.CreateJobInput{
		UserID:      "user-1",
		URL:         "https://example.com/hook",
		Method:      "POST",
		ScheduledAt: time.Now().Add(time.Minute),
	})
	if !errors.Is(err, domain.ErrInsufficientCredits) {
		t.Fatalf("expected ErrInsufficientCredits, got %v", err)
	}
}

func TestCreateJob_DefaultValues(t *testing.T) {
	t.Parallel()

	var created *domain.Job
	jobRepo := &testutil.MockJobRepository{
		CreateFn: func(_ context.Context, j *domain.Job) (*domain.Job, error) {
			created = j
			j.ID = "job-defaults"
			return j, nil
		},
	}
	creditRepo := &testutil.MockCreditRepository{
		HasCreditsFn: func(_ context.Context, _ string) (bool, error) {
			return true, nil
		},
	}
	uc := usecase.NewJobUsecase(jobRepo, &testutil.MockAttemptRepository{}, creditRepo)

	_, err := uc.CreateJob(context.Background(), usecase.CreateJobInput{
		UserID:      "user-1",
		URL:         "https://example.com/hook",
		Method:      "POST",
		ScheduledAt: time.Now().Add(time.Minute),
		// TimeoutSeconds, MaxRetries, Backoff all left at zero values
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created.TimeoutSeconds != 30 {
		t.Fatalf("expected default timeout 30, got %d", created.TimeoutSeconds)
	}
	if created.MaxRetries != 3 {
		t.Fatalf("expected default max retries 3, got %d", created.MaxRetries)
	}
	if created.Backoff != domain.BackoffExponential {
		t.Fatalf("expected default backoff exponential, got %s", created.Backoff)
	}
}

func TestCreateJob_GeneratesIdempotencyKey(t *testing.T) {
	t.Parallel()

	var created *domain.Job
	jobRepo := &testutil.MockJobRepository{
		CreateFn: func(_ context.Context, j *domain.Job) (*domain.Job, error) {
			created = j
			j.ID = "job-idem"
			return j, nil
		},
	}
	creditRepo := &testutil.MockCreditRepository{
		HasCreditsFn: func(_ context.Context, _ string) (bool, error) {
			return true, nil
		},
	}
	uc := usecase.NewJobUsecase(jobRepo, &testutil.MockAttemptRepository{}, creditRepo)

	_, err := uc.CreateJob(context.Background(), usecase.CreateJobInput{
		UserID:         "user-1",
		IdempotencyKey: "", // empty — should be auto-generated
		URL:            "https://example.com/hook",
		Method:         "POST",
		ScheduledAt:    time.Now().Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created.IdempotencyKey == "" {
		t.Fatal("expected idempotency key to be generated, got empty string")
	}
}

func TestCancelJob_HappyPath(t *testing.T) {
	t.Parallel()

	called := false
	jobRepo := &testutil.MockJobRepository{
		CancelFn: func(_ context.Context, jobID, userID string) error {
			called = true
			if jobID != "job-1" || userID != "user-1" {
				t.Fatalf("unexpected args: jobID=%s userID=%s", jobID, userID)
			}
			return nil
		},
	}
	uc := usecase.NewJobUsecase(jobRepo, &testutil.MockAttemptRepository{}, &testutil.MockCreditRepository{})

	err := uc.CancelJob(context.Background(), "job-1", "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("expected repo.Cancel to be called")
	}
}

func TestCancelJob_NotFound(t *testing.T) {
	t.Parallel()

	jobRepo := &testutil.MockJobRepository{
		CancelFn: func(_ context.Context, _, _ string) error {
			return domain.ErrJobNotFound
		},
	}
	uc := usecase.NewJobUsecase(jobRepo, &testutil.MockAttemptRepository{}, &testutil.MockCreditRepository{})

	err := uc.CancelJob(context.Background(), "no-such-job", "user-1")
	if !errors.Is(err, domain.ErrJobNotFound) {
		t.Fatalf("expected ErrJobNotFound, got %v", err)
	}
}

func TestGetByID_HappyPath(t *testing.T) {
	t.Parallel()

	want := testutil.NewJob(testutil.WithJobID("job-42"), testutil.WithUserID("user-1"))
	jobRepo := &testutil.MockJobRepository{
		GetByIDFn: func(_ context.Context, jobID, userID string) (*domain.Job, error) {
			if jobID == "job-42" && userID == "user-1" {
				return want, nil
			}
			return nil, domain.ErrJobNotFound
		},
	}
	uc := usecase.NewJobUsecase(jobRepo, &testutil.MockAttemptRepository{}, &testutil.MockCreditRepository{})

	got, err := uc.GetByID(context.Background(), "job-42", "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != want.ID {
		t.Fatalf("expected job ID %s, got %s", want.ID, got.ID)
	}
}

func TestGetByID_NotFound(t *testing.T) {
	t.Parallel()

	jobRepo := &testutil.MockJobRepository{
		GetByIDFn: func(_ context.Context, _, _ string) (*domain.Job, error) {
			return nil, domain.ErrJobNotFound
		},
	}
	uc := usecase.NewJobUsecase(jobRepo, &testutil.MockAttemptRepository{}, &testutil.MockCreditRepository{})

	_, err := uc.GetByID(context.Background(), "no-such-job", "user-1")
	if !errors.Is(err, domain.ErrJobNotFound) {
		t.Fatalf("expected ErrJobNotFound, got %v", err)
	}
}

func TestListJobs_DefaultLimit(t *testing.T) {
	t.Parallel()

	var capturedInput repository.ListJobsInput
	jobRepo := &testutil.MockJobRepository{
		ListJobsFn: func(_ context.Context, input repository.ListJobsInput) ([]*domain.Job, error) {
			capturedInput = input
			return nil, nil
		},
	}
	uc := usecase.NewJobUsecase(jobRepo, &testutil.MockAttemptRepository{}, &testutil.MockCreditRepository{})

	_, err := uc.ListJobs(context.Background(), usecase.ListJobsInput{
		UserID: "user-1",
		// Limit left at 0
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Default limit is 20, repo receives limit+1 = 21
	if capturedInput.Limit != 21 {
		t.Fatalf("expected repo limit 21 (default 20 + 1), got %d", capturedInput.Limit)
	}
}

func TestListJobs_ClampLimit(t *testing.T) {
	t.Parallel()

	var capturedInput repository.ListJobsInput
	jobRepo := &testutil.MockJobRepository{
		ListJobsFn: func(_ context.Context, input repository.ListJobsInput) ([]*domain.Job, error) {
			capturedInput = input
			return nil, nil
		},
	}
	uc := usecase.NewJobUsecase(jobRepo, &testutil.MockAttemptRepository{}, &testutil.MockCreditRepository{})

	_, err := uc.ListJobs(context.Background(), usecase.ListJobsInput{
		UserID: "user-1",
		Limit:  999,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Clamped to 100, repo receives 100+1 = 101
	if capturedInput.Limit != 101 {
		t.Fatalf("expected repo limit 101 (clamped 100 + 1), got %d", capturedInput.Limit)
	}
}

func TestListJobs_InvalidStatus(t *testing.T) {
	t.Parallel()

	uc := usecase.NewJobUsecase(&testutil.MockJobRepository{}, &testutil.MockAttemptRepository{}, &testutil.MockCreditRepository{})

	_, err := uc.ListJobs(context.Background(), usecase.ListJobsInput{
		UserID: "user-1",
		Status: "bogus",
	})
	if !errors.Is(err, domain.ErrInvalidStatus) {
		t.Fatalf("expected ErrInvalidStatus, got %v", err)
	}
}

func TestListAttempts_HappyPath(t *testing.T) {
	t.Parallel()

	job := testutil.NewJob(testutil.WithJobID("job-1"), testutil.WithUserID("user-1"))
	jobRepo := &testutil.MockJobRepository{
		GetByIDFn: func(_ context.Context, jobID, userID string) (*domain.Job, error) {
			if jobID == "job-1" && userID == "user-1" {
				return job, nil
			}
			return nil, domain.ErrJobNotFound
		},
	}
	wantAttempts := []*domain.JobAttempt{
		{ID: "att-1", JobID: "job-1", AttemptNum: 1},
		{ID: "att-2", JobID: "job-1", AttemptNum: 2},
	}
	attemptRepo := &testutil.MockAttemptRepository{
		ListByJobIDFn: func(_ context.Context, jobID string) ([]*domain.JobAttempt, error) {
			if jobID == "job-1" {
				return wantAttempts, nil
			}
			return nil, nil
		},
	}
	uc := usecase.NewJobUsecase(jobRepo, attemptRepo, &testutil.MockCreditRepository{})

	got, err := uc.ListAttempts(context.Background(), "job-1", "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 attempts, got %d", len(got))
	}
}

func TestListAttempts_JobNotFound(t *testing.T) {
	t.Parallel()

	jobRepo := &testutil.MockJobRepository{
		GetByIDFn: func(_ context.Context, _, _ string) (*domain.Job, error) {
			return nil, domain.ErrJobNotFound
		},
	}
	uc := usecase.NewJobUsecase(jobRepo, &testutil.MockAttemptRepository{}, &testutil.MockCreditRepository{})

	_, err := uc.ListAttempts(context.Background(), "no-such-job", "user-1")
	if !errors.Is(err, domain.ErrJobNotFound) {
		t.Fatalf("expected ErrJobNotFound, got %v", err)
	}
}
