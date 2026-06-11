package handler_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/http/handler"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/testutil"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/usecase"
	"github.com/gin-gonic/gin"
)

func setupBufferRouter(bufRepo *testutil.MockBufferRepository, creditRepo *testutil.MockCreditRepository) *gin.Engine {
	uc := usecase.NewBufferUsecase(bufRepo, creditRepo)
	h := handler.NewBufferHandler(uc, slog.Default())

	r := gin.New()
	g := r.Group("/", injectUser(testUserID))
	g.GET("/buffers/:id/stats", h.Stats)
	g.POST("/buffers/:id/items/:itemId/replay", h.ReplayItem)
	return r
}

func ownedBuffer(_ context.Context, id, uid string) (*domain.Buffer, error) {
	if uid == testUserID {
		return &domain.Buffer{ID: id, UserID: uid}, nil
	}
	return nil, domain.ErrBufferNotFound
}

func TestBufferStats_HappyPath(t *testing.T) {
	bufRepo := &testutil.MockBufferRepository{
		GetByIDFn: ownedBuffer,
		BufferStatsFn: func(_ context.Context, _ string) (domain.BufferStats, error) {
			return domain.BufferStats{Pending: 2, Running: 1, Completed: 9, Failed: 1, Total: 13, SuccessRate: 0.9}, nil
		},
	}
	r := setupBufferRouter(bufRepo, &testutil.MockCreditRepository{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/buffers/buf-1/stats", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusOK, w.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["total"].(float64) != 13 {
		t.Errorf("total = %v, want 13", resp["total"])
	}
	if resp["failed"].(float64) != 1 {
		t.Errorf("failed = %v, want 1", resp["failed"])
	}
}

func TestBufferStats_NotFound(t *testing.T) {
	bufRepo := &testutil.MockBufferRepository{
		GetByIDFn: func(_ context.Context, _, _ string) (*domain.Buffer, error) {
			return nil, domain.ErrBufferNotFound
		},
	}
	r := setupBufferRouter(bufRepo, &testutil.MockCreditRepository{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/buffers/missing/stats", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestReplayBufferItem_HappyPath(t *testing.T) {
	failed := &domain.BufferItem{ID: "item-1", BufferID: "buf-1", UserID: testUserID, URL: "https://x", Method: "POST", Status: domain.BufferItemFailed}
	bufRepo := &testutil.MockBufferRepository{
		GetByIDFn:     ownedBuffer,
		GetItemByIDFn: func(_ context.Context, _, _ string) (*domain.BufferItem, error) { return failed, nil },
		CreateItemFn: func(_ context.Context, it *domain.BufferItem) (*domain.BufferItem, error) {
			it.ID = "item-replay"
			return it, nil
		},
	}
	r := setupBufferRouter(bufRepo, &testutil.MockCreditRepository{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/buffers/buf-1/items/item-1/replay", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusCreated, w.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["id"] != "item-replay" {
		t.Errorf("id = %v, want item-replay", resp["id"])
	}
	if resp["replay_of"] != "item-1" {
		t.Errorf("replay_of = %v, want item-1", resp["replay_of"])
	}
}

func TestReplayBufferItem_NotReplayable(t *testing.T) {
	item := &domain.BufferItem{ID: "item-1", BufferID: "buf-1", UserID: testUserID, Status: domain.BufferItemCompleted}
	bufRepo := &testutil.MockBufferRepository{
		GetByIDFn:     ownedBuffer,
		GetItemByIDFn: func(_ context.Context, _, _ string) (*domain.BufferItem, error) { return item, nil },
	}
	r := setupBufferRouter(bufRepo, &testutil.MockCreditRepository{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/buffers/buf-1/items/item-1/replay", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d", w.Code, http.StatusConflict)
	}
}

func TestReplayBufferItem_InsufficientCredits(t *testing.T) {
	failed := &domain.BufferItem{ID: "item-1", BufferID: "buf-1", UserID: testUserID, Status: domain.BufferItemFailed}
	bufRepo := &testutil.MockBufferRepository{
		GetByIDFn:     ownedBuffer,
		GetItemByIDFn: func(_ context.Context, _, _ string) (*domain.BufferItem, error) { return failed, nil },
	}
	credits := &testutil.MockCreditRepository{
		HasCreditsFn: func(_ context.Context, _ string) (bool, error) { return false, nil },
	}
	r := setupBufferRouter(bufRepo, credits)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/buffers/buf-1/items/item-1/replay", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusPaymentRequired {
		t.Errorf("status = %d, want %d", w.Code, http.StatusPaymentRequired)
	}
}
