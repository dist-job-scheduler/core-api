package usecase

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/repository"
	"github.com/robfig/cron/v3"
)

type ScheduleUsecase struct {
	repo    repository.ScheduleRepository
	jobRepo repository.JobRepository
}

func NewScheduleUsecase(repo repository.ScheduleRepository, jobRepo repository.JobRepository) *ScheduleUsecase {
	return &ScheduleUsecase{repo: repo, jobRepo: jobRepo}
}

type CreateScheduleInput struct {
	UserID         string
	Name           string
	CronExpr       string
	URL            string
	Method         string
	Headers        map[string]string
	Body           *string
	TimeoutSeconds int
	MaxRetries     *int
	Backoff        domain.Backoff
}

func (u *ScheduleUsecase) CreateSchedule(ctx context.Context, input CreateScheduleInput) (*domain.Schedule, error) {
	sched, err := cron.ParseStandard(input.CronExpr)
	if err != nil {
		return nil, domain.ErrInvalidCronExpr
	}

	if input.Headers == nil {
		input.Headers = make(map[string]string)
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

	nextRunAt := sched.Next(time.Now())

	s := &domain.Schedule{
		UserID:         input.UserID,
		Name:           input.Name,
		CronExpr:       input.CronExpr,
		URL:            input.URL,
		Method:         input.Method,
		Headers:        input.Headers,
		Body:           input.Body,
		TimeoutSeconds: input.TimeoutSeconds,
		MaxRetries:     *input.MaxRetries,
		Backoff:        input.Backoff,
		Paused:         false,
		NextRunAt:      nextRunAt,
	}

	created, err := u.repo.Create(ctx, s)
	if err != nil {
		return nil, fmt.Errorf("create schedule: %w", err)
	}
	return created, nil
}

func (u *ScheduleUsecase) GetSchedule(ctx context.Context, id, userID string) (*domain.Schedule, error) {
	s, err := u.repo.GetByID(ctx, id, userID)
	if err != nil {
		return nil, fmt.Errorf("get schedule: %w", err)
	}
	return s, nil
}

type ListSchedulesInput struct {
	UserID string
	Cursor string
	Limit  int
}

type ListSchedulesResult struct {
	Schedules  []*domain.Schedule
	NextCursor *string
}

type scheduleCursor struct {
	CreatedAt time.Time `json:"c"`
	ID        string    `json:"i"`
}

func decodeScheduleCursor(s string) (*time.Time, string, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, "", fmt.Errorf("decode cursor: %w", err)
	}
	var c scheduleCursor
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, "", fmt.Errorf("unmarshal cursor: %w", err)
	}
	return &c.CreatedAt, c.ID, nil
}

func encodeScheduleCursor(createdAt time.Time, id string) string {
	b, _ := json.Marshal(scheduleCursor{CreatedAt: createdAt, ID: id})
	return base64.RawURLEncoding.EncodeToString(b)
}

func (u *ScheduleUsecase) ListSchedules(ctx context.Context, input ListSchedulesInput) (ListSchedulesResult, error) {
	limit := input.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	repoInput := repository.ListSchedulesInput{
		UserID: input.UserID,
		Limit:  limit + 1,
	}

	if input.Cursor != "" {
		cursorTime, cursorID, err := decodeScheduleCursor(input.Cursor)
		if err != nil {
			return ListSchedulesResult{}, domain.ErrInvalidCronExpr // reuse as generic bad cursor
		}
		repoInput.CursorTime = cursorTime
		repoInput.CursorID = cursorID
	}

	schedules, err := u.repo.List(ctx, repoInput)
	if err != nil {
		return ListSchedulesResult{}, fmt.Errorf("list schedules: %w", err)
	}

	var nextCursor *string
	if len(schedules) == limit+1 {
		last := schedules[limit]
		s := encodeScheduleCursor(last.CreatedAt, last.ID)
		nextCursor = &s
		schedules = schedules[:limit]
	}

	return ListSchedulesResult{Schedules: schedules, NextCursor: nextCursor}, nil
}

func (u *ScheduleUsecase) PauseSchedule(ctx context.Context, id, userID string) error {
	if err := u.repo.SetPaused(ctx, id, userID, true); err != nil {
		return fmt.Errorf("pause schedule: %w", err)
	}
	return nil
}

func (u *ScheduleUsecase) ResumeSchedule(ctx context.Context, id, userID string) error {
	if err := u.repo.SetPaused(ctx, id, userID, false); err != nil {
		return fmt.Errorf("resume schedule: %w", err)
	}
	return nil
}

func (u *ScheduleUsecase) DeleteSchedule(ctx context.Context, id, userID string) error {
	if err := u.repo.Delete(ctx, id, userID); err != nil {
		return fmt.Errorf("delete schedule: %w", err)
	}
	return nil
}

type ListScheduleJobsInput struct {
	ScheduleID string
	UserID     string
	Cursor     string
	Limit      int
}

func (u *ScheduleUsecase) ListScheduleJobs(ctx context.Context, input ListScheduleJobsInput) (ListJobsResult, error) {
	// Verify ownership
	if _, err := u.repo.GetByID(ctx, input.ScheduleID, input.UserID); err != nil {
		return ListJobsResult{}, fmt.Errorf("get schedule: %w", err)
	}

	limit := input.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	var cursorTime *time.Time
	var cursorID string

	if input.Cursor != "" {
		ct, cid, err := decodeCursor(input.Cursor)
		if err != nil {
			return ListJobsResult{}, domain.ErrInvalidStatus
		}
		cursorTime = ct
		cursorID = cid
	}

	jobs, err := u.jobRepo.ListByScheduleID(ctx, input.ScheduleID, limit+1, cursorTime, cursorID)
	if err != nil {
		return ListJobsResult{}, fmt.Errorf("list schedule jobs: %w", err)
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
