package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/repository"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type BufferRepository struct {
	pool *pgxpool.Pool
}

func NewBufferRepository(pool *pgxpool.Pool) *BufferRepository {
	return &BufferRepository{pool: pool}
}

// ── Buffer CRUD ──────────────────────────────────────────────────────────────

func (r *BufferRepository) Create(ctx context.Context, b *domain.Buffer) (*domain.Buffer, error) {
	query := `
		INSERT INTO buffers (
			user_id, name, url, method, headers, timeout_seconds,
			rate_limit, max_retries, backoff, paused, webhook_url, webhook_headers
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING id, user_id, name, url, method, headers, timeout_seconds,
		          rate_limit, max_retries, backoff, paused,
		          webhook_url, webhook_headers, created_at, updated_at`

	row := r.pool.QueryRow(ctx, query,
		b.UserID, b.Name, b.URL, b.Method, b.Headers, b.TimeoutSeconds,
		b.RateLimit, b.MaxRetries, b.Backoff, b.Paused, b.WebhookURL, b.WebhookHeaders,
	)

	created, err := scanBuffer(row)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, domain.ErrBufferNameConflict
		}
		return nil, err
	}
	return created, nil
}

func (r *BufferRepository) GetByID(ctx context.Context, id, userID string) (*domain.Buffer, error) {
	query := `
		SELECT id, user_id, name, url, method, headers, timeout_seconds,
		       rate_limit, max_retries, backoff, paused,
		       webhook_url, webhook_headers, created_at, updated_at
		FROM buffers
		WHERE id = $1 AND user_id = $2`

	row := r.pool.QueryRow(ctx, query, id, userID)
	return scanBuffer(row)
}

func (r *BufferRepository) GetByIDInternal(ctx context.Context, id string) (*domain.Buffer, error) {
	query := `
		SELECT id, user_id, name, url, method, headers, timeout_seconds,
		       rate_limit, max_retries, backoff, paused,
		       webhook_url, webhook_headers, created_at, updated_at
		FROM buffers
		WHERE id = $1`

	row := r.pool.QueryRow(ctx, query, id)
	return scanBuffer(row)
}

func (r *BufferRepository) List(ctx context.Context, input repository.ListBuffersInput) ([]*domain.Buffer, error) {
	args := []any{input.UserID}
	where := []string{"user_id = $1"}

	if input.CursorTime != nil {
		args = append(args, *input.CursorTime, input.CursorID)
		where = append(where, fmt.Sprintf("(created_at, id) < ($%d, $%d)", len(args)-1, len(args)))
	}
	args = append(args, input.Limit)

	query := fmt.Sprintf(`
		SELECT id, user_id, name, url, method, headers, timeout_seconds,
		       rate_limit, max_retries, backoff, paused,
		       webhook_url, webhook_headers, created_at, updated_at
		FROM buffers
		WHERE %s
		ORDER BY created_at DESC, id DESC
		LIMIT $%d`,
		strings.Join(where, " AND "), len(args))

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list buffers: %w", err)
	}
	defer rows.Close()

	var buffers []*domain.Buffer
	for rows.Next() {
		b, scanErr := scanBuffer(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		buffers = append(buffers, b)
	}
	return buffers, nil
}

func (r *BufferRepository) SetPaused(ctx context.Context, id, userID string, paused bool) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE buffers SET paused = $3, updated_at = NOW()
		 WHERE id = $1 AND user_id = $2 AND paused = $4`,
		id, userID, paused, !paused)
	if err != nil {
		return fmt.Errorf("set paused: %w", err)
	}
	if tag.RowsAffected() == 0 {
		if _, err := r.GetByID(ctx, id, userID); err != nil {
			return err
		}
		if paused {
			return domain.ErrBufferAlreadyPaused
		}
		return domain.ErrBufferNotPaused
	}
	return nil
}

func (r *BufferRepository) Delete(ctx context.Context, id, userID string) error {
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM buffers WHERE id = $1 AND user_id = $2`,
		id, userID)
	if err != nil {
		return fmt.Errorf("delete buffer: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrBufferNotFound
	}
	return nil
}

// ── Item CRUD ────────────────────────────────────────────────────────────────

