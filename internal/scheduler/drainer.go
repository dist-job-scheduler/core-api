package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/metrics"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/repository"
)

const defaultRetryAfter = 60 * time.Second

// BufferDrainer polls for active buffers and drains their pending items at
// each buffer's configured rate limit.
type BufferDrainer struct {
	id             string
	repo           repository.BufferRepository
	credits        repository.CreditRepository
	signingSecrets repository.SigningSecretRepository
	executor       *Executor
	notifier       *WebhookNotifier
	alerts         *AlertNotifier
	lowBalance     LowBalanceConfig
	logger         *slog.Logger
	pollInterval   time.Duration
	concurrency    int
	sem            chan struct{}
}

func NewBufferDrainer(
	repo repository.BufferRepository,
	credits repository.CreditRepository,
	signingSecrets repository.SigningSecretRepository,
	notifier *WebhookNotifier,
	alerts *AlertNotifier,
	lowBalance LowBalanceConfig,
	logger *slog.Logger,
	pollInterval time.Duration,
	concurrency int,
) *BufferDrainer {
	hostname, _ := os.Hostname()
	id := fmt.Sprintf("drainer-%s-%d", hostname, os.Getpid())
	return &BufferDrainer{
		id:             id,
		repo:           repo,
		credits:        credits,
		signingSecrets: signingSecrets,
		executor:       NewExecutor(logger),
		notifier:       notifier,
		alerts:         alerts,
		lowBalance:     lowBalance,
		logger:         logger.With("component", "buffer_drainer", "worker_id", id),
		pollInterval:   pollInterval,
		concurrency:    concurrency,
		sem:            make(chan struct{}, concurrency),
	}
}

// alertItemFailure fires a best-effort failure alert for a permanently-failed
// buffer item.
func (d *BufferDrainer) alertItemFailure(ctx context.Context, item *domain.BufferItem, statusCode *int, errMsg string) {
	d.alerts.NotifyFailureAsync(ctx, domain.AlertEvent{
		UserID:       item.UserID,
		ResourceType: domain.AlertResourceBufferItem,
		ResourceID:   item.ID,
		BufferID:     item.BufferID,
		URL:          item.URL,
		Method:       item.Method,
		LastError:    errMsg,
		StatusCode:   statusCode,
		Attempts:     item.RetryCount + 1,
		FailedAt:     time.Now(),
	})
}

func (d *BufferDrainer) Start(ctx context.Context) {
	ticker := time.NewTicker(d.pollInterval)
	defer ticker.Stop()

	d.logger.InfoContext(ctx, "buffer drainer started", "concurrency", d.concurrency, "poll_interval", d.pollInterval)

	for {
		select {
		case <-ctx.Done():
			d.logger.InfoContext(ctx, "buffer drainer shut down")
			return
		case <-ticker.C:
			d.drain(ctx)
		}
	}
}

func (d *BufferDrainer) drain(ctx context.Context) {
	start := time.Now()
	defer func() {
		metrics.BufferDrainerCycleDuration.Observe(time.Since(start).Seconds())
	}()

	bufferIDs, err := d.repo.ListActiveBufferIDs(ctx, 100)
	if err != nil {
		d.logger.ErrorContext(ctx, "list active buffers", "error", err)
		return
	}

	for _, bufferID := range bufferIDs {
		d.sem <- struct{}{}
		go func(bid string) {
			defer func() { <-d.sem }()
			d.drainBuffer(ctx, bid)
		}(bufferID)
	}
}

func (d *BufferDrainer) drainBuffer(ctx context.Context, bufferID string) {
	buf, err := d.repo.GetByIDInternal(ctx, bufferID)
	if err != nil {
		d.logger.ErrorContext(ctx, "get buffer for draining", "buffer_id", bufferID, "error", err)
		return
	}

	// Drain strictly in order: one item at a time, each to a terminal state
	// before the next is claimed. ClaimNextItem returns nil when the head is not
	// yet due (in backoff) or nothing is pending — either way we stop and let the
	// next poll cycle resume. This preserves per-buffer (per-client) ordering.
	for {
		if ctx.Err() != nil {
			return
		}

		item, err := d.repo.ClaimNextItem(ctx, bufferID, d.id)
		if err != nil {
			d.logger.ErrorContext(ctx, "claim next buffer item", "buffer_id", bufferID, "error", err)
			return
		}
		if item == nil {
			return
		}

		metrics.BufferItemsInFlight.Inc()
		d.runItem(ctx, buf, item)
		metrics.BufferItemsInFlight.Dec()
	}
}

