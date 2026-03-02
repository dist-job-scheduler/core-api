package handler

const (
	errInternalServer = "Internal server error"
	errJobNotFound    = "Job not found"
	errDuplicateJob   = "Job with this idempotency key already exists"
	errTokenInvalid   = "Token is invalid or expired"
	errInvalidStatus     = "Invalid status value"
	errJobNotCancellable = "Job cannot be cancelled in its current state"

	errScheduleNotFound      = "Schedule not found"
	errInvalidCronExpr       = "Invalid cron expression"
	errScheduleNameConflict  = "Schedule with this name already exists"
	errScheduleAlreadyPaused = "Schedule is already paused"
	errScheduleNotPaused     = "Schedule is not paused"

	errTokenNotFound = "Token not found"
)
