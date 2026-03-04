package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/usecase"
	"github.com/gin-gonic/gin"
)

type JobHandler struct {
	jobUsecase *usecase.JobUsecase
	logger     *slog.Logger
}

func NewJobHandler(jobUsecase *usecase.JobUsecase, logger *slog.Logger) *JobHandler {
	return &JobHandler{jobUsecase: jobUsecase, logger: logger.With("component", "job_handler")}
}

type createJobRequest struct {
	IdempotencyKey string            `json:"idempotency_key" binding:"omitempty,max=256"`
	URL            string            `json:"url"             binding:"required,url,max=2048"`
	Method         string            `json:"method"          binding:"required,oneof=GET POST PUT PATCH DELETE"`
	Headers        map[string]string `json:"headers"`
	Body           *string           `json:"body"`
	TimeoutSeconds int               `json:"timeout_seconds" binding:"omitempty,min=1,max=3600"`
	ScheduledAt    time.Time         `json:"scheduled_at"    binding:"required"`
	MaxRetries     *int              `json:"max_retries"     binding:"omitempty,min=0,max=20"`
	Backoff        domain.Backoff    `json:"backoff"         binding:"omitempty,oneof=exponential linear"`
}

type createJobResponse struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
}

type getJobResponse struct {
	ID          string        `json:"id"`
	Status      domain.Status `json:"status"`
	ScheduledAt time.Time     `json:"scheduled_at"`
	CreatedAt   time.Time     `json:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
	CompletedAt *time.Time    `json:"completed_at,omitempty"`
	LastError   *string       `json:"last_error,omitempty"`
	ScheduleID  *string       `json:"schedule_id,omitempty"`
}

type listJobItem struct {
	ID          string        `json:"id"`
	Status      domain.Status `json:"status"`
	URL         string        `json:"url"`
	Method      string        `json:"method"`
	ScheduledAt time.Time     `json:"scheduled_at"`
	CreatedAt   time.Time     `json:"created_at"`
	CompletedAt *time.Time    `json:"completed_at,omitempty"`
	LastError   *string       `json:"last_error,omitempty"`
	ScheduleID  *string       `json:"schedule_id,omitempty"`
}

type listJobsResponse struct {
	Jobs       []listJobItem `json:"jobs"`
	NextCursor *string       `json:"next_cursor"`
}

type attemptResponse struct {
	ID          string     `json:"id"`
	JobID       string     `json:"job_id"`
	AttemptNum  int        `json:"attempt_num"`
	WorkerID    string     `json:"worker_id"`
	StartedAt   time.Time  `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at"`
	StatusCode  *int       `json:"status_code"`
	Error       *string    `json:"error"`
	DurationMS  *int64     `json:"duration_ms"`
}

func (h *JobHandler) Cancel(ctx *gin.Context) {
	jobID := ctx.Param("id")

	err := h.jobUsecase.CancelJob(ctx.Request.Context(), jobID, ctx.GetString("userID"))
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrJobNotFound):
			ctx.JSON(http.StatusNotFound, gin.H{"error": errJobNotFound})
		case errors.Is(err, domain.ErrJobNotCancellable):
			ctx.JSON(http.StatusConflict, gin.H{"error": errJobNotCancellable})
		default:
			h.logger.ErrorContext(ctx.Request.Context(), "cancel job", "job_id", jobID, "error", err)
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": errInternalServer})
		}
		return
	}

	ctx.Status(http.StatusNoContent)
}

