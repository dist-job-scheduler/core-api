package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/usecase"
	"github.com/gin-gonic/gin"
)

type AlertHandler struct {
	uc     *usecase.AlertUsecase
	logger *slog.Logger
}

func NewAlertHandler(uc *usecase.AlertUsecase, logger *slog.Logger) *AlertHandler {
	return &AlertHandler{uc: uc, logger: logger.With("component", "alert_handler")}
}

type createAlertChannelRequest struct {
	Type   domain.AlertChannelType `json:"type"   binding:"required,oneof=webhook slack"`
	Target string                  `json:"target" binding:"required,url,max=2048"`
	Name   string                  `json:"name"   binding:"omitempty,max=256"`
}

type updateAlertChannelRequest struct {
	Enabled *bool `json:"enabled" binding:"required"`
}

type alertChannelResponse struct {
	ID        string                  `json:"id"`
	Type      domain.AlertChannelType `json:"type"`
	Target    string                  `json:"target"`
	Name      string                  `json:"name"`
	Enabled   bool                    `json:"enabled"`
	CreatedAt time.Time               `json:"created_at"`
	UpdatedAt time.Time               `json:"updated_at"`
}

func toAlertChannelResponse(ch *domain.AlertChannel) alertChannelResponse {
	return alertChannelResponse{
		ID:        ch.ID,
		Type:      ch.Type,
		Target:    ch.Target,
		Name:      ch.Name,
		Enabled:   ch.Enabled,
		CreatedAt: ch.CreatedAt,
		UpdatedAt: ch.UpdatedAt,
	}
}

func (h *AlertHandler) Create(ctx *gin.Context) {
	var req createAlertChannelRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": formatValidationError(err)})
		return
	}

	ch, err := h.uc.CreateChannel(ctx.Request.Context(), usecase.CreateAlertChannelInput{
		UserID: ctx.GetString("userID"),
		Type:   req.Type,
		Target: req.Target,
		Name:   req.Name,
	})
	if err != nil {
		if errors.Is(err, domain.ErrInvalidAlertChannelType) {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": errInvalidAlertChannelType})
			return
		}
		h.logger.ErrorContext(ctx.Request.Context(), "create alert channel", "error", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": errInternalServer})
		return
	}

	ctx.JSON(http.StatusCreated, toAlertChannelResponse(ch))
}

func (h *AlertHandler) List(ctx *gin.Context) {
	channels, err := h.uc.ListChannels(ctx.Request.Context(), ctx.GetString("userID"))
	if err != nil {
		h.logger.ErrorContext(ctx.Request.Context(), "list alert channels", "error", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": errInternalServer})
		return
	}

	items := make([]alertChannelResponse, len(channels))
	for i, ch := range channels {
		items[i] = toAlertChannelResponse(ch)
	}
	ctx.JSON(http.StatusOK, gin.H{"channels": items})
}

func (h *AlertHandler) GetByID(ctx *gin.Context) {
	id := ctx.Param("id")

	ch, err := h.uc.GetChannel(ctx.Request.Context(), id, ctx.GetString("userID"))
	if err != nil {
		if errors.Is(err, domain.ErrAlertChannelNotFound) {
			ctx.JSON(http.StatusNotFound, gin.H{"error": errAlertChannelNotFound})
			return
		}
		h.logger.ErrorContext(ctx.Request.Context(), "get alert channel", "channel_id", id, "error", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": errInternalServer})
		return
	}

	ctx.JSON(http.StatusOK, toAlertChannelResponse(ch))
}

func (h *AlertHandler) Update(ctx *gin.Context) {
	id := ctx.Param("id")

	var req updateAlertChannelRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": formatValidationError(err)})
		return
	}

	if err := h.uc.SetEnabled(ctx.Request.Context(), id, ctx.GetString("userID"), *req.Enabled); err != nil {
		if errors.Is(err, domain.ErrAlertChannelNotFound) {
			ctx.JSON(http.StatusNotFound, gin.H{"error": errAlertChannelNotFound})
			return
		}
		h.logger.ErrorContext(ctx.Request.Context(), "update alert channel", "channel_id", id, "error", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": errInternalServer})
		return
	}

	ctx.Status(http.StatusNoContent)
}

func (h *AlertHandler) Delete(ctx *gin.Context) {
	id := ctx.Param("id")

	if err := h.uc.DeleteChannel(ctx.Request.Context(), id, ctx.GetString("userID")); err != nil {
		if errors.Is(err, domain.ErrAlertChannelNotFound) {
			ctx.JSON(http.StatusNotFound, gin.H{"error": errAlertChannelNotFound})
			return
		}
		h.logger.ErrorContext(ctx.Request.Context(), "delete alert channel", "channel_id", id, "error", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": errInternalServer})
		return
	}

	ctx.Status(http.StatusNoContent)
}
