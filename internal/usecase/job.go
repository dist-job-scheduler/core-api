package usecase

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/repository"
	"github.com/google/uuid"
)

type JobUsecase struct {
	repo     repository.JobRepository
	attempts repository.AttemptRepository
	credits  repository.CreditRepository
}

func NewJobUsecase(repo repository.JobRepository, attempts repository.AttemptRepository, credits repository.CreditRepository) *JobUsecase {
	return &JobUsecase{repo: repo, attempts: attempts, credits: credits}
}

type CreateJobInput struct {
	UserID         string
	IdempotencyKey string
	URL            string
	Method         string
	Headers        map[string]string
	Body           *string
	TimeoutSeconds int
	ScheduledAt    time.Time
	MaxRetries     *int
	Backoff        domain.Backoff
	WebhookURL     *string
	WebhookHeaders map[string]string
}

func (u *JobUsecase) CreateJob(ctx context.Context, input CreateJobInput) (*domain.Job, error) {
	ok, err := u.credits.HasCredits(ctx, input.UserID)
	if err != nil {
		return nil, fmt.Errorf("check credits: %w", err)
	}
	if !ok {
		return nil, domain.ErrInsufficientCredits
	}

	if input.IdempotencyKey == "" {
		input.IdempotencyKey = uuid.New().String()
	}

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

	job := &domain.Job{
		UserID:         input.UserID,
		IdempotencyKey: input.IdempotencyKey,
		URL:            input.URL,
		Method:         input.Method,
		Headers:        input.Headers,
		Body:           input.Body,
		TimeoutSeconds: input.TimeoutSeconds,
		Status:         domain.StatusPending,
		ScheduledAt:    input.ScheduledAt,
		MaxRetries:     *input.MaxRetries,
		Backoff:        input.Backoff,
		WebhookURL:     input.WebhookURL,
		WebhookHeaders: input.WebhookHeaders,
	}

	created, err := u.repo.Create(ctx, job)
	if err != nil {
		return nil, fmt.Errorf("create job: %w", err)
	}

	return created, nil
}

// Replay clones a permanently-failed job into a fresh pending job and returns
// it. The original is left untouched as history; the clone carries a new
// idempotency key and a replay_of pointer back to the source for lineage.
//
// Replay clones rather than resetting the original in place: a job's attempt
// records are keyed UNIQUE(job_id, attempt_num), so re-running the same row
// would collide on attempt_num. A new job gets a clean attempt sequence and
// preserves the failed job's audit trail.
func (u *JobUsecase) Replay(ctx context.Context, jobID, userID string) (*domain.Job, error) {
	job, err := u.repo.GetByID(ctx, jobID, userID)
	if err != nil {
		return nil, fmt.Errorf("get job: %w", err)
	}

	// Only terminal failures are replayable. Pending/running/completed/cancelled
	// jobs are not dead letters.
	if job.Status != domain.StatusFailed {
		return nil, domain.ErrJobNotReplayable
	}

	// A replay enqueues a new execution — gate it on credits like a fresh create.
	ok, err := u.credits.HasCredits(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("check credits: %w", err)
	}
	if !ok {
		return nil, domain.ErrInsufficientCredits
	}

	clone := &domain.Job{
		UserID:         job.UserID,
		IdempotencyKey: uuid.New().String(),
		URL:            job.URL,
		Method:         job.Method,
		Headers:        job.Headers,
		Body:           job.Body,
		TimeoutSeconds: job.TimeoutSeconds,
		Status:         domain.StatusPending,
		ScheduledAt:    time.Now(),
		MaxRetries:     job.MaxRetries,
		Backoff:        job.Backoff,
		WebhookURL:     job.WebhookURL,
		WebhookHeaders: job.WebhookHeaders,
		ReplayOf:       &job.ID,
	}

	created, err := u.repo.Create(ctx, clone)
	if err != nil {
		return nil, fmt.Errorf("create replay job: %w", err)
	}
	return created, nil
}

func (u *JobUsecase) CancelJob(ctx context.Context, jobID, userID string) error {
	if err := u.repo.Cancel(ctx, jobID, userID); err != nil {
		return fmt.Errorf("cancel job: %w", err)
	}
	return nil
}

