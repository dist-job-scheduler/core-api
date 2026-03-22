package handler_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/http/handler"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/repository"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/testutil"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/usecase"
	"github.com/gin-gonic/gin"
)

func setupScheduleRouter(schedRepo *testutil.MockScheduleRepository, jobRepo *testutil.MockJobRepository) *gin.Engine {
	uc := usecase.NewScheduleUsecase(schedRepo, jobRepo)
	h := handler.NewScheduleHandler(uc, slog.Default())

	r := gin.New()
	g := r.Group("/", injectUser(testUserID))
	g.POST("/schedules", h.Create)
	g.GET("/schedules", h.List)
	g.GET("/schedules/:id", h.GetByID)
	g.POST("/schedules/:id/pause", h.Pause)
	g.POST("/schedules/:id/resume", h.Resume)
	g.DELETE("/schedules/:id", h.Delete)
	g.GET("/schedules/:id/jobs", h.ListJobs)
	return r
}

func TestCreateSchedule_HappyPath(t *testing.T) {
	schedRepo := &testutil.MockScheduleRepository{
		CreateFn: func(_ context.Context, s *domain.Schedule) (*domain.Schedule, error) {
			s.ID = "sched-abc"
			return s, nil
		},
	}
	r := setupScheduleRouter(schedRepo, &testutil.MockJobRepository{})

	body := `{"name":"my-schedule","cron_expr":"*/5 * * * *","url":"https://example.com/hook"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/schedules", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["id"] != "sched-abc" {
		t.Errorf("id = %v, want %q", resp["id"], "sched-abc")
	}
	if resp["name"] != "my-schedule" {
		t.Errorf("name = %v, want %q", resp["name"], "my-schedule")
	}
}

func TestCreateSchedule_InvalidCron(t *testing.T) {
	r := setupScheduleRouter(&testutil.MockScheduleRepository{}, &testutil.MockJobRepository{})

	body := `{"name":"bad-cron","cron_expr":"not a cron","url":"https://example.com/hook"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/schedules", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d; body = %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestCreateSchedule_NameConflict(t *testing.T) {
	schedRepo := &testutil.MockScheduleRepository{
		CreateFn: func(_ context.Context, _ *domain.Schedule) (*domain.Schedule, error) {
			return nil, domain.ErrScheduleNameConflict
		},
	}
	r := setupScheduleRouter(schedRepo, &testutil.MockJobRepository{})

	body := `{"name":"dup-name","cron_expr":"*/5 * * * *","url":"https://example.com/hook"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/schedules", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d", w.Code, http.StatusConflict)
	}
}

func TestGetSchedule_HappyPath(t *testing.T) {
	sched := testutil.NewSchedule(testutil.WithScheduleUserID(testUserID), testutil.WithScheduleID("sched-1"))
	schedRepo := &testutil.MockScheduleRepository{
		GetByIDFn: func(_ context.Context, id, uid string) (*domain.Schedule, error) {
			if id == "sched-1" && uid == testUserID {
				return sched, nil
			}
			return nil, domain.ErrScheduleNotFound
		},
	}
	r := setupScheduleRouter(schedRepo, &testutil.MockJobRepository{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/schedules/sched-1", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["id"] != "sched-1" {
		t.Errorf("id = %v, want %q", resp["id"], "sched-1")
	}
}

func TestGetSchedule_NotFound(t *testing.T) {
	schedRepo := &testutil.MockScheduleRepository{
		GetByIDFn: func(_ context.Context, _, _ string) (*domain.Schedule, error) {
			return nil, domain.ErrScheduleNotFound
		},
	}
	r := setupScheduleRouter(schedRepo, &testutil.MockJobRepository{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/schedules/nonexistent", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestListSchedules_HappyPath(t *testing.T) {
	schedules := []*domain.Schedule{
		testutil.NewSchedule(testutil.WithScheduleUserID(testUserID), testutil.WithScheduleID("s-1")),
		testutil.NewSchedule(testutil.WithScheduleUserID(testUserID), testutil.WithScheduleID("s-2")),
	}
	schedRepo := &testutil.MockScheduleRepository{
		ListFn: func(_ context.Context, _ repository.ListSchedulesInput) ([]*domain.Schedule, error) {
			return schedules, nil
		},
	}
	r := setupScheduleRouter(schedRepo, &testutil.MockJobRepository{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/schedules", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	arr, ok := resp["schedules"].([]interface{})
	if !ok {
		t.Fatal("response missing schedules array")
	}
	if len(arr) != 2 {
		t.Errorf("len(schedules) = %d, want 2", len(arr))
	}
}

func TestPauseSchedule_HappyPath(t *testing.T) {
	schedRepo := &testutil.MockScheduleRepository{
		SetPausedFn: func(_ context.Context, _, _ string, paused bool) error {
			if !paused {
				t.Error("expected paused=true")
			}
			return nil
		},
	}
	r := setupScheduleRouter(schedRepo, &testutil.MockJobRepository{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/schedules/sched-1/pause", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
}

func TestPauseSchedule_AlreadyPaused(t *testing.T) {
	schedRepo := &testutil.MockScheduleRepository{
		SetPausedFn: func(_ context.Context, _, _ string, _ bool) error {
			return domain.ErrScheduleAlreadyPaused
		},
	}
	r := setupScheduleRouter(schedRepo, &testutil.MockJobRepository{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/schedules/sched-1/pause", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d", w.Code, http.StatusConflict)
	}
}

func TestResumeSchedule_HappyPath(t *testing.T) {
	schedRepo := &testutil.MockScheduleRepository{
		SetPausedFn: func(_ context.Context, _, _ string, paused bool) error {
			if paused {
				t.Error("expected paused=false")
			}
			return nil
		},
	}
	r := setupScheduleRouter(schedRepo, &testutil.MockJobRepository{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/schedules/sched-1/resume", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
}

func TestResumeSchedule_NotPaused(t *testing.T) {
	schedRepo := &testutil.MockScheduleRepository{
		SetPausedFn: func(_ context.Context, _, _ string, _ bool) error {
			return domain.ErrScheduleNotPaused
		},
	}
	r := setupScheduleRouter(schedRepo, &testutil.MockJobRepository{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/schedules/sched-1/resume", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d", w.Code, http.StatusConflict)
	}
}

func TestDeleteSchedule_HappyPath(t *testing.T) {
	schedRepo := &testutil.MockScheduleRepository{
		DeleteFn: func(_ context.Context, _, _ string) error { return nil },
	}
	r := setupScheduleRouter(schedRepo, &testutil.MockJobRepository{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/schedules/sched-1", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
}

func TestDeleteSchedule_NotFound(t *testing.T) {
	schedRepo := &testutil.MockScheduleRepository{
		DeleteFn: func(_ context.Context, _, _ string) error {
			return domain.ErrScheduleNotFound
		},
	}
	r := setupScheduleRouter(schedRepo, &testutil.MockJobRepository{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/schedules/nonexistent", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}
