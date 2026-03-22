package repository

import (
	"context"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
)

// SigningSecretRepository manages per-user HMAC signing secrets.
type SigningSecretRepository interface {
	// GetActive returns the active signing secret for the user.
	// Returns domain.ErrSigningSecretNotFound if none exists.
	GetActive(ctx context.Context, userID string) (*domain.SigningSecret, error)

	// Create generates and stores a new active signing secret for the user.
	// If one already exists, returns the existing one.
	Create(ctx context.Context, userID string) (*domain.SigningSecret, error)

	// Rotate deactivates the current active secret and creates a new one.
	// Returns the newly created secret.
	Rotate(ctx context.Context, userID string) (*domain.SigningSecret, error)
}
