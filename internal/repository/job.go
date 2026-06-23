package repository

import (
	"context"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
)

type ListJobsInput struct {
	UserID     string
	Status     domain.Status // empty = all statuses
	CursorTime *time.Time    // nil = first page
	CursorID   string        // used only when CursorTime is non-nil
	Limit      int

	// Optional search/filter predicates (validated in the usecase layer).
	Method          string     // exact HTTP method; empty = all
	URLSearch       string     // case-insensitive substring match on url; empty = no filter
	ErrorSearch     string     // case-insensitive substring match on last_error; empty = no filter
	ScheduledAfter  *time.Time // scheduled_at >= this; nil = no lower bound
	ScheduledBefore *time.Time // scheduled_at <= this; nil = no upper bound
}

// UseCase depends on interface, not concrete implementation.
// This way we get: 1) can swap DB later without touching usecase 2) We can pass a mock implementation of interface in tests
type JobRepository interface {
	Create(ctx context.Context, job *domain.Job) (*domain.Job, error)
	GetByID(ctx context.Context, jobID, userID string) (*domain.Job, error)
	ListJobs(ctx context.Context, input ListJobsInput) ([]*domain.Job, error)
	Cancel(ctx context.Context, jobID, userID string) error

	// what does the scheduler worker need? Worker to poll, then claim and process the batch
	// Reaper process to find all failed jobs and re-schedule them for another attempt if a retry is possible
	Claim(ctx context.Context, workerID string, limit int) ([]*domain.Job, error)
	UpdateHeartbeat(ctx context.Context, jobID string) error
	Complete(ctx context.Context, jobID string) error
	Fail(ctx context.Context, jobID string, lastError string) error
	Reschedule(ctx context.Context, jobID string, lastError string, retryAt time.Time) error

	// Reaper methods — recover jobs from crashed workers
	RescheduleStale(ctx context.Context, staleCutoff time.Time, limit int) (int, error)
	FailStale(ctx context.Context, staleCutoff time.Time, limit int) (int, error)

	ListByScheduleID(ctx context.Context, scheduleID string, limit int, cursorTime *time.Time, cursorID string) ([]*domain.Job, error)
}
