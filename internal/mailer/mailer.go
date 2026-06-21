// Package mailer is the outbound port for transactional email. It defines the
// Provider interface consumed by the alert usecase (verification emails) and
// the scheduler's AlertNotifier (failure / low-balance emails), plus a Resend
// implementation and a no-op fallback used when no provider is configured.
//
// The no-op mirrors the nil-AlertNotifier convention: email alerting is an
// opt-in dependency the service can run without. Callers should never assume an
// email was actually delivered — like the other channel types, delivery is
// best-effort.
package mailer

import (
	"context"
	"log/slog"
)

// Email is a single transactional message.
type Email struct {
	To      string
	Subject string
	// Text is the plain-text body. HTML is intentionally omitted — alert and
	// verification mails are short and render fine as text, and skipping HTML
	// keeps the provider surface minimal.
	Text string
}

// Provider sends transactional email. Implementations must be safe for
// concurrent use.
type Provider interface {
	// Send delivers msg. It returns an error only for a definitive failure to
	// hand the message to the provider; callers treat email as best-effort and
	// log rather than propagate.
	Send(ctx context.Context, msg Email) error
}

// NoopProvider is the fallback Provider used when no mail credentials are
// configured. It logs at debug level and reports success without sending.
type NoopProvider struct {
	logger *slog.Logger
}

// NewNoopProvider returns a Provider that drops every message. Used when
// RESEND_API_KEY is unset so the rest of the system behaves identically whether
// or not email is wired up.
func NewNoopProvider(logger *slog.Logger) *NoopProvider {
	return &NoopProvider{logger: logger.With("component", "mailer", "provider", "noop")}
}

func (p *NoopProvider) Send(ctx context.Context, msg Email) error {
	p.logger.DebugContext(ctx, "email dropped (no mail provider configured)",
		"to", msg.To, "subject", msg.Subject)
	return nil
}
