package scheduler

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/testutil"
)

// computeExpectedSig independently reproduces the signer.go formula so the test
// verifies the delivered signature exactly as a customer's SDK would.
func computeExpectedSig(secret, ts, method, url string, body []byte) string {
	bodyStr := ""
	if len(body) > 0 {
		bodyStr = string(body)
	}
	payload := fmt.Sprintf("%s.%s.%s.%s", ts, method, url, bodyStr)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return "v1=" + hex.EncodeToString(mac.Sum(nil))
}

// Deliver POSTs the body, applies user headers, and — when the user has a signing
// secret — attaches a valid X-Fliq-Signature that verifies against signRequest.
func TestWebhookNotifier_Deliver_SignsAndPosts(t *testing.T) {
	const secret = "whsec_test-secret-for-delivery"
	var (
		gotSig  string
		gotTS   string
		gotHdr  string
		gotBody []byte
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSig = r.Header.Get("X-Fliq-Signature")
		gotTS = r.Header.Get("X-Fliq-Timestamp")
		gotHdr = r.Header.Get("X-Custom")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	signing := &testutil.MockSigningSecretRepository{
		GetActiveFn: func(_ context.Context, userID string) (*domain.SigningSecret, error) {
			return &domain.SigningSecret{UserID: userID, Secret: secret, IsActive: true}, nil
		},
	}
	n := newWebhookNotifier(slog.Default(), signing, srv.Client())

	body := []byte(`{"event":"job.completed","job_id":"job-1"}`)
	code, err := n.Deliver(context.Background(), "user-1", srv.URL, map[string]string{"X-Custom": "abc"}, body)
	if err != nil {
		t.Fatalf("Deliver error: %v", err)
	}
	if code != http.StatusOK {
		t.Fatalf("Deliver code = %d, want 200", code)
	}
	if string(gotBody) != string(body) {
		t.Fatalf("delivered body = %q, want %q", gotBody, body)
	}
	if gotHdr != "abc" {
		t.Errorf("custom header not forwarded, got %q", gotHdr)
	}
	if gotSig == "" || gotTS == "" {
		t.Fatal("missing signature/timestamp headers on signed delivery")
	}
	// The signature must verify against the exact timestamp + bytes delivered.
	if wantSig := computeExpectedSig(secret, gotTS, http.MethodPost, srv.URL, gotBody); wantSig != gotSig {
		t.Errorf("signature = %q, want %q", gotSig, wantSig)
	}
}

// Deliver still succeeds (unsigned) when the user has no signing secret.
func TestWebhookNotifier_Deliver_UnsignedWhenNoSecret(t *testing.T) {
	var hadSig bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hadSig = r.Header.Get("X-Fliq-Signature") != ""
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	n := newWebhookNotifier(slog.Default(), &testutil.MockSigningSecretRepository{}, srv.Client()) // default: ErrSigningSecretNotFound
	code, err := n.Deliver(context.Background(), "user-1", srv.URL, nil, []byte(`{}`))
	if err != nil {
		t.Fatalf("Deliver error: %v", err)
	}
	if code != http.StatusNoContent {
		t.Fatalf("code = %d, want 204", code)
	}
	if hadSig {
		t.Error("unsigned delivery should not carry X-Fliq-Signature")
	}
}

// buildDelivery returns a correctly-shaped delivery when the job carries a webhook
// URL, and nil when it doesn't. The row is inserted atomically with the job's
// terminal transition (CompleteWithWebhook/FailWithWebhook), tested at the repo layer.
func TestWorker_BuildDelivery(t *testing.T) {
	w := &Worker{webhookMaxAttempts: 7, logger: slog.Default()}

	url := "https://example.com/hook"
	job := &domain.Job{
		ID:             "job-42",
		UserID:         "user-9",
		Status:         domain.StatusCompleted,
		RetryCount:     2,
		WebhookURL:     &url,
		WebhookHeaders: map[string]string{"X-A": "b"},
	}
	code := 200
	d := w.buildDelivery(context.Background(), job, domain.WebhookEventJobCompleted, &code)

	if d == nil {
		t.Fatal("no delivery built for a job with a webhook URL")
	}
	if d.URL != url || d.UserID != "user-9" || d.Event != domain.WebhookEventJobCompleted {
		t.Errorf("delivery fields wrong: %+v", d)
	}
	if d.JobID == nil || *d.JobID != "job-42" {
		t.Errorf("delivery JobID = %v, want job-42", d.JobID)
	}
	if d.MaxAttempts != 7 {
		t.Errorf("MaxAttempts = %d, want 7 (from worker config)", d.MaxAttempts)
	}
	var payload WebhookPayload
	if err := json.Unmarshal(d.Payload, &payload); err != nil {
		t.Fatalf("payload not valid JSON: %v", err)
	}
	if payload.Event != "job.completed" || payload.JobID != "job-42" || payload.AttemptNum != 3 {
		t.Errorf("payload = %+v, want event=job.completed job_id=job-42 attempt_num=3", payload)
	}

	// No webhook URL → nil (nothing to deliver).
	if w.buildDelivery(context.Background(), &domain.Job{ID: "job-x", UserID: "u"}, domain.WebhookEventJobFailed, nil) != nil {
		t.Error("built a delivery for a job with no webhook URL")
	}
}
