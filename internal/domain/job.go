package domain

import (
	"errors"
	"time"
)

var (
	ErrJobNotFound       = errors.New("job not found")
	ErrDuplicateJob      = errors.New("job with this idempotency key already exists")
	ErrInvalidStatus     = errors.New("invalid status value")
	ErrInvalidMethod     = errors.New("invalid HTTP method filter")
	ErrInvalidSearch     = errors.New("invalid search query")
	ErrJobNotCancellable = errors.New("job is not in a cancellable state")
	ErrJobNotReplayable  = errors.New("job is not in a replayable state")
)

type Status string

const (
	StatusPending   Status = "pending"
	StatusRunning   Status = "running"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

type Backoff string

const (
	BackoffExponential Backoff = "exponential"
	BackoffLinear      Backoff = "linear"
)

type Job struct {
	ID             string            `json:"id"`
	UserID         string            `json:"userID"`
	IdempotencyKey string            `json:"idempotencyKey"`
	URL            string            `json:"url"`
	Method         string            `json:"method"`
	Headers        map[string]string `json:"headers"`
	Body           *string           `json:"body,omitempty"`
	TimeoutSeconds int               `json:"timeoutSeconds"`

	Status      Status    `json:"status"`
	ScheduledAt time.Time `json:"scheduledAt"`

	RetryCount int     `json:"retryCount"`
	MaxRetries int     `json:"maxRetries"`
	Backoff    Backoff `json:"backoff"`

	ClaimedAt   *time.Time `json:"claimedAt"`
	ClaimedBy   *string    `json:"claimedBy"`
	HeartbeatAt *time.Time `json:"heartbeatAt"`
	CompletedAt *time.Time `json:"completedAt"`
	LastError   *string    `json:"lastError"`

	ScheduleID *string `json:"scheduleID,omitempty"`

	// ReplayOf is the ID of the failed job this one was cloned from via the
	// replay endpoint. Nil for ordinary jobs.
	ReplayOf *string `json:"replayOf,omitempty"`

	WebhookURL     *string           `json:"webhookURL,omitempty"`
	WebhookHeaders map[string]string `json:"webhookHeaders,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type JobAttempt struct {
	ID          string
	JobID       string
	AttemptNum  int
	WorkerID    string
	StartedAt   time.Time
	CompletedAt *time.Time
	StatusCode  *int
	Error       *string
	DurationMS  *int64
}
