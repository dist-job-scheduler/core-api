package handler_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/http/handler"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/testutil"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/usecase"
	"github.com/gin-gonic/gin"
)

func setupStatsRouter(statsRepo *testutil.MockStatsRepository, creditRepo *testutil.MockCreditRepository) *gin.Engine {
	uc := usecase.NewStatsUsecase(statsRepo, creditRepo)
	h := handler.NewStatsHandler(uc, slog.Default())

	r := gin.New()
	g := r.Group("/", injectUser(testUserID))
	g.GET("/stats/jobs", h.Jobs)
	g.GET("/stats/usage", h.Usage)
	return r
}

func TestStatsJobs_HappyPath(t *testing.T) {
	statsRepo := &testutil.MockStatsRepository{
		JobStatsFn: func(_ context.Context, _ string, _ time.Time) (domain.JobStats, error) {
			return domain.JobStats{
				TotalExecutions: 20, Succeeded: 18, Failed: 2,
				SuccessRate: 0.9, AvgDurationMS: 120.5, P95DurationMS: 300,
			}, nil
		},
	}
	r := setupStatsRouter(statsRepo, &testutil.MockCreditRepository{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/stats/jobs?days=14", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusOK, w.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["total_executions"].(float64) != 20 {
		t.Errorf("total_executions = %v, want 20", resp["total_executions"])
	}
	if resp["success_rate"].(float64) != 0.9 {
		t.Errorf("success_rate = %v, want 0.9", resp["success_rate"])
	}
	if resp["since_days"].(float64) != 14 {
		t.Errorf("since_days = %v, want 14", resp["since_days"])
	}
}

func TestStatsJobs_InvalidDays(t *testing.T) {
	r := setupStatsRouter(&testutil.MockStatsRepository{}, &testutil.MockCreditRepository{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/stats/jobs?days=abc", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestStatsUsage_HappyPath(t *testing.T) {
	statsRepo := &testutil.MockStatsRepository{
		UsageSeriesFn: func(_ context.Context, _ string, _ time.Time) ([]domain.UsageBucket, error) {
			return []domain.UsageBucket{
				{Date: "2026-06-11", JobExecutions: 4, BufferExecutions: 6},
			}, nil
		},
	}
	r := setupStatsRouter(statsRepo, &testutil.MockCreditRepository{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/stats/usage", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusOK, w.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["total_job_executions"].(float64) != 4 {
		t.Errorf("total_job_executions = %v, want 4", resp["total_job_executions"])
	}
	if resp["total_buffer_executions"].(float64) != 6 {
		t.Errorf("total_buffer_executions = %v, want 6", resp["total_buffer_executions"])
	}
	buckets, ok := resp["buckets"].([]interface{})
	if !ok || len(buckets) != 1 {
		t.Errorf("buckets = %v, want 1 entry", resp["buckets"])
	}
}

func TestStatsUsage_EmptyReturnsArrayNotNull(t *testing.T) {
	r := setupStatsRouter(&testutil.MockStatsRepository{}, &testutil.MockCreditRepository{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/stats/usage", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	// buckets must serialize as [] not null, so clients can iterate safely.
	var resp map[string]json.RawMessage
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if string(resp["buckets"]) != "[]" {
		t.Errorf("buckets = %s, want []", resp["buckets"])
	}
}
