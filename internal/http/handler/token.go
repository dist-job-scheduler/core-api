package handler

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/repository"
	"github.com/gin-gonic/gin"
)

type TokenHandler struct {
	tokenRepo repository.APITokenRepository
	logger    *slog.Logger
}

func NewTokenHandler(tokenRepo repository.APITokenRepository, logger *slog.Logger) *TokenHandler {
	return &TokenHandler{tokenRepo: tokenRepo, logger: logger.With("component", "token_handler")}
}

type createTokenRequest struct {
	Name string `json:"name" binding:"required,max=256"`
}

type createTokenResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Prefix    string    `json:"prefix"`
	Token     string    `json:"token"`
	CreatedAt time.Time `json:"created_at"`
}

type listTokenItem struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	LastUsedAt *time.Time `json:"last_used_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

func (h *TokenHandler) Create(ctx *gin.Context) {
	var req createTokenRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	rawBytes := make([]byte, 32)
	if _, err := rand.Read(rawBytes); err != nil {
		h.logger.ErrorContext(ctx.Request.Context(), "generate token bytes", "error", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": errInternalServer})
		return
	}

	rawToken := "fliq_sk_" + hex.EncodeToString(rawBytes)
	sum := sha256.Sum256([]byte(rawToken))
	tokenHash := fmt.Sprintf("%x", sum)
	prefix := rawToken[:16] // "fliq_sk_" (8) + first 8 hex chars

	userID := ctx.GetString("userID")
	tok, err := h.tokenRepo.Create(ctx.Request.Context(), userID, req.Name, tokenHash, prefix)
	if err != nil {
		h.logger.ErrorContext(ctx.Request.Context(), "create api token", "error", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": errInternalServer})
		return
	}

	ctx.JSON(http.StatusCreated, createTokenResponse{
		ID:        tok.ID,
		Name:      tok.Name,
		Prefix:    tok.Prefix,
		Token:     rawToken,
		CreatedAt: tok.CreatedAt,
	})
}

func (h *TokenHandler) List(ctx *gin.Context) {
	userID := ctx.GetString("userID")
	tokens, err := h.tokenRepo.ListByUserID(ctx.Request.Context(), userID)
	if err != nil {
		h.logger.ErrorContext(ctx.Request.Context(), "list api tokens", "error", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": errInternalServer})
		return
	}

	items := make([]listTokenItem, len(tokens))
	for i, tok := range tokens {
		items[i] = listTokenItem{
			ID:         tok.ID,
			Name:       tok.Name,
			Prefix:     tok.Prefix,
			LastUsedAt: tok.LastUsedAt,
			CreatedAt:  tok.CreatedAt,
		}
	}
	ctx.JSON(http.StatusOK, items)
}

func (h *TokenHandler) Delete(ctx *gin.Context) {
	tokenID := ctx.Param("id")
	userID := ctx.GetString("userID")

	err := h.tokenRepo.Delete(ctx.Request.Context(), tokenID, userID)
	if err != nil {
		if errors.Is(err, domain.ErrTokenNotFound) {
			ctx.JSON(http.StatusNotFound, gin.H{"error": errTokenNotFound})
			return
		}
		h.logger.ErrorContext(ctx.Request.Context(), "delete api token", "token_id", tokenID, "error", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": errInternalServer})
		return
	}

	ctx.Status(http.StatusNoContent)
}
