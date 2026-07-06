package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// querier is the subset of the pgx API shared by *pgxpool.Pool and pgx.Tx, so a
// query helper can run either directly against the pool or inside a transaction.
// This is what lets the job terminal transition and its webhook-delivery insert
// commit atomically (the outbox pattern) — see JobRepository.CompleteWithWebhook.
type querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}
