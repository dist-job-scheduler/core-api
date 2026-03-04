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

type ScheduleHandler struct {
	uc     *usecase.ScheduleUsecase
	logger *slog.Logger
}

func NewScheduleHandler(uc *usecase.ScheduleUsecase, logger *slog.Logger) *ScheduleHandler {
	return &ScheduleHandler{uc: uc, logger: logger.With("component", "schedule_handler")}
}

type createScheduleRequest struct {
	Name           string            `json:"name"            binding:"required,max=256"`
	CronExpr       string            `json:"cron_expr"       binding:"required"`
	URL            string            `json:"url"             binding:"required,url,max=2048"`
	Method         string            `json:"method"          binding:"omitempty,oneof=GET POST PUT PATCH DELETE"`
	Headers        map[string]string `json:"headers"`
	Body           *string           `json:"body"`
	TimeoutSeconds int               `json:"timeout_seconds" binding:"omitempty,min=1,max=3600"`
	MaxRetries     *int              `json:"max_retries"     binding:"omitempty,min=0,max=20"`
	Backoff        domain.Backoff    `json:"backoff"         binding:"omitempty,oneof=exponential linear"`
}

type scheduleResponse struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	CronExpr       string         `json:"cron_expr"`
	URL            string         `json:"url"`
	Method         string         `json:"method"`
	TimeoutSeconds int            `json:"timeout_seconds"`
	MaxRetries     int            `json:"max_retries"`
	Backoff        domain.Backoff `json:"backoff"`
	Paused         bool           `json:"paused"`
	NextRunAt      time.Time      `json:"next_run_at"`
	LastRunAt      *time.Time     `json:"last_run_at,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
}

func toScheduleResponse(s *domain.Schedule) scheduleResponse {
	return scheduleResponse{
		ID:             s.ID,
		Name:           s.Name,
		CronExpr:       s.CronExpr,
		URL:            s.URL,
		Method:         s.Method,
		TimeoutSeconds: s.TimeoutSeconds,
		MaxRetries:     s.MaxRetries,
		Backoff:        s.Backoff,
		Paused:         s.Paused,
		NextRunAt:      s.NextRunAt,
		LastRunAt:      s.LastRunAt,
		CreatedAt:      s.CreatedAt,
	}
}

func (h *ScheduleHandler) Create(ctx *gin.Context) {
	var req createScheduleRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	method := req.Method
	if method == "" {
		method = "POST"
	}

	s, err := h.uc.CreateSchedule(ctx.Request.Context(), usecase.CreateScheduleInput{
		UserID:         ctx.GetString("userID"),
		Name:           req.Name,
		CronExpr:       req.CronExpr,
		URL:            req.URL,
		Method:         method,
		Headers:        req.Headers,
		Body:           req.Body,
		TimeoutSeconds: req.TimeoutSeconds,
		MaxRetries:     req.MaxRetries,
		Backoff:        req.Backoff,
	})
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrInvalidCronExpr):
			ctx.JSON(http.StatusBadRequest, gin.H{"error": errInvalidCronExpr})
		case errors.Is(err, domain.ErrScheduleNameConflict):
			ctx.JSON(http.StatusConflict, gin.H{"error": errScheduleNameConflict})
		default:
			h.logger.Error("create schedule", "error", err)
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": errInternalServer})
		}
		return
	}

	ctx.JSON(http.StatusCreated, toScheduleResponse(s))
}

func (h *ScheduleHandler) List(ctx *gin.Context) {
	limit, _ := strconv.Atoi(ctx.Query("limit"))

	result, err := h.uc.ListSchedules(ctx.Request.Context(), usecase.ListSchedulesInput{
		UserID: ctx.GetString("userID"),
		Cursor: ctx.Query("cursor"),
		Limit:  limit,
	})
	if err != nil {
		h.logger.Error("list schedules", "error", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": errInternalServer})
		return
	}

	items := make([]scheduleResponse, len(result.Schedules))
	for i, s := range result.Schedules {
		items[i] = toScheduleResponse(s)
	}
	ctx.JSON(http.StatusOK, gin.H{
		"schedules":   items,
		"next_cursor": result.NextCursor,
	})
}

func (h *ScheduleHandler) GetByID(ctx *gin.Context) {
	id := ctx.Param("id")

	s, err := h.uc.GetSchedule(ctx.Request.Context(), id, ctx.GetString("userID"))
	if err != nil {
		if errors.Is(err, domain.ErrScheduleNotFound) {
			ctx.JSON(http.StatusNotFound, gin.H{"error": errScheduleNotFound})
			return
		}
		h.logger.Error("get schedule", "schedule_id", id, "error", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": errInternalServer})
		return
	}

	ctx.JSON(http.StatusOK, toScheduleResponse(s))
}

func (h *ScheduleHandler) Pause(ctx *gin.Context) {
	id := ctx.Param("id")

	err := h.uc.PauseSchedule(ctx.Request.Context(), id, ctx.GetString("userID"))
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrScheduleNotFound):
			ctx.JSON(http.StatusNotFound, gin.H{"error": errScheduleNotFound})
		case errors.Is(err, domain.ErrScheduleAlreadyPaused):
			ctx.JSON(http.StatusConflict, gin.H{"error": errScheduleAlreadyPaused})
		default:
			h.logger.Error("pause schedule", "schedule_id", id, "error", err)
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": errInternalServer})
		}
		return
	}

	ctx.Status(http.StatusNoContent)
}

func (h *ScheduleHandler) Resume(ctx *gin.Context) {
	id := ctx.Param("id")

	err := h.uc.ResumeSchedule(ctx.Request.Context(), id, ctx.GetString("userID"))
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrScheduleNotFound):
			ctx.JSON(http.StatusNotFound, gin.H{"error": errScheduleNotFound})
		case errors.Is(err, domain.ErrScheduleNotPaused):
			ctx.JSON(http.StatusConflict, gin.H{"error": errScheduleNotPaused})
		default:
			h.logger.Error("resume schedule", "schedule_id", id, "error", err)
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": errInternalServer})
		}
		return
	}

	ctx.Status(http.StatusNoContent)
}

func (h *ScheduleHandler) Delete(ctx *gin.Context) {
	id := ctx.Param("id")

	err := h.uc.DeleteSchedule(ctx.Request.Context(), id, ctx.GetString("userID"))
	if err != nil {
		if errors.Is(err, domain.ErrScheduleNotFound) {
			ctx.JSON(http.StatusNotFound, gin.H{"error": errScheduleNotFound})
			return
		}
		h.logger.Error("delete schedule", "schedule_id", id, "error", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": errInternalServer})
		return
	}

	ctx.Status(http.StatusNoContent)
}

func (h *ScheduleHandler) ListJobs(ctx *gin.Context) {
	id := ctx.Param("id")
	limit, _ := strconv.Atoi(ctx.Query("limit"))

	result, err := h.uc.ListScheduleJobs(ctx.Request.Context(), usecase.ListScheduleJobsInput{
		ScheduleID: id,
		UserID:     ctx.GetString("userID"),
		Cursor:     ctx.Query("cursor"),
		Limit:      limit,
	})
	if err != nil {
		if errors.Is(err, domain.ErrScheduleNotFound) {
			ctx.JSON(http.StatusNotFound, gin.H{"error": errScheduleNotFound})
			return
		}
		h.logger.Error("list schedule jobs", "schedule_id", id, "error", err)
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
	ctx.JSON(http.StatusOK, gin.H{
		"jobs":        items,
		"next_cursor": result.NextCursor,
	})
}
