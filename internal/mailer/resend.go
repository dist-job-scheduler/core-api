package mailer

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// resendEndpoint is the Resend transactional-email API. The host is fixed and
// provider-owned, so no SSRF-safe dialer is needed (unlike user-supplied
// webhook targets).
const resendEndpoint = "https://api.resend.com/emails"

// ResendProvider sends email via the Resend HTTP API (https://resend.com).
type ResendProvider struct {
	apiKey string
	from   string
	client *http.Client
}

// NewResendProvider returns a Provider backed by Resend. apiKey is the
// RESEND_API_KEY secret; from is the verified sender address (ALERT_EMAIL_FROM).
func NewResendProvider(apiKey, from string) *ResendProvider {
	return &ResendProvider{
		apiKey: apiKey,
		from:   from,
		client: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
			},
		},
	}
}

type resendRequest struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	Text    string   `json:"text"`
}

func (p *ResendProvider) Send(ctx context.Context, msg Email) error {
	body, err := json.Marshal(resendRequest{
		From:    p.from,
		To:      []string{msg.To},
		Subject: msg.Subject,
		Text:    msg.Text,
	})
	if err != nil {
		return fmt.Errorf("marshal resend request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, resendEndpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build resend request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("resend send: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("resend send: unexpected status %d", resp.StatusCode)
	}
	return nil
}
