package handler

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/usecase"
	"github.com/gin-gonic/gin"
)

type StatsHandler struct {
	uc     *usecase.StatsUsecase
	logger *slog.Logger
}

func NewStatsHandler(uc *usecase.StatsUsecase, logger *slog.Logger) *StatsHandler {
	return &StatsHandler{uc: uc, logger: logger.With("component", "stats_handler")}
}

// parseDays reads the ?days query param. Empty or invalid yields 0, which the
// usecase treats as "use the default window".
func parseDays(raw string) (int, bool) {
	if raw == "" {
		return 0, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

type jobStatsResponse struct {
	TotalExecutions int64   `json:"total_executions"`
	Succeeded       int64   `json:"succeeded"`
	Failed          int64   `json:"failed"`
	SuccessRate     float64 `json:"success_rate"`
	AvgDurationMS   float64 `json:"avg_duration_ms"`
	P95DurationMS   float64 `json:"p95_duration_ms"`
	SinceDays       int     `json:"since_days"`
}

func (h *StatsHandler) Jobs(ctx *gin.Context) {
	days, ok := parseDays(ctx.Query("days"))
	if !ok {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": errInvalidDays})
		return
	}

	s, err := h.uc.GetJobStats(ctx.Request.Context(), ctx.GetString("userID"), days)
	if err != nil {
		h.logger.ErrorContext(ctx.Request.Context(), "job stats", "error", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": errInternalServer})
		return
	}

	ctx.JSON(http.StatusOK, jobStatsResponse{
		TotalExecutions: s.TotalExecutions,
		Succeeded:       s.Succeeded,
		Failed:          s.Failed,
		SuccessRate:     s.SuccessRate,
		AvgDurationMS:   s.AvgDurationMS,
		P95DurationMS:   s.P95DurationMS,
		SinceDays:       s.SinceDays,
	})
}

type usageResponse struct {
	Buckets               []domain.UsageBucket `json:"buckets"`
	TotalJobExecutions    int64                `json:"total_job_executions"`
	TotalBufferExecutions int64                `json:"total_buffer_executions"`
	Balance               int64                `json:"balance"`
	Plan                  domain.Plan          `json:"plan"`
	SinceDays             int                  `json:"since_days"`
}

func (h *StatsHandler) Usage(ctx *gin.Context) {
	days, ok := parseDays(ctx.Query("days"))
	if !ok {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": errInvalidDays})
		return
	}

	summary, err := h.uc.GetUsage(ctx.Request.Context(), ctx.GetString("userID"), days)
	if err != nil {
		h.logger.ErrorContext(ctx.Request.Context(), "usage", "error", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": errInternalServer})
		return
	}

	buckets := summary.Buckets
	if buckets == nil {
		buckets = []domain.UsageBucket{}
	}
	ctx.JSON(http.StatusOK, usageResponse{
		Buckets:               buckets,
		TotalJobExecutions:    summary.TotalJobExecutions,
		TotalBufferExecutions: summary.TotalBufferExecutions,
		Balance:               summary.Balance,
		Plan:                  summary.Plan,
		SinceDays:             summary.SinceDays,
	})
}
