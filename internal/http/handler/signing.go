package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/repository"
	"github.com/gin-gonic/gin"
)

type SigningHandler struct {
	signingRepo repository.SigningSecretRepository
	logger      *slog.Logger
}

func NewSigningHandler(signingRepo repository.SigningSecretRepository, logger *slog.Logger) *SigningHandler {
	return &SigningHandler{signingRepo: signingRepo, logger: logger.With("component", "signing_handler")}
}

type signingSecretResponse struct {
	Secret    string    `json:"secret"`
	CreatedAt time.Time `json:"created_at"`
}

// Get returns the active signing secret, creating one if none exists (lazy init).
func (h *SigningHandler) Get(ctx *gin.Context) {
	userID := ctx.GetString("userID")

	secret, err := h.signingRepo.Create(ctx.Request.Context(), userID)
	if err != nil {
		h.logger.ErrorContext(ctx.Request.Context(), "get or create signing secret", "error", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": errInternalServer})
		return
	}

	ctx.JSON(http.StatusOK, signingSecretResponse{
		Secret:    secret.Secret,
		CreatedAt: secret.CreatedAt,
	})
}

// Rotate deactivates the current signing secret and returns a new one.
func (h *SigningHandler) Rotate(ctx *gin.Context) {
	userID := ctx.GetString("userID")

	secret, err := h.signingRepo.Rotate(ctx.Request.Context(), userID)
	if err != nil {
		if errors.Is(err, domain.ErrSigningSecretNotFound) {
			ctx.JSON(http.StatusNotFound, gin.H{"error": errSigningSecretNotFound})
			return
		}
		h.logger.ErrorContext(ctx.Request.Context(), "rotate signing secret", "error", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": errInternalServer})
		return
	}

	ctx.JSON(http.StatusOK, signingSecretResponse{
		Secret:    secret.Secret,
		CreatedAt: secret.CreatedAt,
	})
}
