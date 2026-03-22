//go:build integration

package postgres_test

import (
	"context"
	"testing"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/infrastructure/postgres"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/testutil"
)

func TestUserRepo_UpsertAndFindByID(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	testutil.TruncateAll(t, pool)
	repo := postgres.NewUserRepository(pool)
	ctx := context.Background()

	userID := testutil.UserID()

	// First upsert creates
	if err := repo.Upsert(ctx, userID); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// Second upsert is idempotent
	if err := repo.Upsert(ctx, userID); err != nil {
		t.Fatalf("upsert idempotent: %v", err)
	}

	user, err := repo.FindByID(ctx, userID)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if user.ID != userID {
		t.Fatalf("id = %s, want %s", user.ID, userID)
	}
}

func TestUserRepo_FindByID_NotFound(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	testutil.TruncateAll(t, pool)
	repo := postgres.NewUserRepository(pool)

	_, err := repo.FindByID(context.Background(), "nonexistent")
	if err != domain.ErrUserNotFound {
		t.Fatalf("err = %v, want ErrUserNotFound", err)
	}
}
