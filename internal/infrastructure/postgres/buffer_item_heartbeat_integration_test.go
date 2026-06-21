//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/infrastructure/postgres"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/testutil"
	"github.com/jackc/pgx/v5/pgxpool"
)

func setupBufferRepoPool(t *testing.T) (*postgres.BufferRepository, *pgxpool.Pool, string) {
	t.Helper()
	pool := testutil.SetupTestDB(t)
	testutil.TruncateAll(t, pool)
	userID := testutil.UserID()
	testutil.SeedUser(t, pool, userID)
	return postgres.NewBufferRepository(pool), pool, userID
}

func bufHeartbeatRow(t *testing.T, pool *pgxpool.Pool, itemID string) (time.Time, bool) {
	t.Helper()
	var hb time.Time
	if err := pool.QueryRow(context.Background(),
		`SELECT heartbeat_at FROM buffer_item_heartbeats WHERE buffer_item_id = $1`, itemID).Scan(&hb); err != nil {
		return time.Time{}, false
	}
	return hb, true
}

// claimBufferItem creates a buffer with tokens available + one pending item, then
// claims it so it is 'running'.
func claimBufferItem(t *testing.T, repo *postgres.BufferRepository, pool *pgxpool.Pool, userID string) *domain.BufferItem {
	t.Helper()
	ctx := context.Background()
	b := newTestBuffer(t, repo, userID)
	// Give the token bucket a full balance so the claim isn't gated by refill timing.
	if _, err := pool.Exec(ctx, `UPDATE buffers SET tokens = 1000, last_refill_at = NOW() WHERE id = $1`, b.ID); err != nil {
		t.Fatalf("fund token bucket: %v", err)
	}
	newTestItem(t, repo, b, domain.BufferItemPending)
	item, err := repo.ClaimNextItem(ctx, b.ID, "worker-1")
	if err != nil || item == nil {
		t.Fatalf("claim next item: err=%v item=%v", err, item)
	}
	if item.Status != domain.BufferItemRunning {
		t.Fatalf("status = %s, want running", item.Status)
	}
	return item
}

func TestBufferHeartbeat_ClaimSeedsSidecar(t *testing.T) {
	repo, pool, userID := setupBufferRepoPool(t)
	item := claimBufferItem(t, repo, pool, userID)
	if _, ok := bufHeartbeatRow(t, pool, item.ID); !ok {
		t.Fatal("expected a buffer_item_heartbeats row after claim")
	}
}

func TestBufferHeartbeat_UpdateWritesSidecarOnly(t *testing.T) {
	repo, pool, userID := setupBufferRepoPool(t)
	ctx := context.Background()
	item := claimBufferItem(t, repo, pool, userID)

	before, ok := bufHeartbeatRow(t, pool, item.ID)
	if !ok {
		t.Fatal("no sidecar row after claim")
	}
	var rowHBBefore time.Time
	if err := pool.QueryRow(ctx, `SELECT heartbeat_at FROM buffer_items WHERE id=$1`, item.ID).Scan(&rowHBBefore); err != nil {
		t.Fatalf("read buffer_items.heartbeat_at: %v", err)
	}

	time.Sleep(10 * time.Millisecond)
	if err := repo.UpdateItemHeartbeat(ctx, item.ID); err != nil {
		t.Fatalf("update heartbeat: %v", err)
	}

	after, _ := bufHeartbeatRow(t, pool, item.ID)
	if !after.After(before) {
		t.Fatalf("sidecar heartbeat did not advance: before=%v after=%v", before, after)
	}
	var rowHBAfter time.Time
	if err := pool.QueryRow(ctx, `SELECT heartbeat_at FROM buffer_items WHERE id=$1`, item.ID).Scan(&rowHBAfter); err != nil {
		t.Fatalf("read buffer_items.heartbeat_at: %v", err)
	}
	if !rowHBAfter.Equal(rowHBBefore) {
		t.Fatalf("buffer_items.heartbeat_at changed (%v -> %v) — heartbeat should only touch the sidecar", rowHBBefore, rowHBAfter)
	}
}

func TestBufferHeartbeat_TerminalTransitionsDropSidecar(t *testing.T) {
	for _, tc := range []struct {
		name string
		act  func(repo *postgres.BufferRepository, id string) error
	}{
		{"complete", func(r *postgres.BufferRepository, id string) error { return r.CompleteItem(context.Background(), id, 200) }},
		{"fail", func(r *postgres.BufferRepository, id string) error { return r.FailItem(context.Background(), id, "boom", nil) }},
		{"reschedule", func(r *postgres.BufferRepository, id string) error {
			return r.RescheduleItem(context.Background(), id, "boom", nil, time.Now().Add(time.Minute))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, pool, userID := setupBufferRepoPool(t)
			item := claimBufferItem(t, repo, pool, userID)
			if _, ok := bufHeartbeatRow(t, pool, item.ID); !ok {
				t.Fatal("expected sidecar row after claim")
			}
			if err := tc.act(repo, item.ID); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if _, ok := bufHeartbeatRow(t, pool, item.ID); ok {
				t.Fatalf("%s should have dropped the sidecar row", tc.name)
			}
		})
	}
}

func TestBufferHeartbeat_RescheduleStaleUsesSidecar(t *testing.T) {
	repo, pool, userID := setupBufferRepoPool(t)
	ctx := context.Background()
	item := claimBufferItem(t, repo, pool, userID)

	n, err := repo.RescheduleStaleItems(ctx, time.Now().Add(time.Hour), 100)
	if err != nil {
		t.Fatalf("reschedule stale items: %v", err)
	}
	if n != 1 {
		t.Fatalf("rescheduled %d, want 1", n)
	}
	if _, ok := bufHeartbeatRow(t, pool, item.ID); ok {
		t.Fatal("reschedule-stale should drop the sidecar row")
	}
}

func TestBufferHeartbeat_MissingSidecarRowIsStale(t *testing.T) {
	repo, pool, userID := setupBufferRepoPool(t)
	ctx := context.Background()
	item := claimBufferItem(t, repo, pool, userID)
	if _, err := pool.Exec(ctx, `DELETE FROM buffer_item_heartbeats WHERE buffer_item_id=$1`, item.ID); err != nil {
		t.Fatalf("delete sidecar: %v", err)
	}
	// Past cutoff: only the missing row makes it stale.
	n, err := repo.RescheduleStaleItems(ctx, time.Now().Add(-time.Hour), 100)
	if err != nil {
		t.Fatalf("reschedule stale items: %v", err)
	}
	if n != 1 {
		t.Fatalf("rescheduled %d, want 1 (missing heartbeat must be treated as stale)", n)
	}
}
