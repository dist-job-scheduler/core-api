package postgres

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SigningSecretRepository struct {
	pool *pgxpool.Pool
}

func NewSigningSecretRepository(pool *pgxpool.Pool) *SigningSecretRepository {
	return &SigningSecretRepository{pool: pool}
}

func (r *SigningSecretRepository) GetActive(ctx context.Context, userID string) (*domain.SigningSecret, error) {
	query := `
		SELECT id, user_id, secret, is_active, created_at
		FROM user_signing_secrets
		WHERE user_id = $1 AND is_active = TRUE`

	row := r.pool.QueryRow(ctx, query, userID)
	s, err := scanSigningSecret(row)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (r *SigningSecretRepository) Create(ctx context.Context, userID string) (*domain.SigningSecret, error) {
	// Check if one already exists.
	existing, err := r.GetActive(ctx, userID)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, domain.ErrSigningSecretNotFound) {
		return nil, fmt.Errorf("check existing signing secret: %w", err)
	}

	secret, err := generateSigningSecret()
	if err != nil {
		return nil, fmt.Errorf("generate signing secret: %w", err)
	}

	query := `
		INSERT INTO user_signing_secrets (user_id, secret)
		VALUES ($1, $2)
		RETURNING id, user_id, secret, is_active, created_at`

	row := r.pool.QueryRow(ctx, query, userID, secret)
	s, err := scanSigningSecret(row)
	if err != nil {
		return nil, fmt.Errorf("create signing secret: %w", err)
	}
	return s, nil
}

func (r *SigningSecretRepository) Rotate(ctx context.Context, userID string) (*domain.SigningSecret, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Deactivate existing active secret.
	_, err = tx.Exec(ctx,
		`UPDATE user_signing_secrets SET is_active = FALSE WHERE user_id = $1 AND is_active = TRUE`,
		userID)
	if err != nil {
		return nil, fmt.Errorf("deactivate signing secret: %w", err)
	}

	secret, err := generateSigningSecret()
	if err != nil {
		return nil, fmt.Errorf("generate signing secret: %w", err)
	}

	query := `
		INSERT INTO user_signing_secrets (user_id, secret)
		VALUES ($1, $2)
		RETURNING id, user_id, secret, is_active, created_at`

	row := tx.QueryRow(ctx, query, userID, secret)
	s, err := scanSigningSecret(row)
	if err != nil {
		return nil, fmt.Errorf("create rotated signing secret: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return s, nil
}

func scanSigningSecret(row rowScanner) (*domain.SigningSecret, error) {
	var s domain.SigningSecret
	err := row.Scan(&s.ID, &s.UserID, &s.Secret, &s.IsActive, &s.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrSigningSecretNotFound
		}
		return nil, fmt.Errorf("scan signing secret: %w", err)
	}
	return &s, nil
}

// generateSigningSecret produces a whsec_ prefixed secret with 32 random bytes.
func generateSigningSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "whsec_" + base64.RawURLEncoding.EncodeToString(b), nil
}