func (r *BufferRepository) CreateItem(ctx context.Context, item *domain.BufferItem) (*domain.BufferItem, error) {
	query := `
		INSERT INTO buffer_items (
			buffer_id, user_id, url, method, headers, body,
			timeout_seconds, backoff, status, max_retries, scheduled_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id, buffer_id, user_id, url, method, headers, body,
		          timeout_seconds, backoff, status, retry_count, max_retries,
		          scheduled_at, claimed_at, claimed_by, heartbeat_at,
		          completed_at, last_error, status_code, created_at, updated_at`

	row := r.pool.QueryRow(ctx, query,
		item.BufferID, item.UserID, item.URL, item.Method, item.Headers, item.Body,
		item.TimeoutSeconds, item.Backoff, item.Status, item.MaxRetries, item.ScheduledAt,
	)

	return scanBufferItem(row)
}

func (r *BufferRepository) GetItemByID(ctx context.Context, itemID, bufferID string) (*domain.BufferItem, error) {
	query := `
		SELECT id, buffer_id, user_id, url, method, headers, body,
		       timeout_seconds, backoff, status, retry_count, max_retries,
		       scheduled_at, claimed_at, claimed_by, heartbeat_at,
		       completed_at, last_error, status_code, created_at, updated_at
		FROM buffer_items
		WHERE id = $1 AND buffer_id = $2`

	row := r.pool.QueryRow(ctx, query, itemID, bufferID)
	return scanBufferItem(row)
}

func (r *BufferRepository) ListItems(ctx context.Context, input repository.ListBufferItemsInput) ([]*domain.BufferItem, error) {
	args := []any{input.BufferID}
	where := []string{"buffer_id = $1"}

	if input.Status != "" {
		args = append(args, input.Status)
		where = append(where, fmt.Sprintf("status = $%d", len(args)))
	}
	if input.CursorTime != nil {
		args = append(args, *input.CursorTime, input.CursorID)
		where = append(where, fmt.Sprintf("(created_at, id) < ($%d, $%d)", len(args)-1, len(args)))
	}
	args = append(args, input.Limit)

	query := fmt.Sprintf(`
		SELECT id, buffer_id, user_id, url, method, headers, body,
		       timeout_seconds, backoff, status, retry_count, max_retries,
		       scheduled_at, claimed_at, claimed_by, heartbeat_at,
		       completed_at, last_error, status_code, created_at, updated_at
		FROM buffer_items
		WHERE %s
		ORDER BY created_at DESC, id DESC
		LIMIT $%d`,
		strings.Join(where, " AND "), len(args))

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list buffer items: %w", err)
	}
	defer rows.Close()

	var items []*domain.BufferItem
	for rows.Next() {
		item, scanErr := scanBufferItem(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, nil
}

// ── Drainer-facing ───────────────────────────────────────────────────────────

func (r *BufferRepository) ListActiveBufferIDs(ctx context.Context, limit int) ([]string, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT DISTINCT b.id
		FROM buffers b
		WHERE NOT b.paused
		  AND EXISTS (
			SELECT 1 FROM buffer_items bi
			WHERE bi.buffer_id = b.id
			  AND bi.status = 'pending'
			  AND bi.scheduled_at <= NOW()
		  )
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list active buffer ids: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan buffer id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (r *BufferRepository) ClaimNextItem(ctx context.Context, bufferID, workerID string) (*domain.BufferItem, error) {
	// The subquery picks the buffer's OLDEST pending item (head of the queue),
	// regardless of whether it is due, but only while nothing is running for
	// this buffer. The outer `scheduled_at <= NOW()` then claims it only if the
	// head is actually due — so a head still in backoff blocks its successors
	// (strict ordering) rather than letting a later item overtake it.
	query := `
		UPDATE buffer_items
		SET    status       = 'running',
		       claimed_at   = NOW(),
		       claimed_by   = $1,
		       heartbeat_at = NOW(),
		       updated_at   = NOW()
		WHERE id = (
			SELECT id FROM buffer_items
			WHERE  buffer_id = $2
			  AND  status    = 'pending'
			  AND  NOT EXISTS (
				SELECT 1 FROM buffer_items running
				WHERE  running.buffer_id = $2
				  AND  running.status    = 'running'
			  )
			ORDER BY created_at ASC
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		  AND scheduled_at <= NOW()
		RETURNING id, buffer_id, user_id, url, method, headers, body,
		          timeout_seconds, backoff, status, retry_count, max_retries,
		          scheduled_at, claimed_at, claimed_by, heartbeat_at,
		          completed_at, last_error, status_code, created_at, updated_at`

	item, err := scanBufferItem(r.pool.QueryRow(ctx, query, workerID, bufferID))
	if err != nil {
		if errors.Is(err, domain.ErrBufferItemNotFound) {
			return nil, nil // nothing claimable right now
		}
		return nil, fmt.Errorf("claim next buffer item: %w", err)
	}
	return item, nil
}

func (r *BufferRepository) UpdateItemHeartbeat(ctx context.Context, itemID string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE buffer_items SET heartbeat_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND status = 'running'`, itemID)
	return err
}

func (r *BufferRepository) CompleteItem(ctx context.Context, itemID string, statusCode int) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE buffer_items SET status = 'completed', status_code = $2, completed_at = NOW(), updated_at = NOW()
		WHERE id = $1`, itemID, statusCode)
	return err
}

func (r *BufferRepository) FailItem(ctx context.Context, itemID string, lastError string, statusCode *int) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE buffer_items SET status = 'failed', last_error = $2, status_code = $3, completed_at = NOW(), updated_at = NOW()
		WHERE id = $1`, itemID, lastError, statusCode)
	return err
}

func (r *BufferRepository) RescheduleItem(ctx context.Context, itemID string, lastError string, statusCode *int, retryAt time.Time) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE buffer_items
		SET    status       = 'pending',
		       retry_count  = retry_count + 1,
		       last_error   = $2,
		       status_code  = $3,
		       scheduled_at = $4,
		       claimed_at   = NULL,
		       claimed_by   = NULL,
		       heartbeat_at = NULL,
		       updated_at   = NOW()
		WHERE id = $1`, itemID, lastError, statusCode, retryAt)
	return err
}