func (h *JobHandler) List(ctx *gin.Context) {
	limit, _ := strconv.Atoi(ctx.Query("limit"))

	result, err := h.jobUsecase.ListJobs(ctx.Request.Context(), usecase.ListJobsInput{
		UserID: ctx.GetString("userID"),
		Status: ctx.Query("status"),
		Cursor: ctx.Query("cursor"),
		Limit:  limit,
	})
	if err != nil {
		if errors.Is(err, domain.ErrInvalidStatus) {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": errInvalidStatus})
			return
		}
		h.logger.ErrorContext(ctx.Request.Context(), "list jobs", "error", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": errInternalServer})
		return
	}

	items := make([]listJobItem, len(result.Jobs))
	for i, j := range result.Jobs {
		items[i] = listJobItem{
			ID:          j.ID,
			Status:      j.Status,
			URL:         j.URL,
			Method:      j.Method,
			ScheduledAt: j.ScheduledAt,
			CreatedAt:   j.CreatedAt,
			CompletedAt: j.CompletedAt,
			LastError:   j.LastError,
			ScheduleID:  j.ScheduleID,
		}
	}
	ctx.JSON(http.StatusOK, listJobsResponse{
		Jobs:       items,
		NextCursor: result.NextCursor,
	})
}

func (h *JobHandler) Create(ctx *gin.Context) {
	var req createJobRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	job, err := h.jobUsecase.CreateJob(ctx.Request.Context(), usecase.CreateJobInput{
		UserID:         ctx.GetString("userID"),
		IdempotencyKey: req.IdempotencyKey,
		URL:            req.URL,
		Method:         req.Method,
		Headers:        req.Headers,
		Body:           req.Body,
		TimeoutSeconds: req.TimeoutSeconds,
		ScheduledAt:    req.ScheduledAt,
		MaxRetries:     req.MaxRetries,
		Backoff:        req.Backoff,
	})
	if err != nil {
		if errors.Is(err, domain.ErrDuplicateJob) {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": errDuplicateJob})
			return
		}
		h.logger.ErrorContext(ctx.Request.Context(), "create job", "error", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": errInternalServer})
		return
	}

	ctx.JSON(http.StatusCreated, createJobResponse{
		ID:        job.ID,
		CreatedAt: job.CreatedAt,
	})
}

func (h *JobHandler) ListAttempts(ctx *gin.Context) {
	jobID := ctx.Param("id")

	attempts, err := h.jobUsecase.ListAttempts(ctx.Request.Context(), jobID, ctx.GetString("userID"))
	if err != nil {
		if errors.Is(err, domain.ErrJobNotFound) {
			ctx.JSON(http.StatusNotFound, gin.H{"error": errJobNotFound})
			return
		}
		h.logger.ErrorContext(ctx.Request.Context(), "list attempts", "job_id", jobID, "error", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": errInternalServer})
		return
	}

	resp := make([]attemptResponse, len(attempts))
	for i, a := range attempts {
		resp[i] = attemptResponse{
			ID:          a.ID,
			JobID:       a.JobID,
			AttemptNum:  a.AttemptNum,
			WorkerID:    a.WorkerID,
			StartedAt:   a.StartedAt,
			CompletedAt: a.CompletedAt,
			StatusCode:  a.StatusCode,
			Error:       a.Error,
			DurationMS:  a.DurationMS,
		}
	}
	ctx.JSON(http.StatusOK, resp)
}

func (h *JobHandler) GetByID(ctx *gin.Context) {
	jobID := ctx.Param("id")

	job, err := h.jobUsecase.GetByID(ctx.Request.Context(), jobID, ctx.GetString("userID"))
	if err != nil {
		if errors.Is(err, domain.ErrJobNotFound) {
			ctx.JSON(http.StatusNotFound, gin.H{"error": errJobNotFound})
			return
		}
		h.logger.ErrorContext(ctx.Request.Context(), "get job by id", "job_id", jobID, "error", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": errInternalServer})
		return
	}

	ctx.JSON(http.StatusOK, getJobResponse{
		ID:          job.ID,
		Status:      job.Status,
		ScheduledAt: job.ScheduledAt,
		CreatedAt:   job.CreatedAt,
		UpdatedAt:   job.UpdatedAt,
		CompletedAt: job.CompletedAt,
		LastError:   job.LastError,
		ScheduleID:  job.ScheduleID,
	})
}
