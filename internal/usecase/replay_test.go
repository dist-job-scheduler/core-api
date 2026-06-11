package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/testutil"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/usecase"
)

func TestReplayJob_HappyPath(t *testing.T) {
	t.Parallel()

	failed := testutil.NewJob(
		testutil.WithUserID("user-1"),
		testutil.WithJobID("job-failed"),
		testutil.WithStatus(domain.StatusFailed),
		testutil.WithURL("https://example.com/hook"),
	)

	var created *domain.Job
	jobRepo := &testutil.MockJobRepository{
		GetByIDFn: func(_ context.Context, id, uid string) (*domain.Job, error) {
			if id == "job-failed" && uid == "user-1" {
				return failed, nil
			}
			return nil, domain.ErrJobNotFound
		},
		CreateFn: func(_ context.Context, j *domain.Job) (*domain.Job, error) {
			j.ID = "job-replay"
			created = j
			return j, nil
		},
	}
	uc := usecase.NewJobUsecase(jobRepo, &testutil.MockAttemptRepository{}, &testutil.MockCreditRepository{})

	got, err := uc.Replay(context.Background(), "job-failed", "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != "job-replay" {
		t.Errorf("id = %q, want job-replay", got.ID)
	}
	if created.Status != domain.StatusPending {
		t.Errorf("clone status = %q, want pending", created.Status)
	}
	if created.ReplayOf == nil || *created.ReplayOf != "job-failed" {
		t.Errorf("clone replay_of = %v, want job-failed", created.ReplayOf)
	}
	if created.URL != "https://example.com/hook" {
		t.Errorf("clone url = %q, want carried over from source", created.URL)
	}
	if created.IdempotencyKey == failed.IdempotencyKey {
		t.Error("clone must get a fresh idempotency key, not reuse the source's")
	}
}

func TestReplayJob_NotFailed(t *testing.T) {
	t.Parallel()

	for _, status := range []domain.Status{
		domain.StatusPending, domain.StatusRunning, domain.StatusCompleted, domain.StatusCancelled,
	} {
		job := testutil.NewJob(testutil.WithUserID("user-1"), testutil.WithStatus(status))
		jobRepo := &testutil.MockJobRepository{
			GetByIDFn: func(_ context.Context, _, _ string) (*domain.Job, error) { return job, nil },
			CreateFn: func(_ context.Context, _ *domain.Job) (*domain.Job, error) {
				t.Fatal("Create must not be called for a non-failed job")
				return nil, nil
			},
		}
		uc := usecase.NewJobUsecase(jobRepo, &testutil.MockAttemptRepository{}, &testutil.MockCreditRepository{})

		_, err := uc.Replay(context.Background(), "job-1", "user-1")
		if !errors.Is(err, domain.ErrJobNotReplayable) {
			t.Errorf("status %q: err = %v, want ErrJobNotReplayable", status, err)
		}
	}
}

func TestReplayJob_NotFound(t *testing.T) {
	t.Parallel()

	jobRepo := &testutil.MockJobRepository{
		GetByIDFn: func(_ context.Context, _, _ string) (*domain.Job, error) {
			return nil, domain.ErrJobNotFound
		},
	}
	uc := usecase.NewJobUsecase(jobRepo, &testutil.MockAttemptRepository{}, &testutil.MockCreditRepository{})

	_, err := uc.Replay(context.Background(), "missing", "user-1")
	if !errors.Is(err, domain.ErrJobNotFound) {
		t.Errorf("err = %v, want ErrJobNotFound", err)
	}
}

func TestReplayJob_InsufficientCredits(t *testing.T) {
	t.Parallel()

	failed := testutil.NewJob(testutil.WithUserID("user-1"), testutil.WithStatus(domain.StatusFailed))
	jobRepo := &testutil.MockJobRepository{
		GetByIDFn: func(_ context.Context, _, _ string) (*domain.Job, error) { return failed, nil },
		CreateFn: func(_ context.Context, _ *domain.Job) (*domain.Job, error) {
			t.Fatal("Create must not be called when out of credits")
			return nil, nil
		},
	}
	credits := &testutil.MockCreditRepository{
		HasCreditsFn: func(_ context.Context, _ string) (bool, error) { return false, nil },
	}
	uc := usecase.NewJobUsecase(jobRepo, &testutil.MockAttemptRepository{}, credits)

	_, err := uc.Replay(context.Background(), "job-1", "user-1")
	if !errors.Is(err, domain.ErrInsufficientCredits) {
		t.Errorf("err = %v, want ErrInsufficientCredits", err)
	}
}

