package domain

import (
	"errors"
	"time"
)

var (
	ErrScheduleNotFound      = errors.New("schedule not found")
	ErrInvalidCronExpr       = errors.New("invalid cron expression")
	ErrScheduleAlreadyPaused = errors.New("schedule is already paused")
	ErrScheduleNotPaused     = errors.New("schedule is not paused")
	ErrScheduleNameConflict  = errors.New("schedule with this name already exists")
)

type Schedule struct {
	ID             string
	UserID         string
	Name           string
	CronExpr       string
	URL            string
	Method         string
	Headers        map[string]string
	Body           *string
	TimeoutSeconds int
	MaxRetries     int
	Backoff        Backoff
	Paused         bool
	NextRunAt      time.Time
	LastRunAt      *time.Time
	WebhookURL     *string
	WebhookHeaders map[string]string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
