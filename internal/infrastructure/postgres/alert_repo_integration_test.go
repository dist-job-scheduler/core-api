//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/infrastructure/postgres"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/testutil"
)

func TestAlertRepo_CreateGetList(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	testutil.TruncateAll(t, pool)
	userID := testutil.UserID()
	testutil.SeedUser(t, pool, userID)
	repo := postgres.NewAlertChannelRepository(pool)
	ctx := context.Background()

	created, err := repo.Create(ctx, &domain.AlertChannel{
		UserID:  userID,
		Type:    domain.AlertChannelSlack,
		Target:  "https://hooks.slack.com/services/x",
		Name:    "ops",
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ID == "" {
		t.Fatal("created channel has empty ID")
	}

	got, err := repo.GetByID(ctx, created.ID, userID)
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if got.Target != "https://hooks.slack.com/services/x" || got.Type != domain.AlertChannelSlack {
		t.Errorf("got %+v, want slack target", got)
	}

	list, err := repo.List(ctx, userID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("list len = %d, want 1", len(list))
	}
}

func TestAlertRepo_ListEnabledFiltersDisabled(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	testutil.TruncateAll(t, pool)
	userID := testutil.UserID()
	testutil.SeedUser(t, pool, userID)
	repo := postgres.NewAlertChannelRepository(pool)
	ctx := context.Background()

	on, _ := repo.Create(ctx, &domain.AlertChannel{UserID: userID, Type: domain.AlertChannelWebhook, Target: "https://a.example.com", Enabled: true})
	off, _ := repo.Create(ctx, &domain.AlertChannel{UserID: userID, Type: domain.AlertChannelWebhook, Target: "https://b.example.com", Enabled: true})

	if err := repo.SetEnabled(ctx, off.ID, userID, false); err != nil {
		t.Fatalf("set enabled false: %v", err)
	}

	enabled, err := repo.ListEnabled(ctx, userID)
	if err != nil {
		t.Fatalf("list enabled: %v", err)
	}
	if len(enabled) != 1 || enabled[0].ID != on.ID {
		t.Fatalf("ListEnabled returned %d channels, want only the enabled one (%s)", len(enabled), on.ID)
	}
}

func TestAlertRepo_OwnershipIsolation(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	testutil.TruncateAll(t, pool)
	owner := testutil.UserID()
	other := testutil.UserID()
	testutil.SeedUser(t, pool, owner)
	testutil.SeedUser(t, pool, other)
	repo := postgres.NewAlertChannelRepository(pool)
	ctx := context.Background()

	created, _ := repo.Create(ctx, &domain.AlertChannel{UserID: owner, Type: domain.AlertChannelSlack, Target: "https://x.example.com", Enabled: true})

	// Another user must not see or fetch it.
	if _, err := repo.GetByID(ctx, created.ID, other); !errors.Is(err, domain.ErrAlertChannelNotFound) {
		t.Errorf("cross-user GetByID err = %v, want ErrAlertChannelNotFound", err)
	}
	if err := repo.Delete(ctx, created.ID, other); !errors.Is(err, domain.ErrAlertChannelNotFound) {
		t.Errorf("cross-user Delete err = %v, want ErrAlertChannelNotFound", err)
	}
}

func TestAlertRepo_DeleteAndNotFound(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	testutil.TruncateAll(t, pool)
	userID := testutil.UserID()
	testutil.SeedUser(t, pool, userID)
	repo := postgres.NewAlertChannelRepository(pool)
	ctx := context.Background()

	created, _ := repo.Create(ctx, &domain.AlertChannel{UserID: userID, Type: domain.AlertChannelSlack, Target: "https://x.example.com", Enabled: true})

	if err := repo.Delete(ctx, created.ID, userID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.GetByID(ctx, created.ID, userID); !errors.Is(err, domain.ErrAlertChannelNotFound) {
		t.Errorf("after delete GetByID err = %v, want ErrAlertChannelNotFound", err)
	}
	if err := repo.SetEnabled(ctx, created.ID, userID, true); !errors.Is(err, domain.ErrAlertChannelNotFound) {
		t.Errorf("SetEnabled on missing err = %v, want ErrAlertChannelNotFound", err)
	}
}
