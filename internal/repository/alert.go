package repository

import (
	"context"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
)

// AlertChannelRepository persists user-configured alert channels. Channels are
// few per user, so listing is unpaginated.
type AlertChannelRepository interface {
	Create(ctx context.Context, ch *domain.AlertChannel) (*domain.AlertChannel, error)
	GetByID(ctx context.Context, id, userID string) (*domain.AlertChannel, error)
	List(ctx context.Context, userID string) ([]*domain.AlertChannel, error)
	SetEnabled(ctx context.Context, id, userID string, enabled bool) error
	Delete(ctx context.Context, id, userID string) error

	// ListEnabled returns the user's enabled channels. Called by the scheduler
	// when a job or buffer item is permanently failed, to fan out the alert.
	ListEnabled(ctx context.Context, userID string) ([]*domain.AlertChannel, error)
}
