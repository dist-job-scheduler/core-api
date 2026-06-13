package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AttemptPartitionRepository manages job_attempts' monthly partitions.
type AttemptPartitionRepository struct {
	pool *pgxpool.Pool
}

func NewAttemptPartitionRepository(pool *pgxpool.Pool) *AttemptPartitionRepository {
	return &AttemptPartitionRepository{pool: pool}
}

func monthStart(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func partitionName(m time.Time) string { return "job_attempts_" + m.Format("2006_01") }

func (r *AttemptPartitionRepository) EnsureMonthlyPartitions(ctx context.Context, now time.Time, monthsAhead int) ([]string, error) {
	var created []string
	start := monthStart(now)
	for i := 0; i <= monthsAhead; i++ {
		m := start.AddDate(0, i, 0)
		name := partitionName(m)

		var exists bool
		if err := r.pool.QueryRow(ctx,
			`SELECT to_regclass('public.' || $1) IS NOT NULL`, name).Scan(&exists); err != nil {
			return created, fmt.Errorf("check partition %s: %w", name, err)
		}
		if exists {
			continue
		}

		next := m.AddDate(0, 1, 0)
		// Identifiers/dates are derived from a time value, not user input. DDL
		// can't be parameterized, so build the statement from sanitized parts.
		ddl := fmt.Sprintf(
			`CREATE TABLE IF NOT EXISTS %s PARTITION OF job_attempts FOR VALUES FROM ('%s') TO ('%s') `+
				`WITH (fillfactor=85, autovacuum_vacuum_scale_factor=0.05, `+
				`autovacuum_vacuum_insert_scale_factor=0.1, autovacuum_analyze_scale_factor=0.05)`,
			pgx.Identifier{name}.Sanitize(), m.Format("2006-01-02"), next.Format("2006-01-02"))
		if _, err := r.pool.Exec(ctx, ddl); err != nil {
			return created, fmt.Errorf("create partition %s: %w", name, err)
		}
		created = append(created, name)
	}
	return created, nil
}

func (r *AttemptPartitionRepository) DropPartitionsBefore(ctx context.Context, cutoff time.Time) ([]string, error) {
	cutoffMonth := monthStart(cutoff)

	rows, err := r.pool.Query(ctx, `
		SELECT c.relname
		FROM pg_inherits i
		JOIN pg_class c ON c.oid = i.inhrelid
		JOIN pg_class p ON p.oid = i.inhparent
		WHERE p.relname = 'job_attempts'
		  AND c.relname ~ '^job_attempts_[0-9]{4}_[0-9]{2}$'`)
	if err != nil {
		return nil, fmt.Errorf("list partitions: %w", err)
	}
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan partition name: %w", err)
		}
		names = append(names, n)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate partitions: %w", err)
	}

	var dropped []string
	for _, name := range names {
		m, perr := time.Parse("2006_01", name[len("job_attempts_"):])
		if perr != nil {
			continue // not a monthly partition we manage
		}
		if !m.UTC().Before(cutoffMonth) {
			continue // keep current/future months
		}
		if _, err := r.pool.Exec(ctx, fmt.Sprintf(`DROP TABLE IF EXISTS %s`, pgx.Identifier{name}.Sanitize())); err != nil {
			return dropped, fmt.Errorf("drop partition %s: %w", name, err)
		}
		dropped = append(dropped, name)
	}
	return dropped, nil
}
