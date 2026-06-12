package domain

// JobStats aggregates job execution outcomes for a user over a time window.
// Derived from job_attempts (one row per execution attempt).
type JobStats struct {
	// TotalExecutions is the number of completed attempts in the window.
	TotalExecutions int64
	Succeeded       int64
	Failed          int64
	// SuccessRate is Succeeded/TotalExecutions in [0,1], 0 when there are none.
	SuccessRate   float64
	AvgDurationMS float64
	P95DurationMS float64
	SinceDays     int
}

// BufferStats summarizes the current item backlog and outcomes for one buffer.
type BufferStats struct {
	Pending   int64
	Running   int64
	Completed int64
	Failed    int64
	Total     int64
	// SuccessRate is Completed/(Completed+Failed) in [0,1], 0 when there are none.
	SuccessRate float64
}

// UsageBucket is one UTC-day of execution counts, split by execution type.
type UsageBucket struct {
	Date             string `json:"date"`
	JobExecutions    int64  `json:"job_executions"`
	BufferExecutions int64  `json:"buffer_executions"`
}

// UsageSummary is the account-level usage rollup: a daily time series plus
// window totals and the current credit position.
type UsageSummary struct {
	Buckets               []UsageBucket
	TotalJobExecutions    int64
	TotalBufferExecutions int64
	Balance               int64
	Plan                  Plan
	SinceDays             int
}