// ── Reaper methods ───────────────────────────────────────────────────────────

func (r *BufferRepository) RescheduleStaleItems(ctx context.Context, staleCutoff time.Time, limit int) (int, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE buffer_items
		SET    status       = 'pending',
		       retry_count  = retry_count + 1,
		       last_error   = 'worker timeout',
		       claimed_at   = NULL,
		       claimed_by   = NULL,
		       heartbeat_at = NULL,
		       updated_at   = NOW()
		WHERE id IN (
			SELECT id FROM buffer_items
			WHERE  status       = 'running'
			  AND  heartbeat_at < $1
			  AND  retry_count  < max_retries
			ORDER BY heartbeat_at ASC
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)`, staleCutoff, limit)
	return int(tag.RowsAffected()), err
}

func (r *BufferRepository) FailStaleItems(ctx context.Context, staleCutoff time.Time, limit int) (int, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE buffer_items
		SET    status      = 'failed',
		       last_error  = 'worker timeout: max retries exceeded',
		       completed_at = NOW(),
		       updated_at  = NOW()
		WHERE id IN (
			SELECT id FROM buffer_items
			WHERE  status       = 'running'
			  AND  heartbeat_at < $1
			  AND  retry_count  >= max_retries
			ORDER BY heartbeat_at ASC
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)`, staleCutoff, limit)
	return int(tag.RowsAffected()), err
}

// ── Scan helpers ─────────────────────────────────────────────────────────────

func scanBuffer(row rowScanner) (*domain.Buffer, error) {
	var b domain.Buffer
	err := row.Scan(
		&b.ID, &b.UserID, &b.Name, &b.URL, &b.Method, &b.Headers, &b.TimeoutSeconds,
		&b.RateLimit, &b.MaxRetries, &b.Backoff, &b.Paused,
		&b.WebhookURL, &b.WebhookHeaders, &b.CreatedAt, &b.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrBufferNotFound
		}
		return nil, fmt.Errorf("scan buffer: %w", err)
	}
	return &b, nil
}

func scanBufferItem(row rowScanner) (*domain.BufferItem, error) {
	var item domain.BufferItem
	err := row.Scan(
		&item.ID, &item.BufferID, &item.UserID, &item.URL, &item.Method, &item.Headers, &item.Body,
		&item.TimeoutSeconds, &item.Backoff, &item.Status, &item.RetryCount, &item.MaxRetries,
		&item.ScheduledAt, &item.ClaimedAt, &item.ClaimedBy, &item.HeartbeatAt,
		&item.CompletedAt, &item.LastError, &item.StatusCode, &item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrBufferItemNotFound
		}
		return nil, fmt.Errorf("scan buffer item: %w", err)
	}
	return &item, nil
}
