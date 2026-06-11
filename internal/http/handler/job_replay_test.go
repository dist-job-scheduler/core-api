package handler_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/http/handler"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/testutil"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/usecase"
	"github.com/gin-gonic/gin"
)

func setupReplayRouter(jobRepo *testutil.MockJobRepository, creditRepo *testutil.MockCreditRepository) *gin.Engine {
	uc := usecase.NewJobUsecase(jobRepo, &testutil.MockAttemptRepository{}, creditRepo)
	h := handler.NewJobHandler(uc, slog.Default())

	r := gin.New()
	g := r.Group("/", injectUser(testUserID))
	g.POST("/jobs/:id/replay", h.Replay)
	return r
}

func TestReplayJobHandler_HappyPath(t *testing.T) {
	failed := testutil.NewJob(testutil.WithUserID(testUserID), testutil.WithJobID("job-failed"), testutil.WithStatus(domain.StatusFailed))
	jobRepo := &testutil.MockJobRepository{
		GetByIDFn: func(_ context.Context, _, _ string) (*domain.Job, error) { return failed, nil },
		CreateFn: func(_ context.Context, j *domain.Job) (*domain.Job, error) {
			j.ID = "job-replay"
			return j, nil
		},
	}
	r := setupReplayRouter(jobRepo, &testutil.MockCreditRepository{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/jobs/job-failed/replay", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusCreated, w.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["id"] != "job-replay" {
		t.Errorf("id = %v, want job-replay", resp["id"])
	}
	if resp["replay_of"] != "job-failed" {
		t.Errorf("replay_of = %v, want job-failed", resp["replay_of"])
	}
	if resp["status"] != string(domain.StatusPending) {
		t.Errorf("status = %v, want pending", resp["status"])
	}
}

func TestReplayJobHandler_NotReplayable(t *testing.T) {
	running := testutil.NewJob(testutil.WithUserID(testUserID), testutil.WithStatus(domain.StatusRunning))
	jobRepo := &testutil.MockJobRepository{
		GetByIDFn: func(_ context.Context, _, _ string) (*domain.Job, error) { return running, nil },
	}
	r := setupReplayRouter(jobRepo, &testutil.MockCreditRepository{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/jobs/job-1/replay", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d", w.Code, http.StatusConflict)
	}
}

func TestReplayJobHandler_NotFound(t *testing.T) {
	jobRepo := &testutil.MockJobRepository{
		GetByIDFn: func(_ context.Context, _, _ string) (*domain.Job, error) { return nil, domain.ErrJobNotFound },
	}
	r := setupReplayRouter(jobRepo, &testutil.MockCreditRepository{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/jobs/missing/replay", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestReplayJobHandler_InsufficientCredits(t *testing.T) {
	failed := testutil.NewJob(testutil.WithUserID(testUserID), testutil.WithStatus(domain.StatusFailed))
	jobRepo := &testutil.MockJobRepository{
		GetByIDFn: func(_ context.Context, _, _ string) (*domain.Job, error) { return failed, nil },
	}
	credits := &testutil.MockCreditRepository{
		HasCreditsFn: func(_ context.Context, _ string) (bool, error) { return false, nil },
	}
	r := setupReplayRouter(jobRepo, credits)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/jobs/job-1/replay", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusPaymentRequired {
		t.Errorf("status = %d, want %d", w.Code, http.StatusPaymentRequired)
	}
}
