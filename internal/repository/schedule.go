package repository

import (
	"context"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
)

type ListSchedulesInput struct {
	UserID     string
	CursorTime *time.Time // cursor on (created_at DESC, id DESC)
	CursorID   string
	Limit      int
}

type ScheduleRepository interface {
	Create(ctx context.Context, s *domain.Schedule) (*domain.Schedule, error)
	GetByID(ctx context.Context, id, userID string) (*domain.Schedule, error)
	List(ctx context.Context, input ListSchedulesInput) ([]*domain.Schedule, error)
	SetPaused(ctx context.Context, id, userID string, paused bool) error
	Delete(ctx context.Context, id, userID string) error
	// Atomic: claim due schedules, create jobs, advance next_run_at — all in one tx.
	// creditFilter is called per schedule before inserting the job; returning false
	// skips the job but still advances next_run_at.
	ClaimAndFire(ctx context.Context, limit int, computeNext func(*domain.Schedule) time.Time, creditFilter func(ctx context.Context, userID string) bool) ([]*domain.Job, error)
}
