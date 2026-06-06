package repository

import (
	"context"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
)

type ListBuffersInput struct {
	UserID     string
	CursorTime *time.Time
	CursorID   string
	Limit      int
}

type ListBufferItemsInput struct {
	BufferID   string
	Status     domain.BufferItemStatus // empty = all
	CursorTime *time.Time
	CursorID   string
	Limit      int
}

type BufferRepository interface {
	// CRUD
	Create(ctx context.Context, b *domain.Buffer) (*domain.Buffer, error)
	GetByID(ctx context.Context, id, userID string) (*domain.Buffer, error)
	GetByIDInternal(ctx context.Context, id string) (*domain.Buffer, error)
	List(ctx context.Context, input ListBuffersInput) ([]*domain.Buffer, error)
	SetPaused(ctx context.Context, id, userID string, paused bool) error
	Delete(ctx context.Context, id, userID string) error

	// Item CRUD
	CreateItem(ctx context.Context, item *domain.BufferItem) (*domain.BufferItem, error)
	GetItemByID(ctx context.Context, itemID, bufferID string) (*domain.BufferItem, error)
	ListItems(ctx context.Context, input ListBufferItemsInput) ([]*domain.BufferItem, error)

	// Drainer-facing: find non-paused buffers that have pending items with scheduled_at <= now
	ListActiveBufferIDs(ctx context.Context, limit int) ([]string, error)
	// ClaimNextItem claims the single oldest due item for a buffer, but only if
	// the buffer has no item currently running. This enforces strict per-buffer
	// (per-client) ordering: at most one item in flight, and a retrying item
	// (pending with a future scheduled_at) blocks its successors until terminal.
	// Returns (nil, nil) when there is nothing to claim.
	ClaimNextItem(ctx context.Context, bufferID, workerID string) (*domain.BufferItem, error)
	UpdateItemHeartbeat(ctx context.Context, itemID string) error
	CompleteItem(ctx context.Context, itemID string, statusCode int) error
	FailItem(ctx context.Context, itemID string, lastError string, statusCode *int) error
	RescheduleItem(ctx context.Context, itemID string, lastError string, statusCode *int, retryAt time.Time) error

	// Reaper methods — recover items from crashed drainers
	RescheduleStaleItems(ctx context.Context, staleCutoff time.Time, limit int) (int, error)
	FailStaleItems(ctx context.Context, staleCutoff time.Time, limit int) (int, error)
}
