//go:build integration

package postgres_test

import (
	"context"
	"testing"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/infrastructure/postgres"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/testutil"
)

func newTestBuffer(t *testing.T, repo *postgres.BufferRepository, userID string) *domain.Buffer {
	t.Helper()
	b, err := repo.Create(context.Background(), &domain.Buffer{
		UserID:         userID,
		Name:           "test-buffer",
		URL:            "https://example.com/api",
		Method:         "POST",
		Headers:        map[string]string{},
		WebhookHeaders: map[string]string{},
		TimeoutSeconds: 30,
		RateLimit:      10,
		MaxRetries:     3,
		Backoff:        domain.BackoffExponential,
	})
	if err != nil {
		t.Fatalf("create buffer: %v", err)
	}
	return b
}

func newTestItem(t *testing.T, repo *postgres.BufferRepository, b *domain.Buffer, status domain.BufferItemStatus) *domain.BufferItem {
	t.Helper()
	item, err := repo.CreateItem(context.Background(), &domain.BufferItem{
		BufferID:       b.ID,
		UserID:         b.UserID,
		URL:            b.URL,
		Method:         b.Method,
		Headers:        map[string]string{},
		TimeoutSeconds: 30,
		Backoff:        domain.BackoffExponential,
		Status:         status,
		MaxRetries:     3,
	})
	if err != nil {
		t.Fatalf("create item: %v", err)
	}
	return item
}

func TestBufferRepo_BufferStats(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	testutil.TruncateAll(t, pool)
	userID := testutil.UserID()
	testutil.SeedUser(t, pool, userID)
	repo := postgres.NewBufferRepository(pool)

	b := newTestBuffer(t, repo, userID)
	newTestItem(t, repo, b, domain.BufferItemPending)
	newTestItem(t, repo, b, domain.BufferItemPending)
	newTestItem(t, repo, b, domain.BufferItemRunning)
	newTestItem(t, repo, b, domain.BufferItemCompleted)
	newTestItem(t, repo, b, domain.BufferItemCompleted)
	newTestItem(t, repo, b, domain.BufferItemCompleted)
	newTestItem(t, repo, b, domain.BufferItemFailed)

	s, err := repo.BufferStats(context.Background(), b.ID)
	if err != nil {
		t.Fatalf("buffer stats: %v", err)
	}
	if s.Pending != 2 || s.Running != 1 || s.Completed != 3 || s.Failed != 1 || s.Total != 7 {
		t.Errorf("stats = %+v, want pending2/running1/completed3/failed1/total7", s)
	}
	// 3 completed of 4 terminal = 0.75.
	if s.SuccessRate < 0.74 || s.SuccessRate > 0.76 {
		t.Errorf("success_rate = %f, want ~0.75", s.SuccessRate)
	}
}

func TestBufferRepo_BufferStats_Isolation(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	testutil.TruncateAll(t, pool)
	userID := testutil.UserID()
	testutil.SeedUser(t, pool, userID)
	repo := postgres.NewBufferRepository(pool)

	b1 := newTestBuffer(t, repo, userID)
	b2, err := repo.Create(context.Background(), &domain.Buffer{
		UserID: userID, Name: "other", URL: "https://example.com/x", Method: "POST",
		Headers: map[string]string{}, WebhookHeaders: map[string]string{},
		TimeoutSeconds: 30, RateLimit: 10, MaxRetries: 3, Backoff: domain.BackoffExponential,
	})
	if err != nil {
		t.Fatalf("create buffer 2: %v", err)
	}

	newTestItem(t, repo, b1, domain.BufferItemFailed)
	newTestItem(t, repo, b2, domain.BufferItemCompleted)

	s, err := repo.BufferStats(context.Background(), b1.ID)
	if err != nil {
		t.Fatalf("buffer stats: %v", err)
	}
	if s.Total != 1 || s.Failed != 1 {
		t.Errorf("stats = %+v, want only buffer 1's single failed item", s)
	}
}

func TestBufferRepo_ReplayOfPersists(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	testutil.TruncateAll(t, pool)
	userID := testutil.UserID()
	testutil.SeedUser(t, pool, userID)
	repo := postgres.NewBufferRepository(pool)
	ctx := context.Background()

	b := newTestBuffer(t, repo, userID)
	original := newTestItem(t, repo, b, domain.BufferItemFailed)

	clone, err := repo.CreateItem(ctx, &domain.BufferItem{
		BufferID: b.ID, UserID: userID, URL: b.URL, Method: b.Method,
		Headers: map[string]string{}, TimeoutSeconds: 30, Backoff: domain.BackoffExponential,
		Status: domain.BufferItemPending, MaxRetries: 3, ReplayOf: &original.ID,
	})
	if err != nil {
		t.Fatalf("create replay item: %v", err)
	}
	if clone.ReplayOf == nil || *clone.ReplayOf != original.ID {
		t.Fatalf("clone.ReplayOf = %v, want %s", clone.ReplayOf, original.ID)
	}

	// Round-trips through a read.
	got, err := repo.GetItemByID(ctx, clone.ID, b.ID)
	if err != nil {
		t.Fatalf("get item: %v", err)
	}
	if got.ReplayOf == nil || *got.ReplayOf != original.ID {
		t.Errorf("read-back ReplayOf = %v, want %s", got.ReplayOf, original.ID)
	}
}
