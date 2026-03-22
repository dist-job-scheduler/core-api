package handler_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/http/handler"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/repository"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/testutil"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/usecase"
	"github.com/gin-gonic/gin"
)

func init() { gin.SetMode(gin.TestMode) }

// injectUser is middleware that sets userID in gin.Context (simulating auth middleware).
func injectUser(userID string) gin.HandlerFunc {
	return func(c *gin.Context) { c.Set("userID", userID) }
}

const testUserID = "user-test-123"

func setupJobRouter(jobRepo *testutil.MockJobRepository, attemptRepo *testutil.MockAttemptRepository, creditRepo *testutil.MockCreditRepository) *gin.Engine {
	uc := usecase.NewJobUsecase(jobRepo, attemptRepo, creditRepo)
	h := handler.NewJobHandler(uc, slog.Default())

	r := gin.New()
	g := r.Group("/", injectUser(testUserID))
	g.POST("/jobs", h.Create)
	g.GET("/jobs", h.List)
	g.GET("/jobs/:id", h.GetByID)
	g.DELETE("/jobs/:id", h.Cancel)
	g.GET("/jobs/:id/attempts", h.ListAttempts)
	return r
}

func TestCreateJob_HappyPath(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	jobRepo := &testutil.MockJobRepository{
		CreateFn: func(_ context.Context, job *domain.Job) (*domain.Job, error) {
			job.ID = "job-abc"
			job.CreatedAt = now
			return job, nil
		},
	}
	creditRepo := &testutil.MockCreditRepository{
		HasCreditsFn: func(_ context.Context, _ string) (bool, error) { return true, nil },
	}
	r := setupJobRouter(jobRepo, &testutil.MockAttemptRepository{}, creditRepo)

	body := `{"url":"https://example.com/hook","method":"POST","scheduled_at":"2099-01-01T00:00:00Z"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/jobs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp["id"] != "job-abc" {
		t.Errorf("id = %v, want %q", resp["id"], "job-abc")
	}
	if _, ok := resp["created_at"]; !ok {
		t.Error("response missing created_at")
	}
}

func TestCreateJob_BadJSON(t *testing.T) {
	r := setupJobRouter(&testutil.MockJobRepository{}, &testutil.MockAttemptRepository{}, &testutil.MockCreditRepository{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/jobs", strings.NewReader(`{bad json`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestCreateJob_MissingURL(t *testing.T) {
	r := setupJobRouter(&testutil.MockJobRepository{}, &testutil.MockAttemptRepository{}, &testutil.MockCreditRepository{})

	body := `{"method":"POST","scheduled_at":"2099-01-01T00:00:00Z"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/jobs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestCreateJob_InsufficientCredits(t *testing.T) {
	creditRepo := &testutil.MockCreditRepository{
		HasCreditsFn: func(_ context.Context, _ string) (bool, error) { return false, nil },
	}
	r := setupJobRouter(&testutil.MockJobRepository{}, &testutil.MockAttemptRepository{}, creditRepo)

	body := `{"url":"https://example.com/hook","method":"POST","scheduled_at":"2099-01-01T00:00:00Z"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/jobs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusPaymentRequired {
		t.Errorf("status = %d, want %d", w.Code, http.StatusPaymentRequired)
	}
}

func TestCreateJob_DuplicateIdempotencyKey(t *testing.T) {
	jobRepo := &testutil.MockJobRepository{
		CreateFn: func(_ context.Context, _ *domain.Job) (*domain.Job, error) {
			return nil, domain.ErrDuplicateJob
		},
	}
	creditRepo := &testutil.MockCreditRepository{
		HasCreditsFn: func(_ context.Context, _ string) (bool, error) { return true, nil },
	}
	r := setupJobRouter(jobRepo, &testutil.MockAttemptRepository{}, creditRepo)

	body := `{"url":"https://example.com/hook","method":"POST","scheduled_at":"2099-01-01T00:00:00Z","idempotency_key":"dup-key"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/jobs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestGetByID_HappyPath(t *testing.T) {
	job := testutil.NewJob(testutil.WithUserID(testUserID), testutil.WithJobID("job-xyz"))
	jobRepo := &testutil.MockJobRepository{
		GetByIDFn: func(_ context.Context, id, uid string) (*domain.Job, error) {
			if id == "job-xyz" && uid == testUserID {
				return job, nil
			}
			return nil, domain.ErrJobNotFound
		},
	}
	r := setupJobRouter(jobRepo, &testutil.MockAttemptRepository{}, &testutil.MockCreditRepository{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/jobs/job-xyz", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["id"] != "job-xyz" {
		t.Errorf("id = %v, want %q", resp["id"], "job-xyz")
	}
}

func TestGetByID_NotFound(t *testing.T) {
	jobRepo := &testutil.MockJobRepository{
		GetByIDFn: func(_ context.Context, _, _ string) (*domain.Job, error) {
			return nil, domain.ErrJobNotFound
		},
	}
	r := setupJobRouter(jobRepo, &testutil.MockAttemptRepository{}, &testutil.MockCreditRepository{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/jobs/nonexistent", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestListJobs_HappyPath(t *testing.T) {
	jobs := []*domain.Job{
		testutil.NewJob(testutil.WithUserID(testUserID), testutil.WithJobID("job-1")),
		testutil.NewJob(testutil.WithUserID(testUserID), testutil.WithJobID("job-2")),
	}
	jobRepo := &testutil.MockJobRepository{
		ListJobsFn: func(_ context.Context, _ repository.ListJobsInput) ([]*domain.Job, error) {
			return jobs, nil
		},
	}
	r := setupJobRouter(jobRepo, &testutil.MockAttemptRepository{}, &testutil.MockCreditRepository{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/jobs", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	jobsArr, ok := resp["jobs"].([]interface{})
	if !ok {
		t.Fatal("response missing jobs array")
	}
	if len(jobsArr) != 2 {
		t.Errorf("len(jobs) = %d, want 2", len(jobsArr))
	}
}

func TestListJobs_InvalidStatus(t *testing.T) {
	r := setupJobRouter(&testutil.MockJobRepository{}, &testutil.MockAttemptRepository{}, &testutil.MockCreditRepository{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/jobs?status=bogus", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestCancelJob_HappyPath(t *testing.T) {
	jobRepo := &testutil.MockJobRepository{
		CancelFn: func(_ context.Context, _, _ string) error { return nil },
	}
	r := setupJobRouter(jobRepo, &testutil.MockAttemptRepository{}, &testutil.MockCreditRepository{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/jobs/job-1", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
}

func TestCancelJob_NotFound(t *testing.T) {
	jobRepo := &testutil.MockJobRepository{
		CancelFn: func(_ context.Context, _, _ string) error { return domain.ErrJobNotFound },
	}
	r := setupJobRouter(jobRepo, &testutil.MockAttemptRepository{}, &testutil.MockCreditRepository{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/jobs/nonexistent", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestCancelJob_NotCancellable(t *testing.T) {
	jobRepo := &testutil.MockJobRepository{
		CancelFn: func(_ context.Context, _, _ string) error { return domain.ErrJobNotCancellable },
	}
	r := setupJobRouter(jobRepo, &testutil.MockAttemptRepository{}, &testutil.MockCreditRepository{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/jobs/job-completed", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d", w.Code, http.StatusConflict)
	}
}

func TestListAttempts_HappyPath(t *testing.T) {
	job := testutil.NewJob(testutil.WithUserID(testUserID), testutil.WithJobID("job-1"))
	jobRepo := &testutil.MockJobRepository{
		GetByIDFn: func(_ context.Context, id, uid string) (*domain.Job, error) {
			if id == "job-1" && uid == testUserID {
				return job, nil
			}
			return nil, domain.ErrJobNotFound
		},
	}
	now := time.Now().UTC()
	statusCode := 200
	durationMS := int64(150)
	attemptRepo := &testutil.MockAttemptRepository{
		ListByJobIDFn: func(_ context.Context, jobID string) ([]*domain.JobAttempt, error) {
			return []*domain.JobAttempt{
				{
					ID:          "att-1",
					JobID:       jobID,
					AttemptNum:  1,
					WorkerID:    "worker-1",
					StartedAt:   now,
					CompletedAt: &now,
					StatusCode:  &statusCode,
					DurationMS:  &durationMS,
				},
			}, nil
		},
	}
	r := setupJobRouter(jobRepo, attemptRepo, &testutil.MockCreditRepository{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/jobs/job-1/attempts", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp []map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp) != 1 {
		t.Errorf("len(attempts) = %d, want 1", len(resp))
	}
	if resp[0]["id"] != "att-1" {
		t.Errorf("attempt id = %v, want %q", resp[0]["id"], "att-1")
	}
}

func TestListAttempts_JobNotFound(t *testing.T) {
	jobRepo := &testutil.MockJobRepository{
		GetByIDFn: func(_ context.Context, _, _ string) (*domain.Job, error) {
			return nil, domain.ErrJobNotFound
		},
	}
	r := setupJobRouter(jobRepo, &testutil.MockAttemptRepository{}, &testutil.MockCreditRepository{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/jobs/nonexistent/attempts", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}
