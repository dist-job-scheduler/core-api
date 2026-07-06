package handler

import (
	"encoding/base64"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/repository"
	"github.com/gin-gonic/gin"
)

type WebhookHandler struct {
	deliveries repository.WebhookDeliveryRepository
	logger     *slog.Logger
}

func NewWebhookHandler(deliveries repository.WebhookDeliveryRepository, logger *slog.Logger) *WebhookHandler {
	return &WebhookHandler{deliveries: deliveries, logger: logger.With("component", "webhook_handler")}
}

const (
	defaultDeliveryPageSize = 25
	maxDeliveryPageSize     = 100
)

type webhookDeliveryResponse struct {
	ID          string     `json:"id"`
	JobID       *string    `json:"job_id,omitempty"`
	Event       string     `json:"event"`
	URL         string     `json:"url"`
	Status      string     `json:"status"`
	StatusCode  *int       `json:"status_code,omitempty"`
	Attempts    int        `json:"attempts"`
	MaxAttempts int        `json:"max_attempts"`
	LastError   *string    `json:"last_error,omitempty"`
	NextRetryAt *time.Time `json:"next_retry_at,omitempty"`
	DeliveredAt *time.Time `json:"delivered_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// ListDeliveries returns the authenticated user's recent webhook deliveries,
// newest first, keyset-paginated on (created_at, id). Response shape mirrors the
// other list endpoints: {"deliveries": [...], "next_cursor": "..."|null}.
func (h *WebhookHandler) ListDeliveries(ctx *gin.Context) {
	userID := ctx.GetString("userID")

	limit := defaultDeliveryPageSize
	if raw := ctx.Query("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > maxDeliveryPageSize {
		limit = maxDeliveryPageSize
	}

	var cursorTime *time.Time
	var cursorID string
	if raw := ctx.Query("cursor"); raw != "" {
		t, id, ok := decodeDeliveryCursor(raw)
		if !ok {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "invalid cursor"})
			return
		}
		cursorTime, cursorID = &t, id
	}

	// Fetch one extra row to detect whether a further page exists.
	rows, err := h.deliveries.ListByUser(ctx.Request.Context(), userID, limit+1, cursorTime, cursorID)
	if err != nil {
		h.logger.ErrorContext(ctx.Request.Context(), "list webhook deliveries", "error", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": errInternalServer})
		return
	}

	var nextCursor *string
	if len(rows) > limit {
		last := rows[limit-1]
		c := encodeDeliveryCursor(last.CreatedAt, last.ID)
		nextCursor = &c
		rows = rows[:limit]
	}

	out := make([]webhookDeliveryResponse, 0, len(rows))
	for _, d := range rows {
		out = append(out, toDeliveryResponse(d))
	}

	ctx.JSON(http.StatusOK, gin.H{"deliveries": out, "next_cursor": nextCursor})
}

func toDeliveryResponse(d *domain.WebhookDelivery) webhookDeliveryResponse {
	resp := webhookDeliveryResponse{
		ID:          d.ID,
		JobID:       d.JobID,
		Event:       string(d.Event),
		URL:         d.URL,
		Status:      string(d.Status),
		StatusCode:  d.StatusCode,
		Attempts:    d.Attempts,
		MaxAttempts: d.MaxAttempts,
		LastError:   d.LastError,
		DeliveredAt: d.DeliveredAt,
		CreatedAt:   d.CreatedAt,
	}
	// next_retry_at is only meaningful while the delivery is still being retried.
	if d.Status == domain.WebhookDeliveryPending || d.Status == domain.WebhookDeliveryDelivering {
		nr := d.NextRetryAt
		resp.NextRetryAt = &nr
	}
	return resp
}

// Cursor is an opaque base64 of "<unixNano>|<id>".
func encodeDeliveryCursor(t time.Time, id string) string {
	raw := strconv.FormatInt(t.UnixNano(), 10) + "|" + id
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeDeliveryCursor(s string) (time.Time, string, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return time.Time{}, "", false
	}
	parts := strings.SplitN(string(raw), "|", 2)
	if len(parts) != 2 {
		return time.Time{}, "", false
	}
	nanos, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return time.Time{}, "", false
	}
	return time.Unix(0, nanos).UTC(), parts[1], true
}
