package mailer

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newTestResend returns a ResendProvider whose Send hits srv instead of the
// real Resend API. White-box (same package) so it can set the unexported
// endpoint seam.
func newTestResend(apiKey, from, url string) *ResendProvider {
	p := NewResendProvider(apiKey, from)
	p.endpoint = url
	return p
}

func TestResend_Send_PostsExpectedRequest(t *testing.T) {
	t.Parallel()

	var (
		gotAuth        string
		gotContentType string
		gotMethod      string
		gotBody        resendRequest
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := newTestResend("re_test_key", "alerts@fliq.sh", srv.URL)
	err := p.Send(context.Background(), Email{
		To:      "user@example.com",
		Subject: "Your endpoint is down",
		Text:    "Fliq gave up after 5 attempts.",
	})
	if err != nil {
		t.Fatalf("Send returned error: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if want := "Bearer re_test_key"; gotAuth != want {
		t.Errorf("Authorization = %q, want %q", gotAuth, want)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}
	if gotBody.From != "alerts@fliq.sh" {
		t.Errorf("from = %q, want alerts@fliq.sh", gotBody.From)
	}
	if len(gotBody.To) != 1 || gotBody.To[0] != "user@example.com" {
		t.Errorf("to = %v, want [user@example.com]", gotBody.To)
	}
	if gotBody.Subject != "Your endpoint is down" {
		t.Errorf("subject = %q", gotBody.Subject)
	}
	if gotBody.Text != "Fliq gave up after 5 attempts." {
		t.Errorf("text = %q", gotBody.Text)
	}
}

func TestResend_Send_ErrorsOnNon2xx(t *testing.T) {
	t.Parallel()

	for _, code := range []int{http.StatusUnauthorized, http.StatusTooManyRequests, http.StatusInternalServerError} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(code)
		}))
		p := newTestResend("k", "from@fliq.sh", srv.URL)
		err := p.Send(context.Background(), Email{To: "u@example.com", Subject: "s", Text: "t"})
		srv.Close()
		if err == nil {
			t.Errorf("status %d: Send returned nil error, want failure", code)
		}
	}
}

func TestResend_Send_ErrorsWhenUnreachable(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close() // close immediately so the connection is refused

	p := newTestResend("k", "from@fliq.sh", url)
	if err := p.Send(context.Background(), Email{To: "u@example.com"}); err == nil {
		t.Error("Send to a closed server returned nil error, want failure")
	}
}

func TestNoopProvider_DropsAndSucceeds(t *testing.T) {
	t.Parallel()

	p := NewNoopProvider(testLogger())
	if err := p.Send(context.Background(), Email{To: "u@example.com", Subject: "s", Text: "t"}); err != nil {
		t.Fatalf("NoopProvider.Send returned error: %v", err)
	}
}
