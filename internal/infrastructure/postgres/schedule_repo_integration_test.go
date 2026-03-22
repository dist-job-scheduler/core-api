//go:build integration

package postgres_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/infrastructure/postgres"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/testutil"
)

func setupScheduleRepo(t *testing.T) (*postgres.ScheduleRepository, string) {
	t.Helper()
	pool := testutil.SetupTestDB(t)
	testutil.TruncateAll(t, pool)
	userID := testutil.UserID()
	testutil.SeedUser(t, pool, userID)
	return postgres.NewScheduleRepository(pool, slog.Default()), userID
}

func TestScheduleRepo_CreateAndGetByID(t *testing.T) {
	repo, userID := setupScheduleRepo(t)
	ctx := context.Background()

	s := testutil.NewSchedule(testutil.WithScheduleUserID(userID))
	created, err := repo.Create(ctx, s)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected non-empty ID")
	}

	got, err := repo.GetByID(ctx, created.ID, userID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != created.Name {
		t.Fatalf("name = %s, want %s", got.Name, created.Name)
	}
	if got.CronExpr != created.CronExpr {
		t.Fatalf("cron = %s, want %s", got.CronExpr, created.CronExpr)
	}
}

func TestScheduleRepo_CreateNameConflict(t *testing.T) {
	repo, userID := setupScheduleRepo(t)
	ctx := context.Background()

	s := testutil.NewSchedule(testutil.WithScheduleUserID(userID), testutil.WithScheduleName("unique-name"))
	if _, err := repo.Create(ctx, s); err != nil {
		t.Fatalf("first create: %v", err)
	}

	s2 := testutil.NewSchedule(testutil.WithScheduleUserID(userID), testutil.WithScheduleName("unique-name"))
	_, err := repo.Create(ctx, s2)
	if err != domain.ErrScheduleNameConflict {
		t.Fatalf("err = %v, want ErrScheduleNameConflict", err)
	}
}

func TestScheduleRepo_GetByID_WrongUser(t *testing.T) {
	repo, userID := setupScheduleRepo(t)
	ctx := context.Background()

	s := testutil.NewSchedule(testutil.WithScheduleUserID(userID))
	created, err := repo.Create(ctx, s)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err = repo.GetByID(ctx, created.ID, "other-user")
	if err != domain.ErrScheduleNotFound {
		t.Fatalf("err = %v, want ErrScheduleNotFound", err)
	}
}

func TestScheduleRepo_SetPaused(t *testing.T) {
	repo, userID := setupScheduleRepo(t)
	ctx := context.Background()

	s := testutil.NewSchedule(testutil.WithScheduleUserID(userID))
	created, err := repo.Create(ctx, s)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Pause
	if err := repo.SetPaused(ctx, created.ID, userID, true); err != nil {
		t.Fatalf("pause: %v", err)
	}
	got, _ := repo.GetByID(ctx, created.ID, userID)
	if !got.Paused {
		t.Fatal("expected paused=true")
	}

	// Pause again → already paused
	err = repo.SetPaused(ctx, created.ID, userID, true)
	if err != domain.ErrScheduleAlreadyPaused {
		t.Fatalf("err = %v, want ErrScheduleAlreadyPaused", err)
	}

	// Resume
	if err := repo.SetPaused(ctx, created.ID, userID, false); err != nil {
		t.Fatalf("resume: %v", err)
	}

	// Resume again → not paused
	err = repo.SetPaused(ctx, created.ID, userID, false)
	if err != domain.ErrScheduleNotPaused {
		t.Fatalf("err = %v, want ErrScheduleNotPaused", err)
	}
}

func TestScheduleRepo_Delete(t *testing.T) {
	repo, userID := setupScheduleRepo(t)
	ctx := context.Background()

	s := testutil.NewSchedule(testutil.WithScheduleUserID(userID))
	created, err := repo.Create(ctx, s)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := repo.Delete(ctx, created.ID, userID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	_, err = repo.GetByID(ctx, created.ID, userID)
	if err != domain.ErrScheduleNotFound {
		t.Fatalf("err = %v, want ErrScheduleNotFound", err)
	}
}

func TestScheduleRepo_Delete_NotFound(t *testing.T) {
	repo, userID := setupScheduleRepo(t)
	err := repo.Delete(context.Background(), "nonexistent", userID)
	if err != domain.ErrScheduleNotFound {
		t.Fatalf("err = %v, want ErrScheduleNotFound", err)
	}
}

func TestScheduleRepo_ClaimAndFire(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	testutil.TruncateAll(t, pool)
	userID := testutil.UserID()
	testutil.SeedUser(t, pool, userID)

	repo := postgres.NewScheduleRepository(pool, slog.Default())
	ctx := context.Background()

	// Create schedule due now
	s := testutil.NewSchedule(testutil.WithScheduleUserID(userID))
	s.NextRunAt = time.Now().Add(-time.Minute) // due
	created, err := repo.Create(ctx, s)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	computeNext := func(sched *domain.Schedule) time.Time {
		return time.Now().Add(5 * time.Minute)
	}
	creditFilter := func(_ context.Context, _ string) bool {
		return true
	}

	jobs, err := repo.ClaimAndFire(ctx, 100, computeNext, creditFilter)
	if err != nil {
		t.Fatalf("claim and fire: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("fired %d jobs, want 1", len(jobs))
	}
	if jobs[0].URL != s.URL {
		t.Fatalf("job url = %s, want %s", jobs[0].URL, s.URL)
	}
	if jobs[0].ScheduleID == nil || *jobs[0].ScheduleID != created.ID {
		t.Fatal("expected schedule_id to be set")
	}

	// Verify next_run_at was advanced
	updated, err := repo.GetByID(ctx, created.ID, userID)
	if err != nil {
		t.Fatalf("get updated: %v", err)
	}
	if updated.NextRunAt.Before(time.Now()) {
		t.Fatal("expected next_run_at to be in the future")
	}
	if updated.LastRunAt == nil {
		t.Fatal("expected last_run_at to be set")
	}
}

func TestScheduleRepo_ClaimAndFire_CreditFilterSkip(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	testutil.TruncateAll(t, pool)
	userID := testutil.UserID()
	testutil.SeedUser(t, pool, userID)

	repo := postgres.NewScheduleRepository(pool, slog.Default())
	ctx := context.Background()

	s := testutil.NewSchedule(testutil.WithScheduleUserID(userID))
	s.NextRunAt = time.Now().Add(-time.Minute)
	created, err := repo.Create(ctx, s)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	computeNext := func(_ *domain.Schedule) time.Time {
		return time.Now().Add(5 * time.Minute)
	}
	// Credit filter rejects
	creditFilter := func(_ context.Context, _ string) bool {
		return false
	}

	jobs, err := repo.ClaimAndFire(ctx, 100, computeNext, creditFilter)
	if err != nil {
		t.Fatalf("claim and fire: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("fired %d jobs, want 0 (credits rejected)", len(jobs))
	}

	// But next_run_at should still be advanced
	updated, err := repo.GetByID(ctx, created.ID, userID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if updated.NextRunAt.Before(time.Now()) {
		t.Fatal("expected next_run_at to be advanced even when credits rejected")
	}
}
