package testutil

import (
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/google/uuid"
)

// JobOption customizes a Job created by NewJob.
type JobOption func(*domain.Job)

// NewJob creates a domain.Job with sensible defaults. Use options to override.
func NewJob(opts ...JobOption) *domain.Job {
	now := time.Now().UTC()
	j := &domain.Job{
		ID:             uuid.New().String(),
		UserID:         "user-test-" + uuid.New().String()[:8],
		IdempotencyKey: uuid.New().String(),
		URL:            "https://httpbin.org/post",
		Method:         "POST",
		Headers:        map[string]string{"Content-Type": "application/json"},
		TimeoutSeconds: 30,
		Status:         domain.StatusPending,
		ScheduledAt:    now.Add(time.Minute),
		MaxRetries:     3,
		Backoff:        domain.BackoffExponential,
		WebhookHeaders: map[string]string{},
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	for _, opt := range opts {
		opt(j)
	}
	return j
}

func WithUserID(uid string) JobOption {
	return func(j *domain.Job) { j.UserID = uid }
}

func WithStatus(s domain.Status) JobOption {
	return func(j *domain.Job) { j.Status = s }
}

func WithScheduledAt(t time.Time) JobOption {
	return func(j *domain.Job) { j.ScheduledAt = t }
}

func WithMaxRetries(n int) JobOption {
	return func(j *domain.Job) { j.MaxRetries = n }
}

func WithRetryCount(n int) JobOption {
	return func(j *domain.Job) { j.RetryCount = n }
}

func WithWebhookURL(url string) JobOption {
	return func(j *domain.Job) { j.WebhookURL = &url }
}

func WithJobID(id string) JobOption {
	return func(j *domain.Job) { j.ID = id }
}

func WithMethod(m string) JobOption {
	return func(j *domain.Job) { j.Method = m }
}

func WithURL(url string) JobOption {
	return func(j *domain.Job) { j.URL = url }
}

func WithBody(body string) JobOption {
	return func(j *domain.Job) { j.Body = &body }
}

func WithBackoff(b domain.Backoff) JobOption {
	return func(j *domain.Job) { j.Backoff = b }
}

// ScheduleOption customizes a Schedule created by NewSchedule.
type ScheduleOption func(*domain.Schedule)

// NewSchedule creates a domain.Schedule with sensible defaults.
func NewSchedule(opts ...ScheduleOption) *domain.Schedule {
	now := time.Now().UTC()
	s := &domain.Schedule{
		ID:             uuid.New().String(),
		UserID:         "user-test-" + uuid.New().String()[:8],
		Name:           "test-schedule-" + uuid.New().String()[:8],
		CronExpr:       "*/5 * * * *",
		URL:            "https://httpbin.org/post",
		Method:         "POST",
		Headers:        map[string]string{},
		TimeoutSeconds: 30,
		MaxRetries:     3,
		Backoff:        domain.BackoffExponential,
		Paused:         false,
		NextRunAt:      now.Add(5 * time.Minute),
		WebhookHeaders: map[string]string{},
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func WithScheduleUserID(uid string) ScheduleOption {
	return func(s *domain.Schedule) { s.UserID = uid }
}

func WithScheduleName(name string) ScheduleOption {
	return func(s *domain.Schedule) { s.Name = name }
}

func WithCronExpr(expr string) ScheduleOption {
	return func(s *domain.Schedule) { s.CronExpr = expr }
}

func WithPaused(paused bool) ScheduleOption {
	return func(s *domain.Schedule) { s.Paused = paused }
}

func WithScheduleID(id string) ScheduleOption {
	return func(s *domain.Schedule) { s.ID = id }
}
