package handler

const (
	errInternalServer    = "Internal server error"
	errJobNotFound       = "Job not found"
	errDuplicateJob      = "Job with this idempotency key already exists"
	errTokenInvalid      = "Token is invalid or expired"
	errInvalidStatus     = "Invalid status value"
	errJobNotCancellable = "Job cannot be cancelled in its current state"
	errJobNotReplayable  = "Only failed jobs can be replayed"

	errScheduleNotFound      = "Schedule not found"
	errInvalidCronExpr       = "Invalid cron expression"
	errScheduleNameConflict  = "Schedule with this name already exists"
	errScheduleAlreadyPaused = "Schedule is already paused"
	errScheduleNotPaused     = "Schedule is not paused"

	errTokenNotFound = "Token not found"

	errInsufficientCredits = "Insufficient credits"

	errSigningSecretNotFound = "No signing secret found to rotate"

	errBufferNotFound          = "Buffer not found"
	errBufferNameConflict      = "Buffer with this name already exists"
	errBufferAlreadyPaused     = "Buffer is already paused"
	errBufferNotPaused         = "Buffer is not paused"
	errBufferItemNotFound      = "Buffer item not found"
	errBufferItemNotReplayable = "Only failed buffer items can be replayed"

	errInvalidLimit = "limit must be between 1 and 100"

	errAlertChannelNotFound    = "Alert channel not found"
	errInvalidAlertChannelType = "Invalid alert channel type"
	errAlertChannelNotVerified = "Email alert channels must be verified before they can be enabled"
	errInvalidAlertTarget      = "target must be a URL for webhook/slack channels or an email address for email channels"
	errInvalidVerifyToken      = "Verification link is invalid or expired"

	errInvalidDays = "days must be a non-negative integer"
)
