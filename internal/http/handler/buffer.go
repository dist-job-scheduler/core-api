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

type BufferHandler struct {
	uc     *usecase.BufferUsecase
	logger *slog.Logger
}

func NewBufferHandler(uc *usecase.BufferUsecase, logger *slog.Logger) *BufferHandler {
	return &BufferHandler{uc: uc, logger: logger.With("component", "buffer_handler")}
}

// ── Request / Response types ─────────────────────────────────────────────────

type createBufferRequest struct {
	Name           string            `json:"name"            binding:"required,max=256"`
	URL            string            `json:"url"             binding:"required,url,max=2048"`
	Method         string            `json:"method"          binding:"omitempty,oneof=GET POST PUT PATCH DELETE"`
	Headers        map[string]string `json:"headers"`
	TimeoutSeconds int               `json:"timeout_seconds" binding:"omitempty,min=1,max=3600"`
	RateLimit      int               `json:"rate_limit"      binding:"omitempty,min=1,max=1000"`
	MaxRetries     *int              `json:"max_retries"     binding:"omitempty,min=0,max=20"`
	Backoff        domain.Backoff    `json:"backoff"         binding:"omitempty,oneof=exponential linear"`
	WebhookURL     *string           `json:"webhook_url"     binding:"omitempty,url,max=2048"`
	WebhookHeaders map[string]string `json:"webhook_headers"`
}

