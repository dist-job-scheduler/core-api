package usecase

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/repository"
)

type BufferUsecase struct {
	repo    repository.BufferRepository
	credits repository.CreditRepository
}

func NewBufferUsecase(repo repository.BufferRepository, credits repository.CreditRepository) *BufferUsecase {
	return &BufferUsecase{repo: repo, credits: credits}
}

type CreateBufferInput struct {
	UserID         string
	Name           string
	URL            string
	Method         string
	Headers        map[string]string
	TimeoutSeconds int
	RateLimit      int
	MaxRetries     *int
	Backoff        domain.Backoff
	WebhookURL     *string
	WebhookHeaders map[string]string
}

func (u *BufferUsecase) CreateBuffer(ctx context.Context, input CreateBufferInput) (*domain.Buffer, error) {
	if input.Headers == nil {
		input.Headers = make(map[string]string)
	}
	if input.WebhookHeaders == nil {
		input.WebhookHeaders = make(map[string]string)
	}
	if input.TimeoutSeconds == 0 {
		input.TimeoutSeconds = 30
	}
	if input.MaxRetries == nil {
		defaultRetries := 3
		input.MaxRetries = &defaultRetries
	}
	if input.Backoff == "" {
		input.Backoff = domain.BackoffExponential
	}
	if input.RateLimit == 0 {
		input.RateLimit = 10
	}

	b := &domain.Buffer{
		UserID:         input.UserID,
		Name:           input.Name,
		URL:            input.URL,
		Method:         input.Method,
		Headers:        input.Headers,
		TimeoutSeconds: input.TimeoutSeconds,
		RateLimit:      input.RateLimit,
		MaxRetries:     *input.MaxRetries,
		Backoff:        input.Backoff,
		Paused:         false,
		WebhookURL:     input.WebhookURL,
		WebhookHeaders: input.WebhookHeaders,
	}

	created, err := u.repo.Create(ctx, b)
	if err != nil {
		return nil, fmt.Errorf("create buffer: %w", err)
	}
	return created, nil
}

func (u *BufferUsecase) GetBuffer(ctx context.Context, id, userID string) (*domain.Buffer, error) {
	b, err := u.repo.GetByID(ctx, id, userID)
	if err != nil {
		return nil, fmt.Errorf("get buffer: %w", err)
	}
	return b, nil
}

type ListBuffersInput struct {
	UserID string
	Cursor string
	Limit  int
}

type ListBuffersResult struct {
	Buffers    []*domain.Buffer
	NextCursor *string
}

type bufferCursor struct {
	CreatedAt time.Time `json:"c"`
	ID        string    `json:"i"`
}

func decodeBufferCursor(s string) (*time.Time, string, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, "", fmt.Errorf("decode cursor: %w", err)
	}
	var c bufferCursor
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, "", fmt.Errorf("unmarshal cursor: %w", err)
	}
	return &c.CreatedAt, c.ID, nil
}

func encodeBufferCursor(createdAt time.Time, id string) string {
	b, _ := json.Marshal(bufferCursor{CreatedAt: createdAt, ID: id})
	return base64.RawURLEncoding.EncodeToString(b)
}

func (u *BufferUsecase) ListBuffers(ctx context.Context, input ListBuffersInput) (ListBuffersResult, error) {
	limit := input.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	repoInput := repository.ListBuffersInput{
		UserID: input.UserID,
		Limit:  limit + 1,
	}

	if input.Cursor != "" {
		cursorTime, cursorID, err := decodeBufferCursor(input.Cursor)
		if err != nil {
			return ListBuffersResult{}, fmt.Errorf("bad cursor: %w", err)
		}
		repoInput.CursorTime = cursorTime
		repoInput.CursorID = cursorID
	}

	buffers, err := u.repo.List(ctx, repoInput)
	if err != nil {
		return ListBuffersResult{}, fmt.Errorf("list buffers: %w", err)
	}

	var nextCursor *string
	if len(buffers) == limit+1 {
		last := buffers[limit]
		s := encodeBufferCursor(last.CreatedAt, last.ID)
		nextCursor = &s
		buffers = buffers[:limit]
	}

	return ListBuffersResult{Buffers: buffers, NextCursor: nextCursor}, nil
}

func (u *BufferUsecase) PauseBuffer(ctx context.Context, id, userID string) error {
	if err := u.repo.SetPaused(ctx, id, userID, true); err != nil {
		return fmt.Errorf("pause buffer: %w", err)
	}
	return nil
}

