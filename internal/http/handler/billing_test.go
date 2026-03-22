package handler_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/http/handler"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/testutil"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/usecase"
	"github.com/gin-gonic/gin"
)

func setupBillingRouter(creditRepo *testutil.MockCreditRepository) *gin.Engine {
	uc := usecase.NewBillingUsecase(
		creditRepo,
		&testutil.MockStripeCustomerRepository{},
		&testutil.MockUserRepository{},
		nil, // stripe client not needed for GetBalance / ListTransactions
		usecase.BillingConfig{CreditsPerDollar: 100000},
		slog.Default(),
	)
	h := handler.NewBillingHandler(uc, slog.Default())

	r := gin.New()
	g := r.Group("/", injectUser(testUserID))
	g.GET("/billing/balance", h.GetBalance)
	g.GET("/billing/transactions", h.ListTransactions)
	return r
}

func TestGetBalance_HappyPath(t *testing.T) {
	creditRepo := &testutil.MockCreditRepository{
		GetBalanceFn: func(_ context.Context, userID string) (*domain.CreditBalance, error) {
			return &domain.CreditBalance{
				UserID:         userID,
				Balance:        50000,
				Plan:           domain.PlanFree,
				DailyFreeLimit: 100000,
				RefreshedAt:    time.Now().UTC(),
				UpdatedAt:      time.Now().UTC(),
			}, nil
		},
	}
	r := setupBillingRouter(creditRepo)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/billing/balance", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// balance should be 50000
	if bal, ok := resp["balance"].(float64); !ok || int64(bal) != 50000 {
		t.Errorf("balance = %v, want 50000", resp["balance"])
	}
	if resp["plan"] != string(domain.PlanFree) {
		t.Errorf("plan = %v, want %q", resp["plan"], domain.PlanFree)
	}
	if dl, ok := resp["daily_limit"].(float64); !ok || int(dl) != 100000 {
		t.Errorf("daily_limit = %v, want 100000", resp["daily_limit"])
	}
	if cpd, ok := resp["credits_per_dollar"].(float64); !ok || int64(cpd) != 100000 {
		t.Errorf("credits_per_dollar = %v, want 100000", resp["credits_per_dollar"])
	}
}

func TestListTransactions_HappyPath(t *testing.T) {
	now := time.Now().UTC()
	jobID := "job-1"
	creditRepo := &testutil.MockCreditRepository{
		ListTransactionsFn: func(_ context.Context, _ string, _ string, _ int) ([]domain.CreditTransaction, string, error) {
			return []domain.CreditTransaction{
				{
					ID:        "tx-1",
					Amount:    -1,
					Type:      domain.CreditTxJobExecution,
					JobID:     &jobID,
					CreatedAt: now,
				},
				{
					ID:        "tx-2",
					Amount:    100000,
					Type:      domain.CreditTxDailyGrant,
					CreatedAt: now,
				},
			}, "", nil
		},
	}
	r := setupBillingRouter(creditRepo)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/billing/transactions", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	txs, ok := resp["transactions"].([]interface{})
	if !ok {
		t.Fatal("response missing transactions array")
	}
	if len(txs) != 2 {
		t.Errorf("len(transactions) = %d, want 2", len(txs))
	}
}
