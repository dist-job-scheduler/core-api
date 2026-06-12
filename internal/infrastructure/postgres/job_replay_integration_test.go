//go:build integration

package postgres_test

import (
	"context"
	"testing"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/infrastructure/postgres"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/testutil"
)

func TestJobRepo_ReplayOfPersists(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	testutil.TruncateAll(t, pool)
	userID := testutil.UserID()
	testutil.SeedUser(t, pool, userID)
	repo := postgres.NewJobRepository(pool)
	ctx := context.Background()

	original, err := repo.Create(ctx, testutil.NewJob(testutil.WithUserID(userID)))
	if err != nil {
		t.Fatalf("create original: %v", err)
	}
	if original.ReplayOf != nil {
		t.Errorf("ordinary job ReplayOf = %v, want nil", original.ReplayOf)
	}

	clone := testutil.NewJob(testutil.WithUserID(userID))
	clone.ReplayOf = &original.ID
	created, err := repo.Create(ctx, clone)
	if err != nil {
		t.Fatalf("create clone: %v", err)
	}
	if created.ReplayOf == nil || *created.ReplayOf != original.ID {
		t.Fatalf("clone.ReplayOf = %v, want %s", created.ReplayOf, original.ID)
	}

	// Round-trips through GetByID (which shares scanJob with every other read).
	got, err := repo.GetByID(ctx, created.ID, userID)
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if got.ReplayOf == nil || *got.ReplayOf != original.ID {
		t.Errorf("read-back ReplayOf = %v, want %s", got.ReplayOf, original.ID)
	}
}

func TestJobRepo_ReplayOfRejectsUnknownParent(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	testutil.TruncateAll(t, pool)
	userID := testutil.UserID()
	testutil.SeedUser(t, pool, userID)
	repo := postgres.NewJobRepository(pool)
	ctx := context.Background()

	// replay_of has an FK to jobs(id); a dangling reference must be rejected.
	bogus := "nonexistent-job-id"
	clone := testutil.NewJob(testutil.WithUserID(userID))
	clone.ReplayOf = &bogus
	if _, err := repo.Create(ctx, clone); err == nil {
		t.Fatal("expected FK violation for unknown replay_of parent, got nil")
	}
}
