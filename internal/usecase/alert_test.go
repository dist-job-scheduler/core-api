package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/testutil"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/usecase"
)

func TestCreateAlertChannel_HappyPath(t *testing.T) {
	t.Parallel()

	var saved *domain.AlertChannel
	repo := &testutil.MockAlertChannelRepository{
		CreateFn: func(_ context.Context, ch *domain.AlertChannel) (*domain.AlertChannel, error) {
			ch.ID = "alert-1"
			saved = ch
			return ch, nil
		},
	}
	uc := usecase.NewAlertUsecase(repo)

	got, err := uc.CreateChannel(context.Background(), usecase.CreateAlertChannelInput{
		UserID: "user-1",
		Type:   domain.AlertChannelSlack,
		Target: "https://hooks.slack.com/services/x",
		Name:   "ops",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != "alert-1" {
		t.Errorf("id = %q, want alert-1", got.ID)
	}
	if !saved.Enabled {
		t.Error("new channel should be enabled by default")
	}
}

func TestCreateAlertChannel_InvalidType(t *testing.T) {
	t.Parallel()

	repo := &testutil.MockAlertChannelRepository{
		CreateFn: func(_ context.Context, _ *domain.AlertChannel) (*domain.AlertChannel, error) {
			t.Fatal("Create must not be called for an invalid type")
			return nil, nil
		},
	}
	uc := usecase.NewAlertUsecase(repo)

	_, err := uc.CreateChannel(context.Background(), usecase.CreateAlertChannelInput{
		UserID: "user-1",
		Type:   domain.AlertChannelType("email"),
		Target: "ops@example.com",
	})
	if !errors.Is(err, domain.ErrInvalidAlertChannelType) {
		t.Errorf("err = %v, want ErrInvalidAlertChannelType", err)
	}
}

func TestAlertChannel_SetEnabled_NotFound(t *testing.T) {
	t.Parallel()

	repo := &testutil.MockAlertChannelRepository{
		SetEnabledFn: func(_ context.Context, _, _ string, _ bool) error {
			return domain.ErrAlertChannelNotFound
		},
	}
	uc := usecase.NewAlertUsecase(repo)

	err := uc.SetEnabled(context.Background(), "missing", "user-1", false)
	if !errors.Is(err, domain.ErrAlertChannelNotFound) {
		t.Errorf("err = %v, want ErrAlertChannelNotFound", err)
	}
}

func TestAlertChannel_List(t *testing.T) {
	t.Parallel()

	repo := &testutil.MockAlertChannelRepository{
		ListFn: func(_ context.Context, _ string) ([]*domain.AlertChannel, error) {
			return []*domain.AlertChannel{{ID: "a"}, {ID: "b"}}, nil
		},
	}
	uc := usecase.NewAlertUsecase(repo)

	got, err := uc.ListChannels(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("len = %d, want 2", len(got))
	}
}