type bufferResponse struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	URL            string         `json:"url"`
	Method         string         `json:"method"`
	TimeoutSeconds int            `json:"timeout_seconds"`
	RateLimit      int            `json:"rate_limit"`
	MaxRetries     int            `json:"max_retries"`
	Backoff        domain.Backoff `json:"backoff"`
	Paused         bool           `json:"paused"`
	WebhookURL     *string        `json:"webhook_url,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

func toBufferResponse(b *domain.Buffer) bufferResponse {
	return bufferResponse{
		ID:             b.ID,
		Name:           b.Name,
		URL:            b.URL,
		Method:         b.Method,
		TimeoutSeconds: b.TimeoutSeconds,
		RateLimit:      b.RateLimit,
		MaxRetries:     b.MaxRetries,
		Backoff:        b.Backoff,
		Paused:         b.Paused,
		WebhookURL:     b.WebhookURL,
		CreatedAt:      b.CreatedAt,
		UpdatedAt:      b.UpdatedAt,
	}
}

type pushItemRequest struct {
	Body    *string           `json:"body"`
	Headers map[string]string `json:"headers"`
}

type bufferItemResponse struct {
	ID          string                  `json:"id"`
	BufferID    string                  `json:"buffer_id"`
	Status      domain.BufferItemStatus `json:"status"`
	StatusCode  *int                    `json:"status_code,omitempty"`
	RetryCount  int                     `json:"retry_count"`
	MaxRetries  int                     `json:"max_retries"`
	LastError   *string                 `json:"last_error,omitempty"`
	ReplayOf    *string                 `json:"replay_of,omitempty"`
	CreatedAt   time.Time               `json:"created_at"`
	CompletedAt *time.Time              `json:"completed_at,omitempty"`
}

func toBufferItemResponse(item *domain.BufferItem) bufferItemResponse {
	return bufferItemResponse{
		ID:          item.ID,
		BufferID:    item.BufferID,
		Status:      item.Status,
		StatusCode:  item.StatusCode,
		RetryCount:  item.RetryCount,
		MaxRetries:  item.MaxRetries,
		LastError:   item.LastError,
		ReplayOf:    item.ReplayOf,
		CreatedAt:   item.CreatedAt,
		CompletedAt: item.CompletedAt,
	}
}

// ── Handlers ─────────────────────────────────────────────────────────────────

func (h *BufferHandler) Create(ctx *gin.Context) {
	var req createBufferRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": formatValidationError(err)})
		return
	}

	method := req.Method
	if method == "" {
		method = "POST"
	}

	b, err := h.uc.CreateBuffer(ctx.Request.Context(), usecase.CreateBufferInput{
		UserID:         ctx.GetString("userID"),
		Name:           req.Name,
		URL:            req.URL,
		Method:         method,
		Headers:        req.Headers,
		TimeoutSeconds: req.TimeoutSeconds,
		RateLimit:      req.RateLimit,
		MaxRetries:     req.MaxRetries,
		Backoff:        req.Backoff,
		WebhookURL:     req.WebhookURL,
		WebhookHeaders: req.WebhookHeaders,
	})
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrBufferNameConflict):
			ctx.JSON(http.StatusConflict, gin.H{"error": errBufferNameConflict})
		default:
			h.logger.ErrorContext(ctx.Request.Context(), "create buffer", "error", err)
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": errInternalServer})
		}
		return
	}

	ctx.JSON(http.StatusCreated, toBufferResponse(b))
}

func (h *BufferHandler) List(ctx *gin.Context) {
	limit, err := parseLimit(ctx.Query("limit"))
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": errInvalidLimit})
		return
	}

	result, err := h.uc.ListBuffers(ctx.Request.Context(), usecase.ListBuffersInput{
		UserID: ctx.GetString("userID"),
		Cursor: ctx.Query("cursor"),
		Limit:  limit,
	})
	if err != nil {
		h.logger.ErrorContext(ctx.Request.Context(), "list buffers", "error", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": errInternalServer})
		return
	}

	items := make([]bufferResponse, len(result.Buffers))
	for i, b := range result.Buffers {
		items[i] = toBufferResponse(b)
	}
	ctx.JSON(http.StatusOK, gin.H{
		"buffers":     items,
		"next_cursor": result.NextCursor,
	})
}

func (h *BufferHandler) GetByID(ctx *gin.Context) {
	id := ctx.Param("id")

	b, err := h.uc.GetBuffer(ctx.Request.Context(), id, ctx.GetString("userID"))
	if err != nil {
		if errors.Is(err, domain.ErrBufferNotFound) {
			ctx.JSON(http.StatusNotFound, gin.H{"error": errBufferNotFound})
			return
		}
		h.logger.ErrorContext(ctx.Request.Context(), "get buffer", "buffer_id", id, "error", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": errInternalServer})
		return
	}

	ctx.JSON(http.StatusOK, toBufferResponse(b))
}

type bufferStatsResponse struct {
	Pending     int64   `json:"pending"`
	Running     int64   `json:"running"`
	Completed   int64   `json:"completed"`
	Failed      int64   `json:"failed"`
	Total       int64   `json:"total"`
	SuccessRate float64 `json:"success_rate"`
}

func (h *BufferHandler) Stats(ctx *gin.Context) {
	id := ctx.Param("id")

	s, err := h.uc.GetBufferStats(ctx.Request.Context(), id, ctx.GetString("userID"))
	if err != nil {
		if errors.Is(err, domain.ErrBufferNotFound) {
			ctx.JSON(http.StatusNotFound, gin.H{"error": errBufferNotFound})
			return
		}
		h.logger.ErrorContext(ctx.Request.Context(), "buffer stats", "buffer_id", id, "error", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": errInternalServer})
		return
	}

	ctx.JSON(http.StatusOK, bufferStatsResponse{
		Pending:     s.Pending,
		Running:     s.Running,
		Completed:   s.Completed,
		Failed:      s.Failed,
		Total:       s.Total,
		SuccessRate: s.SuccessRate,
	})
}

func (h *BufferHandler) Pause(ctx *gin.Context) {
	id := ctx.Param("id")

	err := h.uc.PauseBuffer(ctx.Request.Context(), id, ctx.GetString("userID"))
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrBufferNotFound):
			ctx.JSON(http.StatusNotFound, gin.H{"error": errBufferNotFound})
		case errors.Is(err, domain.ErrBufferAlreadyPaused):
			ctx.JSON(http.StatusConflict, gin.H{"error": errBufferAlreadyPaused})
		default:
			h.logger.ErrorContext(ctx.Request.Context(), "pause buffer", "buffer_id", id, "error", err)
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": errInternalServer})
		}
		return
	}

	ctx.Status(http.StatusNoContent)
}

func (h *BufferHandler) Resume(ctx *gin.Context) {
	id := ctx.Param("id")

	err := h.uc.ResumeBuffer(ctx.Request.Context(), id, ctx.GetString("userID"))
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrBufferNotFound):
			ctx.JSON(http.StatusNotFound, gin.H{"error": errBufferNotFound})
		case errors.Is(err, domain.ErrBufferNotPaused):
			ctx.JSON(http.StatusConflict, gin.H{"error": errBufferNotPaused})
		default:
			h.logger.ErrorContext(ctx.Request.Context(), "resume buffer", "buffer_id", id, "error", err)
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": errInternalServer})
		}
		return
	}

	ctx.Status(http.StatusNoContent)
}

func (h *BufferHandler) Delete(ctx *gin.Context) {
	id := ctx.Param("id")

	err := h.uc.DeleteBuffer(ctx.Request.Context(), id, ctx.GetString("userID"))
	if err != nil {
		if errors.Is(err, domain.ErrBufferNotFound) {
			ctx.JSON(http.StatusNotFound, gin.H{"error": errBufferNotFound})
			return
		}
		h.logger.ErrorContext(ctx.Request.Context(), "delete buffer", "buffer_id", id, "error", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": errInternalServer})
		return
	}

	ctx.Status(http.StatusNoContent)
}

func (h *BufferHandler) PushItem(ctx *gin.Context) {
	bufferID := ctx.Param("id")

	var req pushItemRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": formatValidationError(err)})
		return
	}

	item, err := h.uc.PushItem(ctx.Request.Context(), usecase.PushBufferItemInput{
		UserID:   ctx.GetString("userID"),
		BufferID: bufferID,
		Body:     req.Body,
		Headers:  req.Headers,
	})
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrBufferNotFound):
			ctx.JSON(http.StatusNotFound, gin.H{"error": errBufferNotFound})
		case errors.Is(err, domain.ErrInsufficientCredits):
			ctx.JSON(http.StatusPaymentRequired, gin.H{"error": errInsufficientCredits})
		default:
			h.logger.ErrorContext(ctx.Request.Context(), "push buffer item", "buffer_id", bufferID, "error", err)
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": errInternalServer})
		}
		return
	}

	ctx.JSON(http.StatusCreated, toBufferItemResponse(item))
}

func (h *BufferHandler) ListItems(ctx *gin.Context) {
	bufferID := ctx.Param("id")

	limit, err := parseLimit(ctx.Query("limit"))
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": errInvalidLimit})
		return
	}

	result, err := h.uc.ListItems(ctx.Request.Context(), usecase.ListBufferItemsInput{
		BufferID: bufferID,
		UserID:   ctx.GetString("userID"),
		Status:   ctx.Query("status"),
		Cursor:   ctx.Query("cursor"),
		Limit:    limit,
	})
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrBufferNotFound):
			ctx.JSON(http.StatusNotFound, gin.H{"error": errBufferNotFound})
		case errors.Is(err, domain.ErrInvalidStatus):
			ctx.JSON(http.StatusBadRequest, gin.H{"error": errInvalidStatus})
		default:
			h.logger.ErrorContext(ctx.Request.Context(), "list buffer items", "buffer_id", bufferID, "error", err)
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": errInternalServer})
		}
		return
	}

	items := make([]bufferItemResponse, len(result.Items))
	for i, item := range result.Items {
		items[i] = toBufferItemResponse(item)
	}
	ctx.JSON(http.StatusOK, gin.H{
		"items":       items,
		"next_cursor": result.NextCursor,
	})
}

// ReplayItem clones a permanently-failed buffer item into a fresh pending item
// appended to the tail of the buffer.
func (h *BufferHandler) ReplayItem(ctx *gin.Context) {
	bufferID := ctx.Param("id")
	itemID := ctx.Param("itemId")

	item, err := h.uc.ReplayItem(ctx.Request.Context(), itemID, bufferID, ctx.GetString("userID"))
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrBufferNotFound):
			ctx.JSON(http.StatusNotFound, gin.H{"error": errBufferNotFound})
		case errors.Is(err, domain.ErrBufferItemNotFound):
			ctx.JSON(http.StatusNotFound, gin.H{"error": errBufferItemNotFound})
		case errors.Is(err, domain.ErrBufferItemNotReplayable):
			ctx.JSON(http.StatusConflict, gin.H{"error": errBufferItemNotReplayable})
		case errors.Is(err, domain.ErrInsufficientCredits):
			ctx.JSON(http.StatusPaymentRequired, gin.H{"error": errInsufficientCredits})
		default:
			h.logger.ErrorContext(ctx.Request.Context(), "replay buffer item", "buffer_id", bufferID, "item_id", itemID, "error", err)
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": errInternalServer})
		}
		return
	}

	ctx.JSON(http.StatusCreated, toBufferItemResponse(item))
}

func (h *BufferHandler) GetItem(ctx *gin.Context) {
	bufferID := ctx.Param("id")
	itemID := ctx.Param("itemId")

	item, err := h.uc.GetItem(ctx.Request.Context(), itemID, bufferID, ctx.GetString("userID"))
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrBufferNotFound):
			ctx.JSON(http.StatusNotFound, gin.H{"error": errBufferNotFound})
		case errors.Is(err, domain.ErrBufferItemNotFound):
			ctx.JSON(http.StatusNotFound, gin.H{"error": errBufferItemNotFound})
		default:
			h.logger.ErrorContext(ctx.Request.Context(), "get buffer item", "buffer_id", bufferID, "item_id", itemID, "error", err)
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": errInternalServer})
		}
		return
	}

	ctx.JSON(http.StatusOK, toBufferItemResponse(item))
}
