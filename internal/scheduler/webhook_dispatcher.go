package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/metrics"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/repository"
)

const (
	// webhookInflightTimeout is how long a claimed ('delivering') delivery may sit
	// without an outcome before another dispatcher cycle reclaims it. It must exceed
	// the notifier's HTTP client timeout (10s) so an attempt that is merely slow is
	// never re-fired concurrently.
	webhookInflightTimeout = 60 * time.Second

	webhookBatchSize = 50

	// defaultWebhookBaseDelay is the backoff before the first retry; each subsequent
	// retry doubles it, capped at webhookMaxDelay.
	defaultWebhookBaseDelay = 30 * time.Second
	webhookMaxDelay         = 1 * time.Hour
)

// webhookDeliverer signs and POSTs a single webhook body. *WebhookNotifier
// implements it; the interface keeps the dispatcher unit-testable.
type webhookDeliverer interface {
	Deliver(ctx context.Context, userID, url string, headers map[string]string, body []byte) (statusCode int, err error)
}

// WebhookDispatcher drains the durable webhook_deliveries queue: it claims due
// deliveries, POSTs each, and records the outcome — retrying with backoff on
// failure until a 2xx or the attempt budget is exhausted. It runs in the
// scheduler process (single leader), off the request path.
type WebhookDispatcher struct {
	repo      repository.WebhookDeliveryRepository
	deliverer webhookDeliverer
	logger    *slog.Logger
	interval  time.Duration
	batchSize int
	baseDelay time.Duration
}

func NewWebhookDispatcher(repo repository.WebhookDeliveryRepository, deliverer webhookDeliverer, logger *slog.Logger, interval time.Duration) *WebhookDispatcher {
	return &WebhookDispatcher{
		repo:      repo,
		deliverer: deliverer,
		logger:    logger.With("component", "webhook_dispatcher"),
		interval:  interval,
		batchSize: webhookBatchSize,
		baseDelay: defaultWebhookBaseDelay,
	}
}

func (d *WebhookDispatcher) Start(ctx context.Context) {
	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()

	d.logger.InfoContext(ctx, "webhook dispatcher started", "interval", d.interval)

	for {
		select {
		case <-ctx.Done():
			d.logger.InfoContext(ctx, "webhook dispatcher shut down")
			return
		case <-ticker.C:
			d.dispatchBatch(ctx)
		}
	}
}

func (d *WebhookDispatcher) dispatchBatch(ctx context.Context) {
	due, err := d.repo.ClaimDue(ctx, d.batchSize, webhookInflightTimeout)
	if err != nil {
		d.logger.ErrorContext(ctx, "claim due webhook deliveries", "error", err)
		return
	}
	if len(due) == 0 {
		return
	}

	// Deliver the batch concurrently so one slow/hanging customer URL never blocks
	// the others. Each row is already marked 'delivering', so this is safe.
	var wg sync.WaitGroup
	for _, del := range due {
		wg.Add(1)
		go func(del *domain.WebhookDelivery) {
			defer wg.Done()
			d.deliverOne(ctx, del)
		}(del)
	}
	wg.Wait()
}

func (d *WebhookDispatcher) deliverOne(ctx context.Context, del *domain.WebhookDelivery) {
	statusCode, err := d.deliverer.Deliver(ctx, del.UserID, del.URL, del.Headers, del.Payload)

	var scPtr *int
	if statusCode != 0 {
		sc := statusCode
		scPtr = &sc
	}

	if err == nil && statusCode >= 200 && statusCode < 300 {
		metrics.WebhookDeliveriesTotal.WithLabelValues("success").Inc()
		if markErr := d.repo.MarkDelivered(ctx, del.ID, statusCode); markErr != nil {
			d.logger.ErrorContext(ctx, "mark webhook delivered", "delivery_id", del.ID, "error", markErr)
		}
		return
	}

	metrics.WebhookDeliveriesTotal.WithLabelValues("failure").Inc()
	lastErr := deliveryErrorString(statusCode, err)
	attemptsMade := del.Attempts + 1

	if attemptsMade >= del.MaxAttempts {
		d.logger.WarnContext(ctx, "webhook delivery permanently failed",
			"delivery_id", del.ID, "attempts", attemptsMade, "last_error", lastErr)
		if markErr := d.repo.MarkFailed(ctx, del.ID, scPtr, lastErr); markErr != nil {
			d.logger.ErrorContext(ctx, "mark webhook failed", "delivery_id", del.ID, "error", markErr)
		}
		return
	}

	nextRetry := time.Now().Add(webhookRetryDelay(d.baseDelay, attemptsMade))
	d.logger.WarnContext(ctx, "webhook delivery failed, will retry",
		"delivery_id", del.ID, "attempt", attemptsMade, "max_attempts", del.MaxAttempts,
		"retry_at", nextRetry, "last_error", lastErr)
	if markErr := d.repo.Reschedule(ctx, del.ID, scPtr, lastErr, nextRetry); markErr != nil {
		d.logger.ErrorContext(ctx, "reschedule webhook delivery", "delivery_id", del.ID, "error", markErr)
	}
}

// webhookRetryDelay is the backoff before the next attempt. attemptsMade is the
// number of attempts already completed (>= 1): delay = base * 2^(attemptsMade-1),
// capped at webhookMaxDelay. Deterministic (no jitter) so the state machine is
// straightforward to test.
func webhookRetryDelay(base time.Duration, attemptsMade int) time.Duration {
	if attemptsMade < 1 {
		attemptsMade = 1
	}
	delay := base
	for i := 1; i < attemptsMade; i++ {
		delay *= 2
		if delay >= webhookMaxDelay {
			return webhookMaxDelay
		}
	}
	if delay > webhookMaxDelay {
		return webhookMaxDelay
	}
	return delay
}

func deliveryErrorString(statusCode int, err error) string {
	if err != nil {
		return err.Error()
	}
	return fmt.Sprintf("non-2xx response: %d", statusCode)
}
