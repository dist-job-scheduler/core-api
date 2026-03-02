package repository

import (
	"context"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
)

type APITokenRepository interface {
	Create(ctx context.Context, userID, name, tokenHash, prefix string) (*domain.APIToken, error)
	FindByTokenHash(ctx context.Context, tokenHash string) (*domain.APIToken, error)
	ListByUserID(ctx context.Context, userID string) ([]*domain.APIToken, error)
	Delete(ctx context.Context, id, userID string) error
	UpdateLastUsed(ctx context.Context, id string) error
}
