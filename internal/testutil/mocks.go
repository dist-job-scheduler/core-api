package testutil

import (
	"context"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/repository"
)

// ---------- MockJobRepository ----------

type MockJobRepository struct {
	CreateFn           func(ctx context.Context, job *domain.Job) (*domain.Job, error)
	GetByIDFn          func(ctx context.Context, jobID, userID string) (*domain.Job, error)
	ListJobsFn         func(ctx context.Context, input repository.ListJobsInput) ([]*domain.Job, error)
	CancelFn           func(ctx context.Context, jobID, userID string) error
	ClaimFn            func(ctx context.Context, workerID string, limit int) ([]*domain.Job, error)
	UpdateHeartbeatFn  func(ctx context.Context, jobID string) error
	CompleteFn         func(ctx context.Context, jobID string) error
	FailFn             func(ctx context.Context, jobID string, lastError string) error
	RescheduleFn       func(ctx context.Context, jobID string, lastError string, retryAt time.Time) error
	ReschedulesStaleFn func(ctx context.Context, staleCutoff time.Time, limit int) (int, error)
	FailStaleFn        func(ctx context.Context, staleCutoff time.Time, limit int) (int, error)
	ListByScheduleIDFn func(ctx context.Context, scheduleID string, limit int, cursorTime *time.Time, cursorID string) ([]*domain.Job, error)
}

func (m *MockJobRepository) Create(ctx context.Context, job *domain.Job) (*domain.Job, error) {
	if m.CreateFn != nil {
		return m.CreateFn(ctx, job)
	}
	job.ID = "job-1"
	job.CreatedAt = time.Now()
	return job, nil
}

func (m *MockJobRepository) GetByID(ctx context.Context, jobID, userID string) (*domain.Job, error) {
	if m.GetByIDFn != nil {
		return m.GetByIDFn(ctx, jobID, userID)
	}
	return nil, domain.ErrJobNotFound
}

func (m *MockJobRepository) ListJobs(ctx context.Context, input repository.ListJobsInput) ([]*domain.Job, error) {
	if m.ListJobsFn != nil {
		return m.ListJobsFn(ctx, input)
	}
	return nil, nil
}

func (m *MockJobRepository) Cancel(ctx context.Context, jobID, userID string) error {
	if m.CancelFn != nil {
		return m.CancelFn(ctx, jobID, userID)
	}
	return nil
}

func (m *MockJobRepository) Claim(ctx context.Context, workerID string, limit int) ([]*domain.Job, error) {
	if m.ClaimFn != nil {
		return m.ClaimFn(ctx, workerID, limit)
	}
	return nil, nil
}

func (m *MockJobRepository) UpdateHeartbeat(ctx context.Context, jobID string) error {
	if m.UpdateHeartbeatFn != nil {
		return m.UpdateHeartbeatFn(ctx, jobID)
	}
	return nil
}

func (m *MockJobRepository) Complete(ctx context.Context, jobID string) error {
	if m.CompleteFn != nil {
		return m.CompleteFn(ctx, jobID)
	}
	return nil
}

func (m *MockJobRepository) Fail(ctx context.Context, jobID string, lastError string) error {
	if m.FailFn != nil {
		return m.FailFn(ctx, jobID, lastError)
	}
	return nil
}

func (m *MockJobRepository) Reschedule(ctx context.Context, jobID string, lastError string, retryAt time.Time) error {
	if m.RescheduleFn != nil {
		return m.RescheduleFn(ctx, jobID, lastError, retryAt)
	}
	return nil
}

func (m *MockJobRepository) RescheduleStale(ctx context.Context, staleCutoff time.Time, limit int) (int, error) {
	if m.ReschedulesStaleFn != nil {
		return m.ReschedulesStaleFn(ctx, staleCutoff, limit)
	}
	return 0, nil
}

func (m *MockJobRepository) FailStale(ctx context.Context, staleCutoff time.Time, limit int) (int, error) {
	if m.FailStaleFn != nil {
		return m.FailStaleFn(ctx, staleCutoff, limit)
	}
	return 0, nil
}