func (u *BufferUsecase) ResumeBuffer(ctx context.Context, id, userID string) error {
	if err := u.repo.SetPaused(ctx, id, userID, false); err != nil {
		return fmt.Errorf("resume buffer: %w", err)
	}
	return nil
}

func (u *BufferUsecase) DeleteBuffer(ctx context.Context, id, userID string) error {
	if err := u.repo.Delete(ctx, id, userID); err != nil {
		return fmt.Errorf("delete buffer: %w", err)
	}
	return nil
}

// ── Items ────────────────────────────────────────────────────────────────────

type PushBufferItemInput struct {
	UserID   string
	BufferID string
	Body     *string
	Headers  map[string]string
}

func (u *BufferUsecase) PushItem(ctx context.Context, input PushBufferItemInput) (*domain.BufferItem, error) {
	ok, err := u.credits.HasCredits(ctx, input.UserID)
	if err != nil {
		return nil, fmt.Errorf("check credits: %w", err)
	}
	if !ok {
		return nil, domain.ErrInsufficientCredits
	}

	buf, err := u.repo.GetByID(ctx, input.BufferID, input.UserID)
	if err != nil {
		return nil, fmt.Errorf("get buffer: %w", err)
	}

	// Merge headers: buffer defaults + per-item overrides
	merged := make(map[string]string, len(buf.Headers)+len(input.Headers))
	for k, v := range buf.Headers {
		merged[k] = v
	}
	for k, v := range input.Headers {
		merged[k] = v
	}

	item := &domain.BufferItem{
		BufferID:       buf.ID,
		UserID:         input.UserID,
		URL:            buf.URL,
		Method:         buf.Method,
		Headers:        merged,
		Body:           input.Body,
		TimeoutSeconds: buf.TimeoutSeconds,
		Backoff:        buf.Backoff,
		Status:         domain.BufferItemPending,
		MaxRetries:     buf.MaxRetries,
		ScheduledAt:    time.Now(),
	}

	created, err := u.repo.CreateItem(ctx, item)
	if err != nil {
		return nil, fmt.Errorf("create buffer item: %w", err)
	}
	return created, nil
}

func (u *BufferUsecase) GetItem(ctx context.Context, itemID, bufferID, userID string) (*domain.BufferItem, error) {
	// Verify buffer ownership
	if _, err := u.repo.GetByID(ctx, bufferID, userID); err != nil {
		return nil, fmt.Errorf("get buffer: %w", err)
	}
	item, err := u.repo.GetItemByID(ctx, itemID, bufferID)
	if err != nil {
		return nil, fmt.Errorf("get buffer item: %w", err)
	}
	return item, nil
}

type ListBufferItemsInput struct {
	BufferID string
	UserID   string
	Status   string
	Cursor   string
	Limit    int
}

type ListBufferItemsResult struct {
	Items      []*domain.BufferItem
	NextCursor *string
}

var validBufferItemStatuses = map[domain.BufferItemStatus]struct{}{
	domain.BufferItemPending:   {},
	domain.BufferItemRunning:   {},
	domain.BufferItemCompleted: {},
	domain.BufferItemFailed:    {},
}

func (u *BufferUsecase) ListItems(ctx context.Context, input ListBufferItemsInput) (ListBufferItemsResult, error) {
	// Verify buffer ownership
	if _, err := u.repo.GetByID(ctx, input.BufferID, input.UserID); err != nil {
		return ListBufferItemsResult{}, fmt.Errorf("get buffer: %w", err)
	}

	limit := input.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	var status domain.BufferItemStatus
	if input.Status != "" {
		status = domain.BufferItemStatus(input.Status)
		if _, ok := validBufferItemStatuses[status]; !ok {
			return ListBufferItemsResult{}, domain.ErrInvalidStatus
		}
	}

	repoInput := repository.ListBufferItemsInput{
		BufferID: input.BufferID,
		Status:   status,
		Limit:    limit + 1,
	}

	if input.Cursor != "" {
		cursorTime, cursorID, err := decodeBufferCursor(input.Cursor)
		if err != nil {
			return ListBufferItemsResult{}, fmt.Errorf("bad cursor: %w", err)
		}
		repoInput.CursorTime = cursorTime
		repoInput.CursorID = cursorID
	}

	items, err := u.repo.ListItems(ctx, repoInput)
	if err != nil {
		return ListBufferItemsResult{}, fmt.Errorf("list buffer items: %w", err)
	}

	var nextCursor *string
	if len(items) == limit+1 {
		last := items[limit]
		s := encodeBufferCursor(last.CreatedAt, last.ID)
		nextCursor = &s
		items = items[:limit]
	}

	return ListBufferItemsResult{Items: items, NextCursor: nextCursor}, nil
}
