package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type AlertChannelRepository struct {
	pool *pgxpool.Pool
}

func NewAlertChannelRepository(pool *pgxpool.Pool) *AlertChannelRepository {
	return &AlertChannelRepository{pool: pool}
}

func (r *AlertChannelRepository) Create(ctx context.Context, ch *domain.AlertChannel) (*domain.AlertChannel, error) {
	query := `
		INSERT INTO alert_channels (user_id, type, target, name, enabled, verified)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, user_id, type, target, name, enabled, verified, created_at, updated_at`

	row := r.pool.QueryRow(ctx, query, ch.UserID, ch.Type, ch.Target, ch.Name, ch.Enabled, ch.Verified)
	return scanAlertChannel(row)
}

func (r *AlertChannelRepository) GetByID(ctx context.Context, id, userID string) (*domain.AlertChannel, error) {
	query := `
		SELECT id, user_id, type, target, name, enabled, verified, created_at, updated_at
		FROM alert_channels
		WHERE id = $1 AND user_id = $2`

	row := r.pool.QueryRow(ctx, query, id, userID)
	return scanAlertChannel(row)
}

func (r *AlertChannelRepository) List(ctx context.Context, userID string) ([]*domain.AlertChannel, error) {
	return r.list(ctx, `
		SELECT id, user_id, type, target, name, enabled, verified, created_at, updated_at
		FROM alert_channels
		WHERE user_id = $1
		ORDER BY created_at DESC, id DESC`, userID)
}

func (r *AlertChannelRepository) ListEnabled(ctx context.Context, userID string) ([]*domain.AlertChannel, error) {
	return r.list(ctx, `
		SELECT id, user_id, type, target, name, enabled, verified, created_at, updated_at
		FROM alert_channels
		WHERE user_id = $1 AND enabled
		ORDER BY created_at DESC, id DESC`, userID)
}

func (r *AlertChannelRepository) list(ctx context.Context, query, userID string) ([]*domain.AlertChannel, error) {
	rows, err := r.pool.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("list alert channels: %w", err)
	}
	defer rows.Close()

	var channels []*domain.AlertChannel
	for rows.Next() {
		ch, scanErr := scanAlertChannel(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		channels = append(channels, ch)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate alert channels: %w", err)
	}
	return channels, nil
}

func (r *AlertChannelRepository) SetEnabled(ctx context.Context, id, userID string, enabled bool) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE alert_channels SET enabled = $3, updated_at = NOW()
		 WHERE id = $1 AND user_id = $2`,
		id, userID, enabled)
	if err != nil {
		return fmt.Errorf("set alert channel enabled: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrAlertChannelNotFound
	}
	return nil
}

func (r *AlertChannelRepository) Delete(ctx context.Context, id, userID string) error {
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM alert_channels WHERE id = $1 AND user_id = $2`,
		id, userID)
	if err != nil {
		return fmt.Errorf("delete alert channel: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrAlertChannelNotFound
	}
	return nil
}

// MarkVerified flips an email channel to verified + enabled. Keyed by id only —
// the caller holds a signed confirm token, not a session. Idempotent.
func (r *AlertChannelRepository) MarkVerified(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE alert_channels SET verified = TRUE, enabled = TRUE, updated_at = NOW()
		 WHERE id = $1`,
		id)
	if err != nil {
		return fmt.Errorf("mark alert channel verified: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrAlertChannelNotFound
	}
	return nil
}

func scanAlertChannel(row rowScanner) (*domain.AlertChannel, error) {
	var ch domain.AlertChannel
	err := row.Scan(&ch.ID, &ch.UserID, &ch.Type, &ch.Target, &ch.Name, &ch.Enabled, &ch.Verified, &ch.CreatedAt, &ch.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrAlertChannelNotFound
		}
		return nil, fmt.Errorf("scan alert channel: %w", err)
	}
	return &ch, nil
}
