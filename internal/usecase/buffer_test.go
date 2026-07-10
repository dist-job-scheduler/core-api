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

func ptr[T any](v T) *T { return &v }

// ── CreateBuffer ───────────────────────────────────────────────────────────────

func TestCreateBuffer_Defaults(t *testing.T) {
	t.Parallel()

	var created *domain.Buffer
	repo := &testutil.MockBufferRepository{
		CreateFn: func(_ context.Context, b *domain.Buffer) (*domain.Buffer, error) {
			created = b
			b.ID = "buffer-1"
			return b, nil
		},
	}
	uc := usecase.NewBufferUsecase(repo, &testutil.MockCreditRepository{})

	got, err := uc.CreateBuffer(context.Background(), usecase.CreateBufferInput{
		UserID: "user-1",
		Name:   "my-buffer",
		URL:    "https://example.com/hook",
		Method: "POST",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != "buffer-1" {
		t.Fatalf("expected ID buffer-1, got %s", got.ID)
	}
	if created.TimeoutSeconds != 30 {
		t.Errorf("expected default TimeoutSeconds 30, got %d", created.TimeoutSeconds)
	}
	if created.MaxRetries != 3 {
		t.Errorf("expected default MaxRetries 3, got %d", created.MaxRetries)
	}
	if created.RateLimit != 10 {
		t.Errorf("expected default RateLimit 10, got %d", created.RateLimit)
	}
	if created.Backoff != domain.BackoffExponential {
		t.Errorf("expected default backoff exponential, got %s", created.Backoff)
	}
	if created.Paused {
		t.Error("expected new buffer to be unpaused")
	}
	if created.Headers == nil {
		t.Error("expected Headers to be a non-nil map")
	}
	if created.WebhookHeaders == nil {
		t.Error("expected WebhookHeaders to be a non-nil map")
	}
}

func TestCreateBuffer_ExplicitValuesPreserved(t *testing.T) {
	t.Parallel()

	var created *domain.Buffer
	repo := &testutil.MockBufferRepository{
		CreateFn: func(_ context.Context, b *domain.Buffer) (*domain.Buffer, error) {
			created = b
			return b, nil
		},
	}
	uc := usecase.NewBufferUsecase(repo, &testutil.MockCreditRepository{})

	hook := "https://hook.example.com"
	_, err := uc.CreateBuffer(context.Background(), usecase.CreateBufferInput{
		UserID:         "user-1",
		Name:           "explicit",
		URL:            "https://example.com",
		Method:         "PUT",
		Headers:        map[string]string{"X-A": "1"},
		TimeoutSeconds: 90,
		RateLimit:      50,
		MaxRetries:     ptr(7),
		Backoff:        domain.BackoffLinear,
		WebhookURL:     &hook,
		WebhookHeaders: map[string]string{"X-Hook": "y"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created.TimeoutSeconds != 90 || created.RateLimit != 50 || created.MaxRetries != 7 {
		t.Errorf("explicit numeric values not preserved: %+v", created)
	}
	if created.Backoff != domain.BackoffLinear {
		t.Errorf("explicit backoff not preserved, got %s", created.Backoff)
	}
	if created.WebhookURL == nil || *created.WebhookURL != hook {
		t.Errorf("webhook URL not preserved: %v", created.WebhookURL)
	}
	if created.Headers["X-A"] != "1" || created.WebhookHeaders["X-Hook"] != "y" {
		t.Errorf("explicit headers not preserved: %+v / %+v", created.Headers, created.WebhookHeaders)
	}
}

func TestCreateBuffer_ZeroMaxRetriesDefaultsToThree(t *testing.T) {
	t.Parallel()

	// MaxRetries is a *int: nil means "unset" (default 3), but an explicit 0
	// pointer must be honored as a real "no retries" choice.
	var created *domain.Buffer
	repo := &testutil.MockBufferRepository{
		CreateFn: func(_ context.Context, b *domain.Buffer) (*domain.Buffer, error) {
			created = b
			return b, nil
		},
	}
	uc := usecase.NewBufferUsecase(repo, &testutil.MockCreditRepository{})

	_, err := uc.CreateBuffer(context.Background(), usecase.CreateBufferInput{
		UserID:     "user-1",
		Name:       "no-retries",
		URL:        "https://example.com",
		Method:     "POST",
		MaxRetries: ptr(0),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created.MaxRetries != 0 {
		t.Errorf("explicit MaxRetries=0 should be preserved, got %d", created.MaxRetries)
	}
}

func TestCreateBuffer_RepoError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("db down")
	repo := &testutil.MockBufferRepository{
		CreateFn: func(_ context.Context, _ *domain.Buffer) (*domain.Buffer, error) {
			return nil, sentinel
		},
	}
	uc := usecase.NewBufferUsecase(repo, &testutil.MockCreditRepository{})

	_, err := uc.CreateBuffer(context.Background(), usecase.CreateBufferInput{UserID: "u", Name: "n", URL: "x", Method: "POST"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected wrapped sentinel, got %v", err)
	}
}

// ── GetBuffer ──────────────────────────────────────────────────────────────────

func TestGetBuffer_HappyPath(t *testing.T) {
	t.Parallel()

	repo := &testutil.MockBufferRepository{
		GetByIDFn: func(_ context.Context, id, userID string) (*domain.Buffer, error) {
			return &domain.Buffer{ID: id, UserID: userID, Name: "b"}, nil
		},
	}
	uc := usecase.NewBufferUsecase(repo, &testutil.MockCreditRepository{})

	got, err := uc.GetBuffer(context.Background(), "buffer-1", "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != "buffer-1" || got.UserID != "user-1" {
		t.Fatalf("unexpected buffer: %+v", got)
	}
}

func TestGetBuffer_NotFound(t *testing.T) {
	t.Parallel()

	uc := usecase.NewBufferUsecase(&testutil.MockBufferRepository{}, &testutil.MockCreditRepository{})
	_, err := uc.GetBuffer(context.Background(), "missing", "user-1")
	if !errors.Is(err, domain.ErrBufferNotFound) {
		t.Fatalf("expected ErrBufferNotFound, got %v", err)
	}
}

// ── ListBuffers ────────────────────────────────────────────────────────────────

func TestListBuffers_DefaultAndClampLimits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		inLimit   int
		wantQuery int // repo receives limit+1
	}{
		{"zero defaults to 20", 0, 21},
		{"negative defaults to 20", -5, 21},
		{"in-range preserved", 35, 36},
		{"over-max clamps to 100", 500, 101},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var gotLimit int
			repo := &testutil.MockBufferRepository{
				ListFn: func(_ context.Context, in repository.ListBuffersInput) ([]*domain.Buffer, error) {
					gotLimit = in.Limit
					return nil, nil
				},
			}
			uc := usecase.NewBufferUsecase(repo, &testutil.MockCreditRepository{})
			_, err := uc.ListBuffers(context.Background(), usecase.ListBuffersInput{UserID: "u", Limit: tc.inLimit})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotLimit != tc.wantQuery {
				t.Errorf("expected repo limit %d, got %d", tc.wantQuery, gotLimit)
			}
		})
	}
}

func TestListBuffers_PaginationSetsNextCursor(t *testing.T) {
	t.Parallel()

	// limit=2 → repo asked for 3; returning 3 means there's a next page.
	now := time.Now().UTC()
	repo := &testutil.MockBufferRepository{
		ListFn: func(_ context.Context, _ repository.ListBuffersInput) ([]*domain.Buffer, error) {
			return []*domain.Buffer{
				{ID: "b1", CreatedAt: now},
				{ID: "b2", CreatedAt: now.Add(-time.Minute)},
				{ID: "b3", CreatedAt: now.Add(-2 * time.Minute)},
			}, nil
		},
	}
	uc := usecase.NewBufferUsecase(repo, &testutil.MockCreditRepository{})

	res, err := uc.ListBuffers(context.Background(), usecase.ListBuffersInput{UserID: "u", Limit: 2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Buffers) != 2 {
		t.Fatalf("expected page trimmed to 2, got %d", len(res.Buffers))
	}
	if res.NextCursor == nil {
		t.Fatal("expected a next cursor when more rows exist")
	}
	if res.Buffers[1].ID != "b2" {
		t.Errorf("expected last in-page item b2, got %s", res.Buffers[1].ID)
	}
}

func TestListBuffers_NoNextCursorOnLastPage(t *testing.T) {
	t.Parallel()

	repo := &testutil.MockBufferRepository{
		ListFn: func(_ context.Context, _ repository.ListBuffersInput) ([]*domain.Buffer, error) {
			return []*domain.Buffer{{ID: "b1"}, {ID: "b2"}}, nil
		},
	}
	uc := usecase.NewBufferUsecase(repo, &testutil.MockCreditRepository{})

	res, err := uc.ListBuffers(context.Background(), usecase.ListBuffersInput{UserID: "u", Limit: 2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.NextCursor != nil {
		t.Errorf("expected no next cursor on last page, got %v", *res.NextCursor)
	}
	if len(res.Buffers) != 2 {
		t.Errorf("expected 2 buffers, got %d", len(res.Buffers))
	}
}

// TestListBuffers_CursorRoundTrip verifies the opaque cursor produced for one
// page decodes back into the repo query for the next page.
func TestListBuffers_CursorRoundTrip(t *testing.T) {
	t.Parallel()

	last := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	first := &testutil.MockBufferRepository{
		ListFn: func(_ context.Context, _ repository.ListBuffersInput) ([]*domain.Buffer, error) {
			return []*domain.Buffer{
				{ID: "b1", CreatedAt: last.Add(time.Hour)},
				{ID: "b2", CreatedAt: last},
				{ID: "b3", CreatedAt: last.Add(-time.Hour)},
			}, nil
		},
	}
	uc := usecase.NewBufferUsecase(first, &testutil.MockCreditRepository{})
	page1, err := uc.ListBuffers(context.Background(), usecase.ListBuffersInput{UserID: "u", Limit: 2})
	if err != nil {
		t.Fatalf("page1 error: %v", err)
	}
	if page1.NextCursor == nil {
		t.Fatal("expected a next cursor")
	}

	var gotTime *time.Time
	var gotID string
	second := &testutil.MockBufferRepository{
		ListFn: func(_ context.Context, in repository.ListBuffersInput) ([]*domain.Buffer, error) {
			gotTime = in.CursorTime
			gotID = in.CursorID
			return nil, nil
		},
	}
	uc2 := usecase.NewBufferUsecase(second, &testutil.MockCreditRepository{})
	_, err = uc2.ListBuffers(context.Background(), usecase.ListBuffersInput{UserID: "u", Limit: 2, Cursor: *page1.NextCursor})
	if err != nil {
		t.Fatalf("page2 error: %v", err)
	}
	// The cursor points at b2 (the last in-page item).
	if gotID != "b2" {
		t.Errorf("expected cursor ID b2, got %s", gotID)
	}
	if gotTime == nil || !gotTime.Equal(last) {
		t.Errorf("expected cursor time %v, got %v", last, gotTime)
	}
}

func TestListBuffers_BadCursor(t *testing.T) {
	t.Parallel()

	uc := usecase.NewBufferUsecase(&testutil.MockBufferRepository{}, &testutil.MockCreditRepository{})
	_, err := uc.ListBuffers(context.Background(), usecase.ListBuffersInput{UserID: "u", Cursor: "!!!not-base64!!!"})
	if err == nil {
		t.Fatal("expected error for malformed cursor")
	}
}

func TestListBuffers_RepoError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("boom")
	repo := &testutil.MockBufferRepository{
		ListFn: func(_ context.Context, _ repository.ListBuffersInput) ([]*domain.Buffer, error) {
			return nil, sentinel
		},
	}
	uc := usecase.NewBufferUsecase(repo, &testutil.MockCreditRepository{})
	_, err := uc.ListBuffers(context.Background(), usecase.ListBuffersInput{UserID: "u"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected wrapped sentinel, got %v", err)
	}
}

// ── Pause / Resume ─────────────────────────────────────────────────────────────

func TestPauseResumeBuffer(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		call     func(*usecase.BufferUsecase) error
		wantBool bool
	}{
		{"pause", func(u *usecase.BufferUsecase) error { return u.PauseBuffer(context.Background(), "b", "u") }, true},
		{"resume", func(u *usecase.BufferUsecase) error { return u.ResumeBuffer(context.Background(), "b", "u") }, false},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var gotPaused bool
			var called bool
			repo := &testutil.MockBufferRepository{
				SetPausedFn: func(_ context.Context, _, _ string, paused bool) error {
					called = true
					gotPaused = paused
					return nil
				},
			}
			uc := usecase.NewBufferUsecase(repo, &testutil.MockCreditRepository{})
			if err := tc.call(uc); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !called {
				t.Fatal("expected SetPaused to be called")
			}
			if gotPaused != tc.wantBool {
				t.Errorf("expected paused=%v, got %v", tc.wantBool, gotPaused)
			}
		})
	}
}

func TestPauseBuffer_RepoError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("nope")
	repo := &testutil.MockBufferRepository{
		SetPausedFn: func(_ context.Context, _, _ string, _ bool) error { return sentinel },
	}
	uc := usecase.NewBufferUsecase(repo, &testutil.MockCreditRepository{})
	if err := uc.PauseBuffer(context.Background(), "b", "u"); !errors.Is(err, sentinel) {
		t.Fatalf("expected wrapped sentinel, got %v", err)
	}
}

// GetBufferStats is covered in stats_test.go (ownership check + happy path).

// ── DeleteBuffer ───────────────────────────────────────────────────────────────

func TestDeleteBuffer(t *testing.T) {
	t.Parallel()

	called := false
	repo := &testutil.MockBufferRepository{
		DeleteFn: func(_ context.Context, _, _ string) error { called = true; return nil },
	}
	uc := usecase.NewBufferUsecase(repo, &testutil.MockCreditRepository{})
	if err := uc.DeleteBuffer(context.Background(), "b", "u"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("expected Delete to be called")
	}
}

func TestDeleteBuffer_RepoError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("fk violation")
	repo := &testutil.MockBufferRepository{
		DeleteFn: func(_ context.Context, _, _ string) error { return sentinel },
	}
	uc := usecase.NewBufferUsecase(repo, &testutil.MockCreditRepository{})
	if err := uc.DeleteBuffer(context.Background(), "b", "u"); !errors.Is(err, sentinel) {
		t.Fatalf("expected wrapped sentinel, got %v", err)
	}
}

// ── PushItem ───────────────────────────────────────────────────────────────────

func TestPushItem_NoCredits(t *testing.T) {
	t.Parallel()

	credits := &testutil.MockCreditRepository{
		HasCreditsFn: func(_ context.Context, _ string) (bool, error) { return false, nil },
	}
	// GetByID should never be reached.
	repo := &testutil.MockBufferRepository{
		GetByIDFn: func(_ context.Context, _, _ string) (*domain.Buffer, error) {
			t.Fatal("buffer lookup must not happen when credits are insufficient")
			return nil, nil
		},
	}
	uc := usecase.NewBufferUsecase(repo, credits)
	_, err := uc.PushItem(context.Background(), usecase.PushBufferItemInput{UserID: "u", BufferID: "b"})
	if !errors.Is(err, domain.ErrInsufficientCredits) {
		t.Fatalf("expected ErrInsufficientCredits, got %v", err)
	}
}

func TestPushItem_CreditCheckError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("credit svc down")
	credits := &testutil.MockCreditRepository{
		HasCreditsFn: func(_ context.Context, _ string) (bool, error) { return false, sentinel },
	}
	uc := usecase.NewBufferUsecase(&testutil.MockBufferRepository{}, credits)
	_, err := uc.PushItem(context.Background(), usecase.PushBufferItemInput{UserID: "u", BufferID: "b"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected wrapped sentinel, got %v", err)
	}
}

func TestPushItem_BufferNotFound(t *testing.T) {
	t.Parallel()

	uc := usecase.NewBufferUsecase(&testutil.MockBufferRepository{}, &testutil.MockCreditRepository{})
	_, err := uc.PushItem(context.Background(), usecase.PushBufferItemInput{UserID: "u", BufferID: "missing"})
	if !errors.Is(err, domain.ErrBufferNotFound) {
		t.Fatalf("expected ErrBufferNotFound, got %v", err)
	}
}

func TestPushItem_HeaderMergeAndDefaults(t *testing.T) {
	t.Parallel()

	var created *domain.BufferItem
	body := `{"hello":"world"}`
	repo := &testutil.MockBufferRepository{
		GetByIDFn: func(_ context.Context, id, userID string) (*domain.Buffer, error) {
			return &domain.Buffer{
				ID:             id,
				UserID:         userID,
				URL:            "https://target.example.com",
				Method:         "POST",
				Headers:        map[string]string{"X-Default": "d", "X-Override": "buffer"},
				TimeoutSeconds: 45,
				Backoff:        domain.BackoffLinear,
				MaxRetries:     5,
			}, nil
		},
		CreateItemFn: func(_ context.Context, item *domain.BufferItem) (*domain.BufferItem, error) {
			created = item
			item.ID = "item-1"
			return item, nil
		},
	}
	uc := usecase.NewBufferUsecase(repo, &testutil.MockCreditRepository{})

	before := time.Now()
	got, err := uc.PushItem(context.Background(), usecase.PushBufferItemInput{
		UserID:   "u",
		BufferID: "b",
		Body:     &body,
		Headers:  map[string]string{"X-Override": "item", "X-Item": "i"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != "item-1" {
		t.Fatalf("expected item-1, got %s", got.ID)
	}
	// Item overrides win; buffer defaults fill the rest.
	if created.Headers["X-Override"] != "item" {
		t.Errorf("expected item header to override buffer, got %q", created.Headers["X-Override"])
	}
	if created.Headers["X-Default"] != "d" {
		t.Errorf("expected buffer default header to survive, got %q", created.Headers["X-Default"])
	}
	if created.Headers["X-Item"] != "i" {
		t.Errorf("expected item-only header, got %q", created.Headers["X-Item"])
	}
	// Inherited from buffer.
	if created.URL != "https://target.example.com" || created.Method != "POST" {
		t.Errorf("URL/Method not inherited from buffer: %+v", created)
	}
	if created.TimeoutSeconds != 45 || created.Backoff != domain.BackoffLinear || created.MaxRetries != 5 {
		t.Errorf("execution params not inherited from buffer: %+v", created)
	}
	if created.Status != domain.BufferItemPending {
		t.Errorf("expected status pending, got %s", created.Status)
	}
	if created.ScheduledAt.Before(before) {
		t.Errorf("expected ScheduledAt ~now, got %v", created.ScheduledAt)
	}
	if created.Body == nil || *created.Body != body {
		t.Errorf("body not carried through: %v", created.Body)
	}
}

func TestPushItem_CreateError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("insert failed")
	repo := &testutil.MockBufferRepository{
		GetByIDFn: func(_ context.Context, id, userID string) (*domain.Buffer, error) {
			return &domain.Buffer{ID: id, UserID: userID, Headers: map[string]string{}}, nil
		},
		CreateItemFn: func(_ context.Context, _ *domain.BufferItem) (*domain.BufferItem, error) {
			return nil, sentinel
		},
	}
	uc := usecase.NewBufferUsecase(repo, &testutil.MockCreditRepository{})
	_, err := uc.PushItem(context.Background(), usecase.PushBufferItemInput{UserID: "u", BufferID: "b"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected wrapped sentinel, got %v", err)
	}
}

// ── GetItem ────────────────────────────────────────────────────────────────────

func TestGetItem_OwnershipChecked(t *testing.T) {
	t.Parallel()

	itemFetched := false
	repo := &testutil.MockBufferRepository{
		// default GetByID → ErrBufferNotFound
		GetItemByIDFn: func(_ context.Context, _, _ string) (*domain.BufferItem, error) {
			itemFetched = true
			return nil, nil
		},
	}
	uc := usecase.NewBufferUsecase(repo, &testutil.MockCreditRepository{})
	_, err := uc.GetItem(context.Background(), "item", "buffer", "user")
	if !errors.Is(err, domain.ErrBufferNotFound) {
		t.Fatalf("expected ErrBufferNotFound, got %v", err)
	}
	if itemFetched {
		t.Error("item must not be fetched when ownership check fails")
	}
}

func TestGetItem_HappyPath(t *testing.T) {
	t.Parallel()

	repo := &testutil.MockBufferRepository{
		GetByIDFn: func(_ context.Context, id, userID string) (*domain.Buffer, error) {
			return &domain.Buffer{ID: id, UserID: userID}, nil
		},
		GetItemByIDFn: func(_ context.Context, itemID, bufferID string) (*domain.BufferItem, error) {
			return &domain.BufferItem{ID: itemID, BufferID: bufferID, Status: domain.BufferItemCompleted}, nil
		},
	}
	uc := usecase.NewBufferUsecase(repo, &testutil.MockCreditRepository{})
	got, err := uc.GetItem(context.Background(), "item-9", "b", "u")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != "item-9" {
		t.Fatalf("expected item-9, got %s", got.ID)
	}
}

func TestGetItem_ItemNotFound(t *testing.T) {
	t.Parallel()

	repo := &testutil.MockBufferRepository{
		GetByIDFn: func(_ context.Context, id, userID string) (*domain.Buffer, error) {
			return &domain.Buffer{ID: id, UserID: userID}, nil
		},
		// default GetItemByID → ErrBufferItemNotFound
	}
	uc := usecase.NewBufferUsecase(repo, &testutil.MockCreditRepository{})
	_, err := uc.GetItem(context.Background(), "missing", "b", "u")
	if !errors.Is(err, domain.ErrBufferItemNotFound) {
		t.Fatalf("expected ErrBufferItemNotFound, got %v", err)
	}
}

// ── ReplayItem ─────────────────────────────────────────────────────────────────

func TestReplayItem_BufferNotFound(t *testing.T) {
	t.Parallel()

	uc := usecase.NewBufferUsecase(&testutil.MockBufferRepository{}, &testutil.MockCreditRepository{})
	_, err := uc.ReplayItem(context.Background(), "item", "buffer", "user")
	if !errors.Is(err, domain.ErrBufferNotFound) {
		t.Fatalf("expected ErrBufferNotFound, got %v", err)
	}
}

func TestReplayItem_NotReplayableStatus(t *testing.T) {
	t.Parallel()

	for _, status := range []domain.BufferItemStatus{
		domain.BufferItemPending, domain.BufferItemRunning, domain.BufferItemCompleted,
	} {
		status := status
		t.Run(string(status), func(t *testing.T) {
			t.Parallel()
			repo := &testutil.MockBufferRepository{
				GetByIDFn: func(_ context.Context, id, userID string) (*domain.Buffer, error) {
					return &domain.Buffer{ID: id, UserID: userID}, nil
				},
				GetItemByIDFn: func(_ context.Context, itemID, bufferID string) (*domain.BufferItem, error) {
					return &domain.BufferItem{ID: itemID, BufferID: bufferID, Status: status}, nil
				},
			}
			uc := usecase.NewBufferUsecase(repo, &testutil.MockCreditRepository{})
			_, err := uc.ReplayItem(context.Background(), "item", "b", "u")
			if !errors.Is(err, domain.ErrBufferItemNotReplayable) {
				t.Fatalf("status %s: expected ErrBufferItemNotReplayable, got %v", status, err)
			}
		})
	}
}

func TestReplayItem_NoCredits(t *testing.T) {
	t.Parallel()

	repo := &testutil.MockBufferRepository{
		GetByIDFn: func(_ context.Context, id, userID string) (*domain.Buffer, error) {
			return &domain.Buffer{ID: id, UserID: userID}, nil
		},
		GetItemByIDFn: func(_ context.Context, itemID, bufferID string) (*domain.BufferItem, error) {
			return &domain.BufferItem{ID: itemID, BufferID: bufferID, Status: domain.BufferItemFailed}, nil
		},
		CreateItemFn: func(_ context.Context, _ *domain.BufferItem) (*domain.BufferItem, error) {
			t.Fatal("must not create a replay item when credits are insufficient")
			return nil, nil
		},
	}
	credits := &testutil.MockCreditRepository{
		HasCreditsFn: func(_ context.Context, _ string) (bool, error) { return false, nil },
	}
	uc := usecase.NewBufferUsecase(repo, credits)
	_, err := uc.ReplayItem(context.Background(), "item", "b", "u")
	if !errors.Is(err, domain.ErrInsufficientCredits) {
		t.Fatalf("expected ErrInsufficientCredits, got %v", err)
	}
}

func TestReplayItem_HappyPathClonesWithReplayOf(t *testing.T) {
	t.Parallel()

	body := "payload"
	orig := &domain.BufferItem{
		ID:             "orig-1",
		BufferID:       "b",
		UserID:         "u",
		URL:            "https://target",
		Method:         "POST",
		Headers:        map[string]string{"X": "y"},
		Body:           &body,
		TimeoutSeconds: 60,
		Backoff:        domain.BackoffLinear,
		MaxRetries:     4,
		Status:         domain.BufferItemFailed,
	}
	var clone *domain.BufferItem
	repo := &testutil.MockBufferRepository{
		GetByIDFn: func(_ context.Context, id, userID string) (*domain.Buffer, error) {
			return &domain.Buffer{ID: id, UserID: userID}, nil
		},
		GetItemByIDFn: func(_ context.Context, _, _ string) (*domain.BufferItem, error) {
			return orig, nil
		},
		CreateItemFn: func(_ context.Context, item *domain.BufferItem) (*domain.BufferItem, error) {
			clone = item
			item.ID = "clone-1"
			return item, nil
		},
	}
	uc := usecase.NewBufferUsecase(repo, &testutil.MockCreditRepository{})

	got, err := uc.ReplayItem(context.Background(), "orig-1", "b", "u")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != "clone-1" {
		t.Fatalf("expected clone-1, got %s", got.ID)
	}
	if clone.ReplayOf == nil || *clone.ReplayOf != "orig-1" {
		t.Errorf("expected ReplayOf=orig-1, got %v", clone.ReplayOf)
	}
	if clone.Status != domain.BufferItemPending {
		t.Errorf("expected clone status pending, got %s", clone.Status)
	}
	if clone.URL != orig.URL || clone.Method != orig.Method || clone.MaxRetries != orig.MaxRetries {
		t.Errorf("clone did not copy execution params: %+v", clone)
	}
	if clone.Body == nil || *clone.Body != body {
		t.Errorf("clone did not copy body: %v", clone.Body)
	}
	if clone.RetryCount != 0 {
		t.Errorf("expected fresh RetryCount 0, got %d", clone.RetryCount)
	}
}

func TestReplayItem_CreateError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("insert failed")
	repo := &testutil.MockBufferRepository{
		GetByIDFn: func(_ context.Context, id, userID string) (*domain.Buffer, error) {
			return &domain.Buffer{ID: id, UserID: userID}, nil
		},
		GetItemByIDFn: func(_ context.Context, itemID, bufferID string) (*domain.BufferItem, error) {
			return &domain.BufferItem{ID: itemID, BufferID: bufferID, Status: domain.BufferItemFailed}, nil
		},
		CreateItemFn: func(_ context.Context, _ *domain.BufferItem) (*domain.BufferItem, error) {
			return nil, sentinel
		},
	}
	uc := usecase.NewBufferUsecase(repo, &testutil.MockCreditRepository{})
	_, err := uc.ReplayItem(context.Background(), "item", "b", "u")
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected wrapped sentinel, got %v", err)
	}
}

// ── ListItems ──────────────────────────────────────────────────────────────────

func TestListItems_OwnershipChecked(t *testing.T) {
	t.Parallel()

	listed := false
	repo := &testutil.MockBufferRepository{
		ListItemsFn: func(_ context.Context, _ repository.ListBufferItemsInput) ([]*domain.BufferItem, error) {
			listed = true
			return nil, nil
		},
	}
	uc := usecase.NewBufferUsecase(repo, &testutil.MockCreditRepository{})
	_, err := uc.ListItems(context.Background(), usecase.ListBufferItemsInput{BufferID: "b", UserID: "u"})
	if !errors.Is(err, domain.ErrBufferNotFound) {
		t.Fatalf("expected ErrBufferNotFound, got %v", err)
	}
	if listed {
		t.Error("items must not be listed when ownership check fails")
	}
}

func TestListItems_InvalidStatus(t *testing.T) {
	t.Parallel()

	repo := &testutil.MockBufferRepository{
		GetByIDFn: func(_ context.Context, id, userID string) (*domain.Buffer, error) {
			return &domain.Buffer{ID: id, UserID: userID}, nil
		},
	}
	uc := usecase.NewBufferUsecase(repo, &testutil.MockCreditRepository{})
	_, err := uc.ListItems(context.Background(), usecase.ListBufferItemsInput{BufferID: "b", UserID: "u", Status: "bogus"})
	if !errors.Is(err, domain.ErrInvalidStatus) {
		t.Fatalf("expected ErrInvalidStatus, got %v", err)
	}
}

func TestListItems_ValidStatusPassedToRepo(t *testing.T) {
	t.Parallel()

	var gotStatus domain.BufferItemStatus
	repo := &testutil.MockBufferRepository{
		GetByIDFn: func(_ context.Context, id, userID string) (*domain.Buffer, error) {
			return &domain.Buffer{ID: id, UserID: userID}, nil
		},
		ListItemsFn: func(_ context.Context, in repository.ListBufferItemsInput) ([]*domain.BufferItem, error) {
			gotStatus = in.Status
			return nil, nil
		},
	}
	uc := usecase.NewBufferUsecase(repo, &testutil.MockCreditRepository{})
	_, err := uc.ListItems(context.Background(), usecase.ListBufferItemsInput{BufferID: "b", UserID: "u", Status: "failed"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotStatus != domain.BufferItemFailed {
		t.Errorf("expected status failed passed to repo, got %q", gotStatus)
	}
}

func TestListItems_DefaultAndClampLimits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		inLimit   int
		wantQuery int
	}{
		{"zero defaults to 20", 0, 21},
		{"over-max clamps to 100", 1000, 101},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var gotLimit int
			repo := &testutil.MockBufferRepository{
				GetByIDFn: func(_ context.Context, id, userID string) (*domain.Buffer, error) {
					return &domain.Buffer{ID: id, UserID: userID}, nil
				},
				ListItemsFn: func(_ context.Context, in repository.ListBufferItemsInput) ([]*domain.BufferItem, error) {
					gotLimit = in.Limit
					return nil, nil
				},
			}
			uc := usecase.NewBufferUsecase(repo, &testutil.MockCreditRepository{})
			_, err := uc.ListItems(context.Background(), usecase.ListBufferItemsInput{BufferID: "b", UserID: "u", Limit: tc.inLimit})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotLimit != tc.wantQuery {
				t.Errorf("expected repo limit %d, got %d", tc.wantQuery, gotLimit)
			}
		})
	}
}

