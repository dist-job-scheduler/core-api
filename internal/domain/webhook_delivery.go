package domain

import (
	"errors"
	"time"
)

// ErrWebhookDeliveryNotFound is returned when no delivery matches the lookup.
var ErrWebhookDeliveryNotFound = errors.New("webhook delivery not found")

// WebhookEvent identifies what happened to the resource that triggered a delivery.
type WebhookEvent string

const (
	WebhookEventJobCompleted WebhookEvent = "job.completed"
	WebhookEventJobFailed    WebhookEvent = "job.failed"
)

// WebhookDeliveryStatus is the delivery state machine:
//
//	pending    → queued, waiting for its next attempt (next_retry_at has passed)
//	delivering → claimed by a dispatcher, an attempt is in flight
//	delivered  → a 2xx was received (terminal)
//	failed     → the attempt budget was exhausted without a 2xx (terminal)
type WebhookDeliveryStatus string

const (
	WebhookDeliveryPending    WebhookDeliveryStatus = "pending"
	WebhookDeliveryDelivering WebhookDeliveryStatus = "delivering"
	WebhookDeliveryDelivered  WebhookDeliveryStatus = "delivered"
	WebhookDeliveryFailed     WebhookDeliveryStatus = "failed"
)

// WebhookDelivery is one durable, at-least-once webhook delivery record. The
// dispatcher owns the state transitions; see WebhookDeliveryRepository.
type WebhookDelivery struct {
	ID     string
	UserID string
	// JobID is the job whose terminal state triggered this delivery (nil if the
	// event has no originating job).
	JobID   *string
	Event   WebhookEvent
	URL     string
	Headers map[string]string
	// Payload is the exact JSON body that is signed and POSTed.
	Payload     []byte
	Status      WebhookDeliveryStatus
	StatusCode  *int
	Attempts    int
	MaxAttempts int
	NextRetryAt time.Time
	LastError   *string
	DeliveredAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
