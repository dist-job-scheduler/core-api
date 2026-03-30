package domain

import (
	"errors"
	"time"
)

var (
	ErrBufferNotFound      = errors.New("buffer not found")
	ErrBufferNameConflict  = errors.New("buffer with this name already exists")
	ErrBufferAlreadyPaused = errors.New("buffer is already paused")
	ErrBufferNotPaused     = errors.New("buffer is not paused")
	ErrBufferItemNotFound  = errors.New("buffer item not found")
)

type BufferItemStatus string

const (
	BufferItemPending   BufferItemStatus = "pending"
	BufferItemRunning   BufferItemStatus = "running"
	BufferItemCompleted BufferItemStatus = "completed"
	BufferItemFailed    BufferItemStatus = "failed"
)

type Buffer struct {
	ID             string
	UserID         string
	Name           string
	URL            string
	Method         string
	Headers        map[string]string
	TimeoutSeconds int
	RateLimit      int
	MaxRetries     int
	Backoff        Backoff
	Paused         bool
	WebhookURL     *string
	WebhookHeaders map[string]string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type BufferItem struct {
	ID             string
	BufferID       string
	UserID         string
	URL            string
	Method         string
	Headers        map[string]string
	Body           *string
	TimeoutSeconds int
	Backoff        Backoff

	Status     BufferItemStatus
	RetryCount int
	MaxRetries int

	ScheduledAt time.Time
	ClaimedAt   *time.Time
	ClaimedBy   *string
	HeartbeatAt *time.Time
	CompletedAt *time.Time
	LastError   *string
	StatusCode  *int

	CreatedAt time.Time
	UpdatedAt time.Time
}
