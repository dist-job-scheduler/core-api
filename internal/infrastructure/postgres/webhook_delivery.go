package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type WebhookDeliveryRepository struct {
	pool *pgxpool.Pool
}

func NewWebhookDeliveryRepository(pool *pgxpool.Pool) *WebhookDeliveryRepository {
	return &WebhookDeliveryRepository{pool: pool}
}

// deliveryColumns is the canonical column list, shared by every SELECT/RETURNING
// so scanWebhookDelivery always sees the same order.
const deliveryColumns = `id, user_id, job_id, event, url, headers, payload,
	status, status_code, attempts, max_attempts, next_retry_at, last_error,
	delivered_at, created_at, updated_at`

func (r *WebhookDeliveryRepository) Enqueue(ctx context.Context, d *domain.WebhookDelivery) (*domain.WebhookDelivery, error) {
	headers := d.Headers
	if headers == nil {
		headers = map[string]string{}
	}
	maxAttempts := d.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 10
	}

	query := `
		INSERT INTO webhook_deliveries (user_id, job_id, event, url, headers, payload, max_attempts)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING ` + deliveryColumns

	row := r.pool.QueryRow(ctx, query,
		d.UserID, d.JobID, string(d.Event), d.URL, headers, string(d.Payload), maxAttempts)
	return scanWebhookDelivery(row)
}

// ClaimDue moves up to `limit` ready deliveries into 'delivering' and returns them.
// FOR UPDATE SKIP LOCKED makes concurrent dispatchers safe; the OR clause reclaims
// in-flight rows abandoned by a crashed dispatcher so delivery stays at-least-once.
func (r *WebhookDeliveryRepository) ClaimDue(ctx context.Context, limit int, inflightTimeout time.Duration) ([]*domain.WebhookDelivery, error) {
	query := `
		UPDATE webhook_deliveries
		SET status = 'delivering', updated_at = NOW()
		WHERE id IN (
			SELECT id FROM webhook_deliveries
			WHERE (status = 'pending' AND next_retry_at <= NOW())
			   OR (status = 'delivering' AND updated_at < NOW() - make_interval(secs => $2))
			ORDER BY next_retry_at
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING ` + deliveryColumns

	rows, err := r.pool.Query(ctx, query, limit, inflightTimeout.Seconds())
	if err != nil {
		return nil, fmt.Errorf("claim due webhook deliveries: %w", err)
	}
	defer rows.Close()

	var out []*domain.WebhookDelivery
	for rows.Next() {
		d, err := scanWebhookDelivery(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate claimed deliveries: %w", err)
	}
	return out, nil
}

func (r *WebhookDeliveryRepository) MarkDelivered(ctx context.Context, id string, statusCode int) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE webhook_deliveries
		SET status = 'delivered', status_code = $2, attempts = attempts + 1,
		    delivered_at = NOW(), last_error = NULL, updated_at = NOW()
		WHERE id = $1`, id, statusCode)
	if err != nil {
		return fmt.Errorf("mark delivery delivered: %w", err)
	}
	return nil
}

func (r *WebhookDeliveryRepository) Reschedule(ctx context.Context, id string, statusCode *int, lastErr string, nextRetryAt time.Time) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE webhook_deliveries
		SET status = 'pending', status_code = $2, attempts = attempts + 1,
		    last_error = $3, next_retry_at = $4, updated_at = NOW()
		WHERE id = $1`, id, statusCode, lastErr, nextRetryAt)
	if err != nil {
		return fmt.Errorf("reschedule delivery: %w", err)
	}
	return nil
}

func (r *WebhookDeliveryRepository) MarkFailed(ctx context.Context, id string, statusCode *int, lastErr string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE webhook_deliveries
		SET status = 'failed', status_code = $2, attempts = attempts + 1,
		    last_error = $3, updated_at = NOW()
		WHERE id = $1`, id, statusCode, lastErr)
	if err != nil {
		return fmt.Errorf("mark delivery failed: %w", err)
	}
	return nil
}

func (r *WebhookDeliveryRepository) ListByUser(ctx context.Context, userID string, limit int, cursorTime *time.Time, cursorID string) ([]*domain.WebhookDelivery, error) {
	args := []any{userID}
	where := "user_id = $1"
	if cursorTime != nil {
		args = append(args, *cursorTime, cursorID)
		where += fmt.Sprintf(" AND (created_at, id) < ($%d, $%d)", len(args)-1, len(args))
	}
	args = append(args, limit)

	query := fmt.Sprintf(`
		SELECT %s FROM webhook_deliveries
		WHERE %s
		ORDER BY created_at DESC, id DESC
		LIMIT $%d`, deliveryColumns, where, len(args))

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list webhook deliveries: %w", err)
	}
	defer rows.Close()

	var out []*domain.WebhookDelivery
	for rows.Next() {
		d, err := scanWebhookDelivery(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate webhook deliveries: %w", err)
	}
	return out, nil
}

func scanWebhookDelivery(row rowScanner) (*domain.WebhookDelivery, error) {
	var d domain.WebhookDelivery
	var payload string
	err := row.Scan(
		&d.ID, &d.UserID, &d.JobID, &d.Event, &d.URL, &d.Headers, &payload,
		&d.Status, &d.StatusCode, &d.Attempts, &d.MaxAttempts, &d.NextRetryAt,
		&d.LastError, &d.DeliveredAt, &d.CreatedAt, &d.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrWebhookDeliveryNotFound
		}
		return nil, fmt.Errorf("scan webhook delivery: %w", err)
	}
	d.Payload = []byte(payload)
	return &d, nil
}