func TestListItems_PaginationSetsNextCursor(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	repo := &testutil.MockBufferRepository{
		GetByIDFn: func(_ context.Context, id, userID string) (*domain.Buffer, error) {
			return &domain.Buffer{ID: id, UserID: userID}, nil
		},
		ListItemsFn: func(_ context.Context, _ repository.ListBufferItemsInput) ([]*domain.BufferItem, error) {
			return []*domain.BufferItem{
				{ID: "i1", CreatedAt: now},
				{ID: "i2", CreatedAt: now.Add(-time.Minute)},
				{ID: "i3", CreatedAt: now.Add(-2 * time.Minute)},
			}, nil
		},
	}
	uc := usecase.NewBufferUsecase(repo, &testutil.MockCreditRepository{})
	res, err := uc.ListItems(context.Background(), usecase.ListBufferItemsInput{BufferID: "b", UserID: "u", Limit: 2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Items) != 2 {
		t.Fatalf("expected page trimmed to 2, got %d", len(res.Items))
	}
	if res.NextCursor == nil {
		t.Fatal("expected a next cursor when more rows exist")
	}
}

func TestListItems_BadCursor(t *testing.T) {
	t.Parallel()

	repo := &testutil.MockBufferRepository{
		GetByIDFn: func(_ context.Context, id, userID string) (*domain.Buffer, error) {
			return &domain.Buffer{ID: id, UserID: userID}, nil
		},
	}
	uc := usecase.NewBufferUsecase(repo, &testutil.MockCreditRepository{})
	_, err := uc.ListItems(context.Background(), usecase.ListBufferItemsInput{BufferID: "b", UserID: "u", Cursor: "@@@bad@@@"})
	if err == nil {
		t.Fatal("expected error for malformed cursor")
	}
}

func TestListItems_RepoError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("query failed")
	repo := &testutil.MockBufferRepository{
		GetByIDFn: func(_ context.Context, id, userID string) (*domain.Buffer, error) {
			return &domain.Buffer{ID: id, UserID: userID}, nil
		},
		ListItemsFn: func(_ context.Context, _ repository.ListBufferItemsInput) ([]*domain.BufferItem, error) {
			return nil, sentinel
		},
	}
	uc := usecase.NewBufferUsecase(repo, &testutil.MockCreditRepository{})
	_, err := uc.ListItems(context.Background(), usecase.ListBufferItemsInput{BufferID: "b", UserID: "u"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected wrapped sentinel, got %v", err)
	}
}
