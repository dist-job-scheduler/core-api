package repository

import (
	"context"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
)

// WebhookDeliveryRepository persists the durable webhook delivery log and drives
// the at-least-once retry state machine.
type WebhookDeliveryRepository interface {
	// Enqueue inserts a new pending delivery, due immediately.
	Enqueue(ctx context.Context, d *domain.WebhookDelivery) (*domain.WebhookDelivery, error)

	// ClaimDue atomically claims up to limit deliveries that are ready to attempt —
	// pending rows whose next_retry_at has passed, plus in-flight ('delivering') rows
	// abandoned by a crashed dispatcher (not updated within inflightTimeout). Claimed
	// rows move to 'delivering' so a concurrent dispatcher won't pick them up.
	ClaimDue(ctx context.Context, limit int, inflightTimeout time.Duration) ([]*domain.WebhookDelivery, error)

	// MarkDelivered records a terminal success (2xx received).
	MarkDelivered(ctx context.Context, id string, statusCode int) error

	// Reschedule records a failed attempt and re-queues the delivery for a later
	// retry at nextRetryAt. statusCode is nil when the request never completed.
	Reschedule(ctx context.Context, id string, statusCode *int, lastErr string, nextRetryAt time.Time) error

	// MarkFailed records a terminal failure (attempt budget exhausted).
	MarkFailed(ctx context.Context, id string, statusCode *int, lastErr string) error

	// ListByUser returns the user's deliveries newest-first, keyset-paginated on
	// (created_at, id). Pass a nil cursor for the first page.
	ListByUser(ctx context.Context, userID string, limit int, cursorTime *time.Time, cursorID string) ([]*domain.WebhookDelivery, error)
}