func (m *MockJobRepository) ListByScheduleID(ctx context.Context, scheduleID string, limit int, cursorTime *time.Time, cursorID string) ([]*domain.Job, error) {
	if m.ListByScheduleIDFn != nil {
		return m.ListByScheduleIDFn(ctx, scheduleID, limit, cursorTime, cursorID)
	}
	return nil, nil
}

// ---------- MockAttemptRepository ----------

type MockAttemptRepository struct {
	CreateAttemptFn   func(ctx context.Context, attempt *domain.JobAttempt) (*domain.JobAttempt, error)
	CompleteAttemptFn func(ctx context.Context, id string, startedAt time.Time, statusCode *int, errMsg *string, durationMS int64) error
	ListByJobIDFn     func(ctx context.Context, jobID string) ([]*domain.JobAttempt, error)
}

func (m *MockAttemptRepository) CreateAttempt(ctx context.Context, attempt *domain.JobAttempt) (*domain.JobAttempt, error) {
	if m.CreateAttemptFn != nil {
		return m.CreateAttemptFn(ctx, attempt)
	}
	attempt.ID = "attempt-1"
	return attempt, nil
}

func (m *MockAttemptRepository) CompleteAttempt(ctx context.Context, id string, startedAt time.Time, statusCode *int, errMsg *string, durationMS int64) error {
	if m.CompleteAttemptFn != nil {
		return m.CompleteAttemptFn(ctx, id, startedAt, statusCode, errMsg, durationMS)
	}
	return nil
}

func (m *MockAttemptRepository) ListByJobID(ctx context.Context, jobID string) ([]*domain.JobAttempt, error) {
	if m.ListByJobIDFn != nil {
		return m.ListByJobIDFn(ctx, jobID)
	}
	return nil, nil
}

// ---------- MockCreditRepository ----------

type MockCreditRepository struct {
	EnsureExistsFn        func(ctx context.Context, userID string) error
	GetBalanceFn          func(ctx context.Context, userID string) (*domain.CreditBalance, error)
	HasCreditsFn          func(ctx context.Context, userID string) (bool, error)
	DeductFn              func(ctx context.Context, userID, jobID string) error
	DeductForBufferItemFn func(ctx context.Context, userID, bufferItemID string) error
	TopUpFn               func(ctx context.Context, userID string, amount int64, stripePaymentIntentID string) error
	UpdatePlanFn          func(ctx context.Context, userID string, plan domain.Plan) error
	ListTransactionsFn    func(ctx context.Context, userID string, cursor string, limit int) ([]domain.CreditTransaction, string, error)
}

func (m *MockCreditRepository) EnsureExists(ctx context.Context, userID string) error {
	if m.EnsureExistsFn != nil {
		return m.EnsureExistsFn(ctx, userID)
	}
	return nil
}

func (m *MockCreditRepository) GetBalance(ctx context.Context, userID string) (*domain.CreditBalance, error) {
	if m.GetBalanceFn != nil {
		return m.GetBalanceFn(ctx, userID)
	}
	return &domain.CreditBalance{UserID: userID, Balance: 100000, Plan: domain.PlanFree, DailyFreeLimit: 100000}, nil
}

func (m *MockCreditRepository) HasCredits(ctx context.Context, userID string) (bool, error) {
	if m.HasCreditsFn != nil {
		return m.HasCreditsFn(ctx, userID)
	}
	return true, nil
}

func (m *MockCreditRepository) Deduct(ctx context.Context, userID, jobID string) error {
	if m.DeductFn != nil {
		return m.DeductFn(ctx, userID, jobID)
	}
	return nil
}

func (m *MockCreditRepository) DeductForBufferItem(ctx context.Context, userID, bufferItemID string) error {
	if m.DeductForBufferItemFn != nil {
		return m.DeductForBufferItemFn(ctx, userID, bufferItemID)
	}
	return nil
}

