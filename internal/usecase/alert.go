package usecase

import (
	"context"
	"fmt"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/emailverify"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/mailer"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/repository"
)

type AlertUsecase struct {
	repo    repository.AlertChannelRepository
	mailer  mailer.Provider
	signer  *emailverify.Signer
	baseURL string
}

// NewAlertUsecase wires the alert-channel store with the email-verification
// dependencies. mailer and signer are only exercised for email channels; for a
// webhook/slack-only deployment they can be a NoopProvider and any signer.
func NewAlertUsecase(repo repository.AlertChannelRepository, mail mailer.Provider, signer *emailverify.Signer, baseURL string) *AlertUsecase {
	return &AlertUsecase{repo: repo, mailer: mail, signer: signer, baseURL: baseURL}
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

	// Email channels start disabled+unverified: we must confirm the address owns
	// itself before delivering, so Fliq cannot be used to spam third parties.
	// webhook/slack channels are enabled and verified on creation (the target is
	// a caller-owned URL).
	verified := !input.Type.RequiresVerification()

	ch := &domain.AlertChannel{
		UserID:   input.UserID,
		Type:     input.Type,
		Target:   input.Target,
		Name:     input.Name,
		Enabled:  verified,
		Verified: verified,
	}

	created, err := u.repo.Create(ctx, ch)
	if err != nil {
		return nil, fmt.Errorf("create alert channel: %w", err)
	}

	if input.Type.RequiresVerification() {
		// Best-effort: a failed verification send must not fail channel creation.
		// The channel stays disabled until confirmed; the user can re-trigger by
		// recreating it. (Live send requires RESEND_API_KEY; otherwise the noop
		// mailer drops it.)
		u.sendVerificationEmail(ctx, created)
	}

	return created, nil
}

// sendVerificationEmail mails a signed confirm link to the channel's target
// address. Best-effort — errors are swallowed (channel stays unverified).
func (u *AlertUsecase) sendVerificationEmail(ctx context.Context, ch *domain.AlertChannel) {
	token := u.signer.Sign(ch.ID)
	link := fmt.Sprintf("%s/alerts/verify?token=%s", u.baseURL, token)
	_ = u.mailer.Send(ctx, mailer.Email{
		To:      ch.Target,
		Subject: "Confirm your Fliq alert email",
		Text: fmt.Sprintf(
			"You (or someone) added this address as a Fliq failure-alert destination.\n\n"+
				"Confirm it to start receiving alerts:\n%s\n\n"+
				"If you didn't request this, ignore this email — no alerts will be sent.",
			link),
	})
}

// VerifyChannel confirms an email channel from a signed token, enabling it.
func (u *AlertUsecase) VerifyChannel(ctx context.Context, token string) error {
	channelID, err := u.signer.Verify(token)
	if err != nil {
		return fmt.Errorf("verify alert channel token: %w", err)
	}
	if err := u.repo.MarkVerified(ctx, channelID); err != nil {
		return fmt.Errorf("mark alert channel verified: %w", err)
	}
	return nil
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
	// An unverified email channel can never be enabled via the API — only the
	// confirm-token endpoint flips it. This closes the obvious bypass of the
	// verification gate (PATCH enabled=true on a freshly created email channel).
	if enabled {
		ch, err := u.repo.GetByID(ctx, id, userID)
		if err != nil {
			return fmt.Errorf("get alert channel: %w", err)
		}
		if !ch.Verified {
			return domain.ErrAlertChannelNotVerified
		}
	}
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
