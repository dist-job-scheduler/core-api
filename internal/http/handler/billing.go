package handler

import (
	"io"
	"log/slog"
	"net/http"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/usecase"
	"github.com/gin-gonic/gin"
)

// BillingHandler serves the /billing/* routes.
type BillingHandler struct {
	uc     *usecase.BillingUsecase
	logger *slog.Logger
}

func NewBillingHandler(uc *usecase.BillingUsecase, logger *slog.Logger) *BillingHandler {
	return &BillingHandler{
		uc:     uc,
		logger: logger.With("component", "billing_handler"),
	}
}

// GetBalance returns the authenticated user's current credit balance.
// GET /billing/balance
func (h *BillingHandler) GetBalance(c *gin.Context) {
	userID := c.GetString("userID")

	balance, err := h.uc.GetBalance(c.Request.Context(), userID)
	if err != nil {
		h.logger.ErrorContext(c.Request.Context(), "get balance", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": errInternalServer})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"balance":     balance.Balance,
		"plan":        balance.Plan,
		"daily_limit": balance.DailyFreeLimit,
	})
}

// CreateCheckoutSession creates a Stripe Checkout Session for a credit pack purchase.
// POST /billing/checkout
// Body: {"pack":"starter"}
func (h *BillingHandler) CreateCheckoutSession(c *gin.Context) {
	userID := c.GetString("userID")

	var req struct {
		Pack string `json:"pack" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	url, err := h.uc.CreateCheckoutSession(c.Request.Context(), userID, req.Pack)
	if err != nil {
		h.logger.ErrorContext(c.Request.Context(), "create checkout session", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": errInternalServer})
		return
	}

	c.JSON(http.StatusOK, gin.H{"url": url})
}

// HandleWebhook processes Stripe webhook events.
// POST /billing/webhook  (no auth middleware — verified by Stripe signature)
func (h *BillingHandler) HandleWebhook(c *gin.Context) {
	payload, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot read body"})
		return
	}
	defer func() { _ = c.Request.Body.Close() }()

	sig := c.GetHeader("Stripe-Signature")
	if sig == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing Stripe-Signature header"})
		return
	}

	if err := h.uc.HandleWebhook(c.Request.Context(), payload, sig); err != nil {
		h.logger.ErrorContext(c.Request.Context(), "handle webhook", "error", err)
		// Return 400 so Stripe retries invalid-signature errors; 500 for transient failures.
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.Status(http.StatusOK)
}
