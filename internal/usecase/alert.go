package usecase

import (
	"context"
	"fmt"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/repository"
)

type AlertUsecase struct {
	repo repository.AlertChannelRepository
}

func NewAlertUsecase(repo repository.AlertChannelRepository) *AlertUsecase {
	return &AlertUsecase{repo: repo}
}

type CreateAlertChannelInput struct {
	UserID string
	Type   domain.AlertChannelType
	Target string
	Name   string
}

func (u *AlertUsecase) CreateChannel(ctx context.Context, input CreateAlertChannelInput) (*domain.AlertChannel, error) {
	if !domain.ValidAlertChannelType(input.Type) {
		return nil, domain.ErrInvalidAlertChannelType
	}

	ch := &domain.AlertChannel{
		UserID:  input.UserID,
		Type:    input.Type,
		Target:  input.Target,
		Name:    input.Name,
		Enabled: true,
	}

	created, err := u.repo.Create(ctx, ch)
	if err != nil {
		return nil, fmt.Errorf("create alert channel: %w", err)
	}
	return created, nil
}

func (u *AlertUsecase) ListChannels(ctx context.Context, userID string) ([]*domain.AlertChannel, error) {
	channels, err := u.repo.List(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list alert channels: %w", err)
	}
	return channels, nil
}

func (u *AlertUsecase) GetChannel(ctx context.Context, id, userID string) (*domain.AlertChannel, error) {
	ch, err := u.repo.GetByID(ctx, id, userID)
	if err != nil {
		return nil, fmt.Errorf("get alert channel: %w", err)
	}
	return ch, nil
}

func (u *AlertUsecase) SetEnabled(ctx context.Context, id, userID string, enabled bool) error {
	if err := u.repo.SetEnabled(ctx, id, userID, enabled); err != nil {
		return fmt.Errorf("set alert channel enabled: %w", err)
	}
	return nil
}

func (u *AlertUsecase) DeleteChannel(ctx context.Context, id, userID string) error {
	if err := u.repo.Delete(ctx, id, userID); err != nil {
		return fmt.Errorf("delete alert channel: %w", err)
	}
	return nil
}
