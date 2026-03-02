package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type APITokenRepository struct {
	pool *pgxpool.Pool
}

func NewAPITokenRepository(pool *pgxpool.Pool) *APITokenRepository {
	return &APITokenRepository{pool: pool}
}

func (r *APITokenRepository) Create(ctx context.Context, userID, name, tokenHash, prefix string) (*domain.APIToken, error) {
	query := `
		INSERT INTO api_tokens (user_id, name, token_hash, prefix)
		VALUES ($1, $2, $3, $4)
		RETURNING id, user_id, name, prefix, last_used_at, created_at`

	row := r.pool.QueryRow(ctx, query, userID, name, tokenHash, prefix)
	tok, err := scanAPIToken(row)
	if err != nil {
		return nil, fmt.Errorf("create api token: %w", err)
	}
	return tok, nil
}

func (r *APITokenRepository) FindByTokenHash(ctx context.Context, tokenHash string) (*domain.APIToken, error) {
	query := `
		SELECT id, user_id, name, prefix, last_used_at, created_at
		FROM api_tokens
		WHERE token_hash = $1`

	row := r.pool.QueryRow(ctx, query, tokenHash)
	tok, err := scanAPIToken(row)
	if err != nil {
		return nil, err
	}
	return tok, nil
}

func (r *APITokenRepository) ListByUserID(ctx context.Context, userID string) ([]*domain.APIToken, error) {
	query := `
		SELECT id, user_id, name, prefix, last_used_at, created_at
		FROM api_tokens
		WHERE user_id = $1
		ORDER BY created_at DESC`

	rows, err := r.pool.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("list api tokens: %w", err)
	}
	defer rows.Close()

	var tokens []*domain.APIToken
	for rows.Next() {
		tok, err := scanAPIToken(rows)
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, tok)
	}
	return tokens, nil
}

func (r *APITokenRepository) Delete(ctx context.Context, id, userID string) error {
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM api_tokens WHERE id = $1 AND user_id = $2`,
		id, userID)
	if err != nil {
		return fmt.Errorf("delete api token: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrTokenNotFound
	}
	return nil
}

func (r *APITokenRepository) UpdateLastUsed(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE api_tokens SET last_used_at = NOW() WHERE id = $1`,
		id)
	return err
}

func scanAPIToken(row rowScanner) (*domain.APIToken, error) {
	var tok domain.APIToken
	err := row.Scan(&tok.ID, &tok.UserID, &tok.Name, &tok.Prefix, &tok.LastUsedAt, &tok.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrTokenNotFound
		}
		return nil, fmt.Errorf("scan api token: %w", err)
	}
	return &tok, nil
}
