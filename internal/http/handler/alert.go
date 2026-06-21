package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"net/mail"
	"net/url"
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
	// Target is validated per-type in validateTarget (URL for webhook/slack, an
	// email address for email) rather than with a single binding tag, since the
	// two shapes are mutually exclusive.
	Type   domain.AlertChannelType `json:"type"   binding:"required,oneof=webhook slack email"`
	Target string                  `json:"target" binding:"required,max=2048"`
	Name   string                  `json:"name"   binding:"omitempty,max=256"`
}

// validateTarget enforces the target shape for the channel type: a parseable
// http(s) URL for webhook/slack, a valid address for email.
func validateTarget(t domain.AlertChannelType, target string) bool {
	if t == domain.AlertChannelEmail {
		_, err := mail.ParseAddress(target)
		return err == nil
	}
	u, err := url.Parse(target)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
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
	Verified  bool                    `json:"verified"`
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
		Verified:  ch.Verified,
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

	if !validateTarget(req.Type, req.Target) {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": errInvalidAlertTarget})
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
		if errors.Is(err, domain.ErrAlertChannelNotVerified) {
			ctx.JSON(http.StatusConflict, gin.H{"error": errAlertChannelNotVerified})
			return
		}
		h.logger.ErrorContext(ctx.Request.Context(), "update alert channel", "channel_id", id, "error", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": errInternalServer})
		return
	}

	ctx.Status(http.StatusNoContent)
}

// Verify confirms an email alert channel from a signed token in the query
// string and enables it. Public (the token is the credential) — registered
// outside the authenticated /alerts group.
func (h *AlertHandler) Verify(ctx *gin.Context) {
	token := ctx.Query("token")
	if token == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": errInvalidVerifyToken})
		return
	}

	if err := h.uc.VerifyChannel(ctx.Request.Context(), token); err != nil {
		if errors.Is(err, domain.ErrInvalidVerificationToken) || errors.Is(err, domain.ErrAlertChannelNotFound) {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": errInvalidVerifyToken})
			return
		}
		h.logger.ErrorContext(ctx.Request.Context(), "verify alert channel", "error", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": errInternalServer})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"status": "verified"})
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
