package usecase_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/emailverify"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/mailer"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/testutil"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/usecase"
)

const testSecret = "test-secret-32-chars-long-xxxxxx"

func newTestAlertUsecase(repo *testutil.MockAlertChannelRepository, mail mailer.Provider) *usecase.AlertUsecase {
	if mail == nil {
		mail = &testutil.FakeMailer{}
	}
	return usecase.NewAlertUsecase(repo, mail, emailverify.NewSigner([]byte(testSecret)), "https://api.fliq.test")
}

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
	uc := newTestAlertUsecase(repo, nil)

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
	if !saved.Enabled || !saved.Verified {
		t.Error("webhook/slack channel should be enabled and verified by default")
	}
}

func TestCreateAlertChannel_Email_StartsDisabledAndSendsVerification(t *testing.T) {
	t.Parallel()

	var saved *domain.AlertChannel
	repo := &testutil.MockAlertChannelRepository{
		CreateFn: func(_ context.Context, ch *domain.AlertChannel) (*domain.AlertChannel, error) {
			ch.ID = "alert-email-1"
			saved = ch
			return ch, nil
		},
	}
	mail := &testutil.FakeMailer{}
	uc := newTestAlertUsecase(repo, mail)

	_, err := uc.CreateChannel(context.Background(), usecase.CreateAlertChannelInput{
		UserID: "user-1",
		Type:   domain.AlertChannelEmail,
		Target: "ops@example.com",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if saved.Enabled || saved.Verified {
		t.Error("email channel must start disabled + unverified until confirmed")
	}
	msgs := mail.Messages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 verification email, got %d", len(msgs))
	}
	if msgs[0].To != "ops@example.com" {
		t.Errorf("verification email To = %q, want ops@example.com", msgs[0].To)
	}
	if !strings.Contains(msgs[0].Text, "/alerts/verify?token=") {
		t.Errorf("verification email missing confirm link: %q", msgs[0].Text)
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
	uc := newTestAlertUsecase(repo, nil)

	_, err := uc.CreateChannel(context.Background(), usecase.CreateAlertChannelInput{
		UserID: "user-1",
		Type:   domain.AlertChannelType("carrier_pigeon"),
		Target: "coop-3",
	})
	if !errors.Is(err, domain.ErrInvalidAlertChannelType) {
		t.Errorf("err = %v, want ErrInvalidAlertChannelType", err)
	}
}

func TestVerifyChannel_RoundTrip(t *testing.T) {
	t.Parallel()

	var verifiedID string
	repo := &testutil.MockAlertChannelRepository{
		CreateFn: func(_ context.Context, ch *domain.AlertChannel) (*domain.AlertChannel, error) {
			ch.ID = "alert-email-1"
			return ch, nil
		},
		MarkVerifiedFn: func(_ context.Context, id string) error {
			verifiedID = id
			return nil
		},
	}
	mail := &testutil.FakeMailer{}
	uc := newTestAlertUsecase(repo, mail)

	created, err := uc.CreateChannel(context.Background(), usecase.CreateAlertChannelInput{
		UserID: "user-1",
		Type:   domain.AlertChannelEmail,
		Target: "ops@example.com",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Pull the token straight out of the email the usecase just sent.
	text := mail.Messages()[0].Text
	i := strings.Index(text, "token=")
	if i < 0 {
		t.Fatal("no token in verification email")
	}
	token := strings.Fields(text[i+len("token="):])[0]

	if err := uc.VerifyChannel(context.Background(), token); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if verifiedID != created.ID {
		t.Errorf("verified id = %q, want %q", verifiedID, created.ID)
	}
}

func TestVerifyChannel_BadToken(t *testing.T) {
	t.Parallel()

	repo := &testutil.MockAlertChannelRepository{
		MarkVerifiedFn: func(_ context.Context, _ string) error {
			t.Fatal("MarkVerified must not be called for a bad token")
			return nil
		},
	}
	uc := newTestAlertUsecase(repo, nil)

	err := uc.VerifyChannel(context.Background(), "not.a.token")
	if !errors.Is(err, domain.ErrInvalidVerificationToken) {
		t.Errorf("err = %v, want ErrInvalidVerificationToken", err)
	}
}

func TestSetEnabled_UnverifiedEmailRejected(t *testing.T) {
	t.Parallel()

	repo := &testutil.MockAlertChannelRepository{
		GetByIDFn: func(_ context.Context, _, _ string) (*domain.AlertChannel, error) {
			return &domain.AlertChannel{ID: "e", Type: domain.AlertChannelEmail, Verified: false}, nil
		},
		SetEnabledFn: func(_ context.Context, _, _ string, _ bool) error {
			t.Fatal("SetEnabled must not be called for an unverified email channel")
			return nil
		},
	}
	uc := newTestAlertUsecase(repo, nil)

	err := uc.SetEnabled(context.Background(), "e", "user-1", true)
	if !errors.Is(err, domain.ErrAlertChannelNotVerified) {
		t.Errorf("err = %v, want ErrAlertChannelNotVerified", err)
	}
}

func TestAlertChannel_SetEnabled_NotFound(t *testing.T) {
	t.Parallel()

	repo := &testutil.MockAlertChannelRepository{
		SetEnabledFn: func(_ context.Context, _, _ string, _ bool) error {
			return domain.ErrAlertChannelNotFound
		},
	}
	uc := newTestAlertUsecase(repo, nil)

	// Disable path skips the verification check, so this exercises the repo error.
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
	uc := newTestAlertUsecase(repo, nil)

	got, err := uc.ListChannels(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("len = %d, want 2", len(got))
	}
}