func (m *MockCreditRepository) TopUp(ctx context.Context, userID string, amount int64, stripePaymentIntentID string) error {
	if m.TopUpFn != nil {
		return m.TopUpFn(ctx, userID, amount, stripePaymentIntentID)
	}
	return nil
}

func (m *MockCreditRepository) UpdatePlan(ctx context.Context, userID string, plan domain.Plan) error {
	if m.UpdatePlanFn != nil {
		return m.UpdatePlanFn(ctx, userID, plan)
	}
	return nil
}

func (m *MockCreditRepository) ListTransactions(ctx context.Context, userID string, cursor string, limit int) ([]domain.CreditTransaction, string, error) {
	if m.ListTransactionsFn != nil {
		return m.ListTransactionsFn(ctx, userID, cursor, limit)
	}
	return nil, "", nil
}

// ---------- MockScheduleRepository ----------

type MockScheduleRepository struct {
	CreateFn       func(ctx context.Context, s *domain.Schedule) (*domain.Schedule, error)
	GetByIDFn      func(ctx context.Context, id, userID string) (*domain.Schedule, error)
	ListFn         func(ctx context.Context, input repository.ListSchedulesInput) ([]*domain.Schedule, error)
	SetPausedFn    func(ctx context.Context, id, userID string, paused bool) error
	DeleteFn       func(ctx context.Context, id, userID string) error
	ClaimAndFireFn func(ctx context.Context, limit int, computeNext func(*domain.Schedule) time.Time, creditFilter func(ctx context.Context, userID string) bool) ([]*domain.Job, error)
}

func (m *MockScheduleRepository) Create(ctx context.Context, s *domain.Schedule) (*domain.Schedule, error) {
	if m.CreateFn != nil {
		return m.CreateFn(ctx, s)
	}
	s.ID = "sched-1"
	s.CreatedAt = time.Now()
	return s, nil
}

func (m *MockScheduleRepository) GetByID(ctx context.Context, id, userID string) (*domain.Schedule, error) {
	if m.GetByIDFn != nil {
		return m.GetByIDFn(ctx, id, userID)
	}
	return nil, domain.ErrScheduleNotFound
}

func (m *MockScheduleRepository) List(ctx context.Context, input repository.ListSchedulesInput) ([]*domain.Schedule, error) {
	if m.ListFn != nil {
		return m.ListFn(ctx, input)
	}
	return nil, nil
}

func (m *MockScheduleRepository) SetPaused(ctx context.Context, id, userID string, paused bool) error {
	if m.SetPausedFn != nil {
		return m.SetPausedFn(ctx, id, userID, paused)
	}
	return nil
}

func (m *MockScheduleRepository) Delete(ctx context.Context, id, userID string) error {
	if m.DeleteFn != nil {
		return m.DeleteFn(ctx, id, userID)
	}
	return nil
}

func (m *MockScheduleRepository) ClaimAndFire(ctx context.Context, limit int, computeNext func(*domain.Schedule) time.Time, creditFilter func(ctx context.Context, userID string) bool) ([]*domain.Job, error) {
	if m.ClaimAndFireFn != nil {
		return m.ClaimAndFireFn(ctx, limit, computeNext, creditFilter)
	}
	return nil, nil
}

// ---------- MockAPITokenRepository ----------

type MockAPITokenRepository struct {
	CreateFn          func(ctx context.Context, userID, name, tokenHash, prefix string) (*domain.APIToken, error)
	FindByTokenHashFn func(ctx context.Context, tokenHash string) (*domain.APIToken, error)
	ListByUserIDFn    func(ctx context.Context, userID string) ([]*domain.APIToken, error)
	DeleteFn          func(ctx context.Context, id, userID string) error
	UpdateLastUsedFn  func(ctx context.Context, id string) error
}

func (m *MockAPITokenRepository) Create(ctx context.Context, userID, name, tokenHash, prefix string) (*domain.APIToken, error) {
	if m.CreateFn != nil {
		return m.CreateFn(ctx, userID, name, tokenHash, prefix)
	}
	return &domain.APIToken{ID: "tok-1", UserID: userID, Name: name, Prefix: prefix, CreatedAt: time.Now()}, nil
}