func (d *BufferDrainer) runItem(ctx context.Context, buf *domain.Buffer, item *domain.BufferItem) {
	// Credit gate
	ok, err := d.credits.HasCredits(ctx, item.UserID)
	if err != nil {
		d.logger.WarnContext(ctx, "credit check failed, proceeding anyway", "item_id", item.ID, "error", err)
	} else if !ok {
		errMsg := "insufficient credits"
		if failErr := d.repo.FailItem(ctx, item.ID, errMsg, nil); failErr != nil {
			d.logger.ErrorContext(ctx, "fail item (no credits)", "item_id", item.ID, "error", failErr)
		}
		metrics.BufferItemsCompletedTotal.WithLabelValues("failed").Inc()
		d.notifyBufferItem(ctx, buf, item, domain.BufferItemFailed, nil, &errMsg)
		d.alertItemFailure(ctx, item, nil, errMsg)
		return
	}

	heartbeatCtx, cancelHeartbeat := context.WithCancel(ctx)
	defer cancelHeartbeat()
	go d.heartbeat(heartbeatCtx, item.ID)

	var signingSecret string
	secret, secretErr := d.signingSecrets.GetActive(ctx, item.UserID)
	if secretErr != nil && !errors.Is(secretErr, domain.ErrSigningSecretNotFound) {
		d.logger.WarnContext(ctx, "fetch signing secret failed", "item_id", item.ID, "error", secretErr)
	} else if secretErr == nil {
		signingSecret = secret.Secret
	}

	// Build a temporary Job for the executor
	job := &domain.Job{
		ID:             item.ID,
		UserID:         item.UserID,
		URL:            item.URL,
		Method:         item.Method,
		Headers:        item.Headers,
		Body:           item.Body,
		TimeoutSeconds: item.TimeoutSeconds,
	}

	result := d.executor.Run(ctx, job, signingSecret)

	// Deduct credit
	crossing, deductErr := d.credits.DeductForBufferItem(ctx, item.UserID, item.ID, d.lowBalance.Threshold)
	if deductErr != nil {
		d.logger.WarnContext(ctx, "credit deduction failed", "item_id", item.ID, "error", deductErr)
	} else {
		fireCreditLow(ctx, d.alerts, d.lowBalance, item.UserID, crossing)
	}

	// 429 — rate limited by target API
	if result.StatusCode == http.StatusTooManyRequests {
		retryAfter := defaultRetryAfter
		if result.RetryAfter != nil {
			retryAfter = *result.RetryAfter
		}
		retryAt := time.Now().Add(retryAfter)
		errMsg := fmt.Sprintf("rate limited (429), retry after %s", retryAfter)
		statusCode := result.StatusCode
		if reschedErr := d.repo.RescheduleItem(ctx, item.ID, errMsg, &statusCode, retryAt); reschedErr != nil {
			d.logger.ErrorContext(ctx, "reschedule rate-limited item", "item_id", item.ID, "error", reschedErr)
		}
		metrics.BufferItemsCompletedTotal.WithLabelValues("rate_limited").Inc()
		d.logger.InfoContext(ctx, "buffer item rate-limited, rescheduled",
			"item_id", item.ID, "buffer_id", item.BufferID, "retry_at", retryAt)
		return
	}

	// Success
	if result.Err == nil && result.StatusCode >= 200 && result.StatusCode < 300 {
		if completeErr := d.repo.CompleteItem(ctx, item.ID, result.StatusCode); completeErr != nil {
			d.logger.ErrorContext(ctx, "complete buffer item", "item_id", item.ID, "error", completeErr)
		}
		metrics.BufferItemsCompletedTotal.WithLabelValues("success").Inc()
		d.notifyBufferItem(ctx, buf, item, domain.BufferItemCompleted, &result.StatusCode, nil)
		d.logger.InfoContext(ctx, "buffer item completed", "item_id", item.ID, "duration", result.Duration)
		return
	}

	// Failure
	errMsg := ""
	if result.Err != nil {
		errMsg = result.Err.Error()
	} else {
		errMsg = fmt.Sprintf("unexpected status code: %d", result.StatusCode)
	}

	var statusCode *int
	if result.StatusCode != 0 {
		statusCode = &result.StatusCode
	}

	if item.RetryCount < item.MaxRetries {
		retryAt := time.Now().Add(retryDelay(item.Backoff, item.RetryCount))
		if reschedErr := d.repo.RescheduleItem(ctx, item.ID, errMsg, statusCode, retryAt); reschedErr != nil {
			d.logger.ErrorContext(ctx, "reschedule buffer item", "item_id", item.ID, "error", reschedErr)
		}
		metrics.BufferItemsCompletedTotal.WithLabelValues("retry").Inc()
		d.logger.WarnContext(ctx, "buffer item failed, will retry",
			"item_id", item.ID, "error", errMsg,
			"attempt", item.RetryCount+1, "max_retries", item.MaxRetries)
	} else {
		if failErr := d.repo.FailItem(ctx, item.ID, errMsg, statusCode); failErr != nil {
			d.logger.ErrorContext(ctx, "fail buffer item", "item_id", item.ID, "error", failErr)
		}
		metrics.BufferItemsCompletedTotal.WithLabelValues("failed").Inc()
		d.notifyBufferItem(ctx, buf, item, domain.BufferItemFailed, statusCode, &errMsg)
		d.alertItemFailure(ctx, item, statusCode, errMsg)
		d.logger.WarnContext(ctx, "buffer item permanently failed", "item_id", item.ID, "error", errMsg)
	}
}

func (d *BufferDrainer) heartbeat(ctx context.Context, itemID string) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := d.repo.UpdateItemHeartbeat(ctx, itemID); err != nil {
				d.logger.WarnContext(ctx, "buffer item heartbeat failed", "item_id", itemID, "error", err)
			}
		}
	}
}

// notifyBufferItem sends a webhook if the buffer has one configured.
func (d *BufferDrainer) notifyBufferItem(ctx context.Context, buf *domain.Buffer, item *domain.BufferItem, status domain.BufferItemStatus, statusCode *int, lastError *string) {
	if buf.WebhookURL == nil {
		return
	}

	// Construct a temporary Job so we can reuse the existing WebhookNotifier.
	job := &domain.Job{
		ID:             item.ID,
		UserID:         item.UserID,
		Status:         domain.Status(status),
		WebhookURL:     buf.WebhookURL,
		WebhookHeaders: buf.WebhookHeaders,
		LastError:      lastError,
		RetryCount:     item.RetryCount,
	}
	now := time.Now()
	job.CompletedAt = &now

	d.notifier.notifyAsync(ctx, job, statusCode)
}