func TestReplayBufferItem_HappyPath(t *testing.T) {
	t.Parallel()

	body := `{"x":1}`
	failed := &domain.BufferItem{
		ID:         "item-failed",
		BufferID:   "buf-1",
		UserID:     "user-1",
		URL:        "https://example.com/api",
		Method:     "POST",
		Body:       &body,
		MaxRetries: 3,
		Status:     domain.BufferItemFailed,
	}
	var created *domain.BufferItem
	bufRepo := &testutil.MockBufferRepository{
		GetByIDFn: func(_ context.Context, id, uid string) (*domain.Buffer, error) {
			if id == "buf-1" && uid == "user-1" {
				return &domain.Buffer{ID: "buf-1", UserID: "user-1"}, nil
			}
			return nil, domain.ErrBufferNotFound
		},
		GetItemByIDFn: func(_ context.Context, itemID, bufferID string) (*domain.BufferItem, error) {
			if itemID == "item-failed" && bufferID == "buf-1" {
				return failed, nil
			}
			return nil, domain.ErrBufferItemNotFound
		},
		CreateItemFn: func(_ context.Context, it *domain.BufferItem) (*domain.BufferItem, error) {
			it.ID = "item-replay"
			created = it
			return it, nil
		},
	}
	uc := usecase.NewBufferUsecase(bufRepo, &testutil.MockCreditRepository{})

	got, err := uc.ReplayItem(context.Background(), "item-failed", "buf-1", "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != "item-replay" {
		t.Errorf("id = %q, want item-replay", got.ID)
	}
	if created.Status != domain.BufferItemPending {
		t.Errorf("clone status = %q, want pending", created.Status)
	}
	if created.ReplayOf == nil || *created.ReplayOf != "item-failed" {
		t.Errorf("clone replay_of = %v, want item-failed", created.ReplayOf)
	}
	if created.URL != failed.URL {
		t.Errorf("clone url = %q, want carried over", created.URL)
	}
}

func TestReplayBufferItem_NotFailed(t *testing.T) {
	t.Parallel()

	item := &domain.BufferItem{ID: "item-1", BufferID: "buf-1", UserID: "user-1", Status: domain.BufferItemCompleted}
	bufRepo := &testutil.MockBufferRepository{
		GetByIDFn: func(_ context.Context, _, _ string) (*domain.Buffer, error) {
			return &domain.Buffer{ID: "buf-1", UserID: "user-1"}, nil
		},
		GetItemByIDFn: func(_ context.Context, _, _ string) (*domain.BufferItem, error) { return item, nil },
		CreateItemFn: func(_ context.Context, _ *domain.BufferItem) (*domain.BufferItem, error) {
			t.Fatal("CreateItem must not be called for a non-failed item")
			return nil, nil
		},
	}
	uc := usecase.NewBufferUsecase(bufRepo, &testutil.MockCreditRepository{})

	_, err := uc.ReplayItem(context.Background(), "item-1", "buf-1", "user-1")
	if !errors.Is(err, domain.ErrBufferItemNotReplayable) {
		t.Errorf("err = %v, want ErrBufferItemNotReplayable", err)
	}
}

func TestReplayBufferItem_BufferNotOwned(t *testing.T) {
	t.Parallel()

	bufRepo := &testutil.MockBufferRepository{
		GetByIDFn: func(_ context.Context, _, _ string) (*domain.Buffer, error) {
			return nil, domain.ErrBufferNotFound
		},
	}
	uc := usecase.NewBufferUsecase(bufRepo, &testutil.MockCreditRepository{})

	_, err := uc.ReplayItem(context.Background(), "item-1", "buf-1", "user-1")
	if !errors.Is(err, domain.ErrBufferNotFound) {
		t.Errorf("err = %v, want ErrBufferNotFound", err)
	}
}

func TestReplayBufferItem_InsufficientCredits(t *testing.T) {
	t.Parallel()

	failed := &domain.BufferItem{ID: "item-1", BufferID: "buf-1", UserID: "user-1", Status: domain.BufferItemFailed}
	bufRepo := &testutil.MockBufferRepository{
		GetByIDFn: func(_ context.Context, _, _ string) (*domain.Buffer, error) {
			return &domain.Buffer{ID: "buf-1", UserID: "user-1"}, nil
		},
		GetItemByIDFn: func(_ context.Context, _, _ string) (*domain.BufferItem, error) { return failed, nil },
		CreateItemFn: func(_ context.Context, _ *domain.BufferItem) (*domain.BufferItem, error) {
			t.Fatal("CreateItem must not be called when out of credits")
			return nil, nil
		},
	}
	credits := &testutil.MockCreditRepository{
		HasCreditsFn: func(_ context.Context, _ string) (bool, error) { return false, nil },
	}
	uc := usecase.NewBufferUsecase(bufRepo, credits)

	_, err := uc.ReplayItem(context.Background(), "item-1", "buf-1", "user-1")
	if !errors.Is(err, domain.ErrInsufficientCredits) {
		t.Errorf("err = %v, want ErrInsufficientCredits", err)
	}
}