func (m *MockAPITokenRepository) FindByTokenHash(ctx context.Context, tokenHash string) (*domain.APIToken, error) {
	if m.FindByTokenHashFn != nil {
		return m.FindByTokenHashFn(ctx, tokenHash)
	}
	return nil, domain.ErrTokenNotFound
}

func (m *MockAPITokenRepository) ListByUserID(ctx context.Context, userID string) ([]*domain.APIToken, error) {
	if m.ListByUserIDFn != nil {
		return m.ListByUserIDFn(ctx, userID)
	}
	return nil, nil
}

func (m *MockAPITokenRepository) Delete(ctx context.Context, id, userID string) error {
	if m.DeleteFn != nil {
		return m.DeleteFn(ctx, id, userID)
	}
	return nil
}

func (m *MockAPITokenRepository) UpdateLastUsed(ctx context.Context, id string) error {
	if m.UpdateLastUsedFn != nil {
		return m.UpdateLastUsedFn(ctx, id)
	}
	return nil
}

// ---------- MockUserRepository ----------

type MockUserRepository struct {
	UpsertFn   func(ctx context.Context, clerkID string) error
	FindByIDFn func(ctx context.Context, id string) (*domain.User, error)
}

func (m *MockUserRepository) Upsert(ctx context.Context, clerkID string) error {
	if m.UpsertFn != nil {
		return m.UpsertFn(ctx, clerkID)
	}
	return nil
}

