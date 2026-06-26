package usecase_test

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/repository"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/testutil"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/usecase"
)

// These tests guard against an off-by-one in keyset pagination: the next-page
// cursor must be the last row *returned to the caller*, not the extra "sentinel"
// row fetched to detect a next page. Because every repo applies a strict
// `(ts, id) < (cursor)` filter, pointing the cursor at the sentinel excludes
// that row from the next page too — silently dropping exactly one record at
// every page boundary. Each test walks all pages and asserts every record is
// seen exactly once, so it fails on the buggy `[limit]` and passes on `[limit-1]`.

type keyRow struct {
	id string
	ts time.Time
}

// keysetPage mimics a Postgres keyset query: rows ordered by (ts, id) DESC,
// filtered strictly below the cursor, capped at limit.
func keysetPage(all []keyRow, cursorTime *time.Time, cursorID string, limit int) []keyRow {
	sorted := append([]keyRow(nil), all...)
	sort.Slice(sorted, func(i, j int) bool {
		if !sorted[i].ts.Equal(sorted[j].ts) {
			return sorted[i].ts.After(sorted[j].ts)
		}
		return sorted[i].id > sorted[j].id
	})
	out := make([]keyRow, 0, limit)
	for _, r := range sorted {
		if cursorTime != nil {
			// strict: (ts, id) < (cursorTime, cursorID)
			if r.ts.After(*cursorTime) || (r.ts.Equal(*cursorTime) && r.id >= cursorID) {
				continue
			}
		}
		out = append(out, r)
		if len(out) == limit {
			break
		}
	}
	return out
}

func TestListJobs_PaginationWalksEveryRow(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	const total = 7
	store := make([]keyRow, total)
	for i := 0; i < total; i++ {
		store[i] = keyRow{id: string(rune('a' + i)), ts: base.Add(time.Duration(i) * time.Minute)}
	}

	jobRepo := &testutil.MockJobRepository{
		ListJobsFn: func(_ context.Context, in repository.ListJobsInput) ([]*domain.Job, error) {
			page := keysetPage(store, in.CursorTime, in.CursorID, in.Limit)
			jobs := make([]*domain.Job, len(page))
			for i, r := range page {
				jobs[i] = &domain.Job{ID: r.id, ScheduledAt: r.ts}
			}
			return jobs, nil
		},
	}
	uc := usecase.NewJobUsecase(jobRepo, &testutil.MockAttemptRepository{}, &testutil.MockCreditRepository{})

	seen := map[string]int{}
	cursor := ""
	pages := 0
	for {
		res, err := uc.ListJobs(context.Background(), usecase.ListJobsInput{UserID: "u", Limit: 2, Cursor: cursor})
		if err != nil {
			t.Fatalf("page %d: unexpected error: %v", pages, err)
		}
		for _, j := range res.Jobs {
			seen[j.ID]++
		}
		pages++
		if pages > total+2 {
			t.Fatalf("pagination did not terminate")
		}
		if res.NextCursor == nil {
			break
		}
		cursor = *res.NextCursor
	}

	if len(seen) != total {
		t.Fatalf("expected %d distinct jobs across all pages, saw %d: %v", total, len(seen), seen)
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("job %s returned %d times, want exactly 1", id, n)
		}
	}
}

func TestListSchedules_PaginationWalksEveryRow(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	const total = 7
	store := make([]keyRow, total)
	for i := 0; i < total; i++ {
		store[i] = keyRow{id: string(rune('a' + i)), ts: base.Add(time.Duration(i) * time.Minute)}
	}

	schedRepo := &testutil.MockScheduleRepository{
		ListFn: func(_ context.Context, in repository.ListSchedulesInput) ([]*domain.Schedule, error) {
			page := keysetPage(store, in.CursorTime, in.CursorID, in.Limit)
			out := make([]*domain.Schedule, len(page))
			for i, r := range page {
				out[i] = &domain.Schedule{ID: r.id, CreatedAt: r.ts}
			}
			return out, nil
		},
	}
	uc := usecase.NewScheduleUsecase(schedRepo, &testutil.MockJobRepository{})

	seen := map[string]int{}
	cursor := ""
	pages := 0
	for {
		res, err := uc.ListSchedules(context.Background(), usecase.ListSchedulesInput{UserID: "u", Limit: 2, Cursor: cursor})
		if err != nil {
			t.Fatalf("page %d: unexpected error: %v", pages, err)
		}
		for _, s := range res.Schedules {
			seen[s.ID]++
		}
		pages++
		if pages > total+2 {
			t.Fatalf("pagination did not terminate")
		}
		if res.NextCursor == nil {
			break
		}
		cursor = *res.NextCursor
	}

	if len(seen) != total {
		t.Fatalf("expected %d distinct schedules across all pages, saw %d: %v", total, len(seen), seen)
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("schedule %s returned %d times, want exactly 1", id, n)
		}
	}
}
