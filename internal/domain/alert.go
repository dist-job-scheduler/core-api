package domain

import (
	"errors"
	"time"
)

var (
	ErrAlertChannelNotFound    = errors.New("alert channel not found")
	ErrInvalidAlertChannelType = errors.New("invalid alert channel type")
)

// AlertChannelType is the delivery mechanism for an alert channel.
type AlertChannelType string

const (
	// AlertChannelWebhook POSTs a structured JSON payload to the target URL.
	AlertChannelWebhook AlertChannelType = "webhook"
	// AlertChannelSlack POSTs a Slack-incoming-webhook message ({"text": ...})
	// to the target URL.
	AlertChannelSlack AlertChannelType = "slack"
)

// ValidAlertChannelType reports whether t is a supported channel type.
func ValidAlertChannelType(t AlertChannelType) bool {
	switch t {
	case AlertChannelWebhook, AlertChannelSlack:
		return true
	default:
		return false
	}
}

// AlertChannel is a user-configured destination notified when one of the user's
// jobs or buffer items exhausts its retries (permanent failure).
type AlertChannel struct {
	ID        string
	UserID    string
	Type      AlertChannelType
	Target    string
	Name      string
	Enabled   bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// AlertResourceType identifies what kind of resource failed.
type AlertResourceType string

const (
	AlertResourceJob        AlertResourceType = "job"
	AlertResourceBufferItem AlertResourceType = "buffer_item"
)

// AlertEvent is the failure that triggers alert delivery. It is assembled by the
// scheduler at the moment a job or buffer item is marked permanently failed and
// passed to the alert notifier for fan-out across the user's enabled channels.
type AlertEvent struct {
	UserID       string
	ResourceType AlertResourceType
	ResourceID   string
	// BufferID is set only for buffer_item events.
	BufferID   string
	URL        string
	Method     string
	LastError  string
	StatusCode *int
	// Attempts is the total number of execution attempts made before giving up.
	Attempts int
	FailedAt time.Time
}