func (m *MockUserRepository) FindByID(ctx context.Context, id string) (*domain.User, error) {
	if m.FindByIDFn != nil {
		return m.FindByIDFn(ctx, id)
	}
	return &domain.User{ID: id, CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil
}

// ---------- MockStripeCustomerRepository ----------

type MockStripeCustomerRepository struct {
	FindByUserIDFn func(ctx context.Context, userID string) (string, error)
	SaveFn         func(ctx context.Context, userID, stripeCustomerID string) error
}

func (m *MockStripeCustomerRepository) FindByUserID(ctx context.Context, userID string) (string, error) {
	if m.FindByUserIDFn != nil {
		return m.FindByUserIDFn(ctx, userID)
	}
	return "", nil
}

func (m *MockStripeCustomerRepository) Save(ctx context.Context, userID, stripeCustomerID string) error {
	if m.SaveFn != nil {
		return m.SaveFn(ctx, userID, stripeCustomerID)
	}
	return nil
}

// ---------- MockAlertChannelRepository ----------

type MockAlertChannelRepository struct {
	CreateFn      func(ctx context.Context, ch *domain.AlertChannel) (*domain.AlertChannel, error)
	GetByIDFn     func(ctx context.Context, id, userID string) (*domain.AlertChannel, error)
	ListFn        func(ctx context.Context, userID string) ([]*domain.AlertChannel, error)
	SetEnabledFn  func(ctx context.Context, id, userID string, enabled bool) error
	DeleteFn      func(ctx context.Context, id, userID string) error
	ListEnabledFn func(ctx context.Context, userID string) ([]*domain.AlertChannel, error)
}

func (m *MockAlertChannelRepository) Create(ctx context.Context, ch *domain.AlertChannel) (*domain.AlertChannel, error) {
	if m.CreateFn != nil {
		return m.CreateFn(ctx, ch)
	}
	ch.ID = "alert-1"
	ch.CreatedAt = time.Now()
	ch.UpdatedAt = ch.CreatedAt
	return ch, nil
}

func (m *MockAlertChannelRepository) GetByID(ctx context.Context, id, userID string) (*domain.AlertChannel, error) {
	if m.GetByIDFn != nil {
		return m.GetByIDFn(ctx, id, userID)
	}
	return nil, domain.ErrAlertChannelNotFound
}

func (m *MockAlertChannelRepository) List(ctx context.Context, userID string) ([]*domain.AlertChannel, error) {
	if m.ListFn != nil {
		return m.ListFn(ctx, userID)
	}
	return nil, nil
}

func (m *MockAlertChannelRepository) SetEnabled(ctx context.Context, id, userID string, enabled bool) error {
	if m.SetEnabledFn != nil {
		return m.SetEnabledFn(ctx, id, userID, enabled)
	}
	return nil
}

func (m *MockAlertChannelRepository) Delete(ctx context.Context, id, userID string) error {
	if m.DeleteFn != nil {
		return m.DeleteFn(ctx, id, userID)
	}
	return nil
}

func (m *MockAlertChannelRepository) ListEnabled(ctx context.Context, userID string) ([]*domain.AlertChannel, error) {
	if m.ListEnabledFn != nil {
		return m.ListEnabledFn(ctx, userID)
	}
	return nil, nil
}

// ---------- MockStatsRepository ----------

type MockStatsRepository struct {
	JobStatsFn    func(ctx context.Context, userID string, since time.Time) (domain.JobStats, error)
	UsageSeriesFn func(ctx context.Context, userID string, since time.Time) ([]domain.UsageBucket, error)
}

func (m *MockStatsRepository) JobStats(ctx context.Context, userID string, since time.Time) (domain.JobStats, error) {
	if m.JobStatsFn != nil {
		return m.JobStatsFn(ctx, userID, since)
	}
	return domain.JobStats{}, nil
}

func (m *MockStatsRepository) UsageSeries(ctx context.Context, userID string, since time.Time) ([]domain.UsageBucket, error) {
	if m.UsageSeriesFn != nil {
		return m.UsageSeriesFn(ctx, userID, since)
	}
	return nil, nil
}

// ---------- MockBufferRepository ----------

// MockBufferRepository implements repository.BufferRepository. Only the methods
// exercised by a given test need a *Fn hook; the rest return zero values.
type MockBufferRepository struct {
	CreateFn          func(ctx context.Context, b *domain.Buffer) (*domain.Buffer, error)
	GetByIDFn         func(ctx context.Context, id, userID string) (*domain.Buffer, error)
	GetByIDInternalFn func(ctx context.Context, id string) (*domain.Buffer, error)
	ListFn            func(ctx context.Context, input repository.ListBuffersInput) ([]*domain.Buffer, error)
	SetPausedFn       func(ctx context.Context, id, userID string, paused bool) error
	DeleteFn          func(ctx context.Context, id, userID string) error
	BufferStatsFn     func(ctx context.Context, bufferID string) (domain.BufferStats, error)
	CreateItemFn      func(ctx context.Context, item *domain.BufferItem) (*domain.BufferItem, error)
	GetItemByIDFn     func(ctx context.Context, itemID, bufferID string) (*domain.BufferItem, error)
	ListItemsFn       func(ctx context.Context, input repository.ListBufferItemsInput) ([]*domain.BufferItem, error)
}

func (m *MockBufferRepository) Create(ctx context.Context, b *domain.Buffer) (*domain.Buffer, error) {
	if m.CreateFn != nil {
		return m.CreateFn(ctx, b)
	}
	b.ID = "buffer-1"
	return b, nil
}

func (m *MockBufferRepository) GetByID(ctx context.Context, id, userID string) (*domain.Buffer, error) {
	if m.GetByIDFn != nil {
		return m.GetByIDFn(ctx, id, userID)
	}
	return nil, domain.ErrBufferNotFound
}

func (m *MockBufferRepository) GetByIDInternal(ctx context.Context, id string) (*domain.Buffer, error) {
	if m.GetByIDInternalFn != nil {
		return m.GetByIDInternalFn(ctx, id)
	}
	return nil, domain.ErrBufferNotFound
}

func (m *MockBufferRepository) List(ctx context.Context, input repository.ListBuffersInput) ([]*domain.Buffer, error) {
	if m.ListFn != nil {
		return m.ListFn(ctx, input)
	}
	return nil, nil
}

func (m *MockBufferRepository) SetPaused(ctx context.Context, id, userID string, paused bool) error {
	if m.SetPausedFn != nil {
		return m.SetPausedFn(ctx, id, userID, paused)
	}
	return nil
}

func (m *MockBufferRepository) Delete(ctx context.Context, id, userID string) error {
	if m.DeleteFn != nil {
		return m.DeleteFn(ctx, id, userID)
	}
	return nil
}

func (m *MockBufferRepository) BufferStats(ctx context.Context, bufferID string) (domain.BufferStats, error) {
	if m.BufferStatsFn != nil {
		return m.BufferStatsFn(ctx, bufferID)
	}
	return domain.BufferStats{}, nil
}

func (m *MockBufferRepository) CreateItem(ctx context.Context, item *domain.BufferItem) (*domain.BufferItem, error) {
	if m.CreateItemFn != nil {
		return m.CreateItemFn(ctx, item)
	}
	item.ID = "item-1"
	return item, nil
}

func (m *MockBufferRepository) GetItemByID(ctx context.Context, itemID, bufferID string) (*domain.BufferItem, error) {
	if m.GetItemByIDFn != nil {
		return m.GetItemByIDFn(ctx, itemID, bufferID)
	}
	return nil, domain.ErrBufferItemNotFound
}

func (m *MockBufferRepository) ListItems(ctx context.Context, input repository.ListBufferItemsInput) ([]*domain.BufferItem, error) {
	if m.ListItemsFn != nil {
		return m.ListItemsFn(ctx, input)
	}
	return nil, nil
}

func (m *MockBufferRepository) ListActiveBufferIDs(ctx context.Context, limit int) ([]string, error) {
	return nil, nil
}

func (m *MockBufferRepository) ClaimNextItem(ctx context.Context, bufferID, workerID string) (*domain.BufferItem, error) {
	return nil, nil
}

func (m *MockBufferRepository) UpdateItemHeartbeat(ctx context.Context, itemID string) error {
	return nil
}

func (m *MockBufferRepository) CompleteItem(ctx context.Context, itemID string, statusCode int) error {
	return nil
}

func (m *MockBufferRepository) FailItem(ctx context.Context, itemID string, lastError string, statusCode *int) error {
	return nil
}

func (m *MockBufferRepository) RescheduleItem(ctx context.Context, itemID string, lastError string, statusCode *int, retryAt time.Time) error {
	return nil
}

func (m *MockBufferRepository) RescheduleStaleItems(ctx context.Context, staleCutoff time.Time, limit int) (int, error) {
	return 0, nil
}

func (m *MockBufferRepository) FailStaleItems(ctx context.Context, staleCutoff time.Time, limit int) (int, error) {
	return 0, nil
}

// ---------- MockSigningSecretRepository ----------

type MockSigningSecretRepository struct {
	GetActiveFn func(ctx context.Context, userID string) (*domain.SigningSecret, error)
	CreateFn    func(ctx context.Context, userID string) (*domain.SigningSecret, error)
	RotateFn    func(ctx context.Context, userID string) (*domain.SigningSecret, error)
}

func (m *MockSigningSecretRepository) GetActive(ctx context.Context, userID string) (*domain.SigningSecret, error) {
	if m.GetActiveFn != nil {
		return m.GetActiveFn(ctx, userID)
	}
	return nil, domain.ErrSigningSecretNotFound
}

func (m *MockSigningSecretRepository) Create(ctx context.Context, userID string) (*domain.SigningSecret, error) {
	if m.CreateFn != nil {
		return m.CreateFn(ctx, userID)
	}
	return &domain.SigningSecret{ID: "sig-1", UserID: userID, Secret: "test-secret-32chars-for-signing!", IsActive: true, CreatedAt: time.Now()}, nil
}

func (m *MockSigningSecretRepository) Rotate(ctx context.Context, userID string) (*domain.SigningSecret, error) {
	if m.RotateFn != nil {
		return m.RotateFn(ctx, userID)
	}
	return &domain.SigningSecret{ID: "sig-1", UserID: userID, Secret: "test-secret-32chars-for-signing!", IsActive: true, CreatedAt: time.Now()}, nil
}
