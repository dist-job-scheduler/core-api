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

func TestCreateSchedule_HappyPath(t *testing.T) {
	t.Parallel()

	schedRepo := &testutil.MockScheduleRepository{
		CreateFn: func(_ context.Context, s *domain.Schedule) (*domain.Schedule, error) {
			s.ID = "sched-123"
			s.CreatedAt = time.Now().UTC()
			return s, nil
		},
	}
	uc := usecase.NewScheduleUsecase(schedRepo, &testutil.MockJobRepository{})

	got, err := uc.CreateSchedule(context.Background(), usecase.CreateScheduleInput{
		UserID:   "user-1",
		Name:     "my-schedule",
		CronExpr: "*/5 * * * *",
		URL:      "https://example.com/hook",
		Method:   "POST",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != "sched-123" {
		t.Fatalf("expected schedule ID sched-123, got %s", got.ID)
	}
	if got.NextRunAt.IsZero() {
		t.Fatal("expected next_run_at to be set, got zero time")
	}
	if got.Paused {
		t.Fatal("expected schedule to not be paused")
	}
}

func TestCreateSchedule_InvalidCron(t *testing.T) {
	t.Parallel()

	uc := usecase.NewScheduleUsecase(&testutil.MockScheduleRepository{}, &testutil.MockJobRepository{})

	_, err := uc.CreateSchedule(context.Background(), usecase.CreateScheduleInput{
		UserID:   "user-1",
		Name:     "bad-cron",
		CronExpr: "not a cron expression",
		URL:      "https://example.com/hook",
		Method:   "POST",
	})
	if !errors.Is(err, domain.ErrInvalidCronExpr) {
		t.Fatalf("expected ErrInvalidCronExpr, got %v", err)
	}
}

func TestCreateSchedule_DefaultValues(t *testing.T) {
	t.Parallel()

	var created *domain.Schedule
	schedRepo := &testutil.MockScheduleRepository{
		CreateFn: func(_ context.Context, s *domain.Schedule) (*domain.Schedule, error) {
			created = s
			s.ID = "sched-defaults"
			return s, nil
		},
	}
	uc := usecase.NewScheduleUsecase(schedRepo, &testutil.MockJobRepository{})

	_, err := uc.CreateSchedule(context.Background(), usecase.CreateScheduleInput{
		UserID:   "user-1",
		Name:     "default-schedule",
		CronExpr: "0 * * * *",
		URL:      "https://example.com/hook",
		Method:   "GET",
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

func TestGetSchedule_HappyPath(t *testing.T) {
	t.Parallel()

	want := testutil.NewSchedule(testutil.WithScheduleID("sched-1"), testutil.WithScheduleUserID("user-1"))
	schedRepo := &testutil.MockScheduleRepository{
		GetByIDFn: func(_ context.Context, id, userID string) (*domain.Schedule, error) {
			if id == "sched-1" && userID == "user-1" {
				return want, nil
			}
			return nil, domain.ErrScheduleNotFound
		},
	}
	uc := usecase.NewScheduleUsecase(schedRepo, &testutil.MockJobRepository{})

	got, err := uc.GetSchedule(context.Background(), "sched-1", "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != want.ID {
		t.Fatalf("expected schedule ID %s, got %s", want.ID, got.ID)
	}
}

func TestGetSchedule_NotFound(t *testing.T) {
	t.Parallel()

	schedRepo := &testutil.MockScheduleRepository{
		GetByIDFn: func(_ context.Context, _, _ string) (*domain.Schedule, error) {
			return nil, domain.ErrScheduleNotFound
		},
	}
	uc := usecase.NewScheduleUsecase(schedRepo, &testutil.MockJobRepository{})

	_, err := uc.GetSchedule(context.Background(), "no-such-sched", "user-1")
	if !errors.Is(err, domain.ErrScheduleNotFound) {
		t.Fatalf("expected ErrScheduleNotFound, got %v", err)
	}
}

func TestListSchedules_DefaultLimit(t *testing.T) {
	t.Parallel()

	var capturedInput repository.ListSchedulesInput
	schedRepo := &testutil.MockScheduleRepository{
		ListFn: func(_ context.Context, input repository.ListSchedulesInput) ([]*domain.Schedule, error) {
			capturedInput = input
			return nil, nil
		},
	}
	uc := usecase.NewScheduleUsecase(schedRepo, &testutil.MockJobRepository{})

	_, err := uc.ListSchedules(context.Background(), usecase.ListSchedulesInput{
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

func TestPauseSchedule_HappyPath(t *testing.T) {
	t.Parallel()

	var capturedPaused bool
	schedRepo := &testutil.MockScheduleRepository{
		SetPausedFn: func(_ context.Context, id, userID string, paused bool) error {
			capturedPaused = paused
			if id != "sched-1" || userID != "user-1" {
				t.Fatalf("unexpected args: id=%s userID=%s", id, userID)
			}
			return nil
		},
	}
	uc := usecase.NewScheduleUsecase(schedRepo, &testutil.MockJobRepository{})

	err := uc.PauseSchedule(context.Background(), "sched-1", "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !capturedPaused {
		t.Fatal("expected SetPaused to be called with paused=true")
	}
}

func TestPauseSchedule_NotFound(t *testing.T) {
	t.Parallel()

	schedRepo := &testutil.MockScheduleRepository{
		SetPausedFn: func(_ context.Context, _, _ string, _ bool) error {
			return domain.ErrScheduleNotFound
		},
	}
	uc := usecase.NewScheduleUsecase(schedRepo, &testutil.MockJobRepository{})

	err := uc.PauseSchedule(context.Background(), "no-such-sched", "user-1")
	if !errors.Is(err, domain.ErrScheduleNotFound) {
		t.Fatalf("expected ErrScheduleNotFound, got %v", err)
	}
}

func TestPauseSchedule_AlreadyPaused(t *testing.T) {
	t.Parallel()

	schedRepo := &testutil.MockScheduleRepository{
		SetPausedFn: func(_ context.Context, _, _ string, _ bool) error {
			return domain.ErrScheduleAlreadyPaused
		},
	}
	uc := usecase.NewScheduleUsecase(schedRepo, &testutil.MockJobRepository{})

	err := uc.PauseSchedule(context.Background(), "sched-1", "user-1")
	if !errors.Is(err, domain.ErrScheduleAlreadyPaused) {
		t.Fatalf("expected ErrScheduleAlreadyPaused, got %v", err)
	}
}

func TestResumeSchedule_HappyPath(t *testing.T) {
	t.Parallel()

	var capturedPaused bool
	called := false
	schedRepo := &testutil.MockScheduleRepository{
		SetPausedFn: func(_ context.Context, _, _ string, paused bool) error {
			capturedPaused = paused
			called = true
			return nil
		},
	}
	uc := usecase.NewScheduleUsecase(schedRepo, &testutil.MockJobRepository{})

	err := uc.ResumeSchedule(context.Background(), "sched-1", "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("expected SetPaused to be called")
	}
	if capturedPaused {
		t.Fatal("expected SetPaused to be called with paused=false")
	}
}

func TestDeleteSchedule_HappyPath(t *testing.T) {
	t.Parallel()

	called := false
	schedRepo := &testutil.MockScheduleRepository{
		DeleteFn: func(_ context.Context, id, userID string) error {
			called = true
			if id != "sched-1" || userID != "user-1" {
				t.Fatalf("unexpected args: id=%s userID=%s", id, userID)
			}
			return nil
		},
	}
	uc := usecase.NewScheduleUsecase(schedRepo, &testutil.MockJobRepository{})

	err := uc.DeleteSchedule(context.Background(), "sched-1", "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("expected repo.Delete to be called")
	}
}

func TestListScheduleJobs_HappyPath(t *testing.T) {
	t.Parallel()

	sched := testutil.NewSchedule(testutil.WithScheduleID("sched-1"), testutil.WithScheduleUserID("user-1"))
	schedRepo := &testutil.MockScheduleRepository{
		GetByIDFn: func(_ context.Context, id, userID string) (*domain.Schedule, error) {
			if id == "sched-1" && userID == "user-1" {
				return sched, nil
			}
			return nil, domain.ErrScheduleNotFound
		},
	}
	jobs := []*domain.Job{
		testutil.NewJob(testutil.WithJobID("job-a")),
		testutil.NewJob(testutil.WithJobID("job-b")),
	}
	jobRepo := &testutil.MockJobRepository{
		ListByScheduleIDFn: func(_ context.Context, scheduleID string, _ int, _ *time.Time, _ string) ([]*domain.Job, error) {
			if scheduleID == "sched-1" {
				return jobs, nil
			}
			return nil, nil
		},
	}
	uc := usecase.NewScheduleUsecase(schedRepo, jobRepo)

	result, err := uc.ListScheduleJobs(context.Background(), usecase.ListScheduleJobsInput{
		ScheduleID: "sched-1",
		UserID:     "user-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Jobs) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(result.Jobs))
	}
}

func TestListScheduleJobs_ScheduleNotFound(t *testing.T) {
	t.Parallel()

	schedRepo := &testutil.MockScheduleRepository{
		GetByIDFn: func(_ context.Context, _, _ string) (*domain.Schedule, error) {
			return nil, domain.ErrScheduleNotFound
		},
	}
	uc := usecase.NewScheduleUsecase(schedRepo, &testutil.MockJobRepository{})

	_, err := uc.ListScheduleJobs(context.Background(), usecase.ListScheduleJobsInput{
		ScheduleID: "no-such-sched",
		UserID:     "user-1",
	})
	if !errors.Is(err, domain.ErrScheduleNotFound) {
		t.Fatalf("expected ErrScheduleNotFound, got %v", err)
	}
}