func (u *JobUsecase) GetByID(ctx context.Context, jobID, userID string) (*domain.Job, error) {
	job, err := u.repo.GetByID(ctx, jobID, userID)
	if err != nil {
		return nil, fmt.Errorf("get job: %w", err)
	}
	return job, nil
}

type ListJobsInput struct {
	UserID string
	Status string
	Cursor string // raw base64url from query param
	Limit  int

	// Optional search/filter predicates. All combine with AND and with Status.
	URLSearch       string     // case-insensitive substring match on the target URL
	Method          string     // exact HTTP method (case-insensitive); empty = all
	ScheduledAfter  *time.Time // scheduled_at >= this; nil = no lower bound
	ScheduledBefore *time.Time // scheduled_at <= this; nil = no upper bound
}

// maxSearchLen caps the URL search term to keep ILIKE scans bounded.
const maxSearchLen = 512

var validMethods = map[string]struct{}{
	"GET": {}, "POST": {}, "PUT": {}, "PATCH": {}, "DELETE": {},
}

type ListJobsResult struct {
	Jobs       []*domain.Job
	NextCursor *string
}

type jobCursor struct {
	ScheduledAt time.Time `json:"s"`
	ID          string    `json:"i"`
}

func decodeCursor(s string) (*time.Time, string, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, "", fmt.Errorf("decode cursor: %w", err)
	}
	var c jobCursor
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, "", fmt.Errorf("unmarshal cursor: %w", err)
	}
	return &c.ScheduledAt, c.ID, nil
}

func encodeCursor(scheduledAt time.Time, id string) string {
	b, _ := json.Marshal(jobCursor{ScheduledAt: scheduledAt, ID: id})
	return base64.RawURLEncoding.EncodeToString(b)
}

var validStatuses = map[domain.Status]struct{}{
	domain.StatusPending:   {},
	domain.StatusRunning:   {},
	domain.StatusCompleted: {},
	domain.StatusFailed:    {},
	domain.StatusCancelled: {},
}

func (u *JobUsecase) ListJobs(ctx context.Context, input ListJobsInput) (ListJobsResult, error) {
	limit := input.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	var status domain.Status
	if input.Status != "" {
		status = domain.Status(input.Status)
		if _, ok := validStatuses[status]; !ok {
			return ListJobsResult{}, domain.ErrInvalidStatus
		}
	}

	var method string
	if m := strings.ToUpper(strings.TrimSpace(input.Method)); m != "" {
		if _, ok := validMethods[m]; !ok {
			return ListJobsResult{}, domain.ErrInvalidMethod
		}
		method = m
	}

	search := strings.TrimSpace(input.URLSearch)
	if len(search) > maxSearchLen {
		return ListJobsResult{}, domain.ErrInvalidSearch
	}

	repoInput := repository.ListJobsInput{
		UserID:          input.UserID,
		Status:          status,
		Method:          method,
		URLSearch:       search,
		ScheduledAfter:  input.ScheduledAfter,
		ScheduledBefore: input.ScheduledBefore,
		Limit:           limit + 1,
	}

	if input.Cursor != "" {
		cursorTime, cursorID, err := decodeCursor(input.Cursor)
		if err != nil {
			return ListJobsResult{}, domain.ErrInvalidStatus
		}
		repoInput.CursorTime = cursorTime
		repoInput.CursorID = cursorID
	}

	jobs, err := u.repo.ListJobs(ctx, repoInput)
	if err != nil {
		return ListJobsResult{}, fmt.Errorf("list jobs: %w", err)
	}

	var nextCursor *string
	if len(jobs) == limit+1 {
		last := jobs[limit]
		s := encodeCursor(last.ScheduledAt, last.ID)
		nextCursor = &s
		jobs = jobs[:limit]
	}

	return ListJobsResult{Jobs: jobs, NextCursor: nextCursor}, nil
}

func (u *JobUsecase) ListAttempts(ctx context.Context, jobID, userID string) ([]*domain.JobAttempt, error) {
	// Verify the job exists and belongs to this user before returning its attempts.
	if _, err := u.repo.GetByID(ctx, jobID, userID); err != nil {
		return nil, fmt.Errorf("get job: %w", err)
	}
	attempts, err := u.attempts.ListByJobID(ctx, jobID)
	if err != nil {
		return nil, fmt.Errorf("list attempts: %w", err)
	}
	return attempts, nil
}
