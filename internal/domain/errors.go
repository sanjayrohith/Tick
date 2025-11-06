package domain

import "errors"

// Sentinel errors crossing the store boundary.
//
// Every backend returns these rather than driver-specific errors, so callers can
// branch on outcome without importing pgx or go-redis. The API layer maps them
// to HTTP status codes in exactly one place.
var (
	// ErrNotFound means no row matched the identifier.
	ErrNotFound = errors.New("not found")

	// ErrDuplicateIdempotencyKey means a task with this (queue, idempotency_key)
	// already exists. Submission treats this as success and returns the original
	// task, because a client retry must not create a second execution.
	ErrDuplicateIdempotencyKey = errors.New("duplicate idempotency key")

	// ErrNotCancellable means the task is no longer pending. A running task
	// cannot be cancelled: its side effects are already in flight.
	ErrNotCancellable = errors.New("task is not cancellable")

	// ErrClaimLost means the worker no longer owns the task it tried to update.
	// The usual cause is a heartbeat that expired and a sweeper that reclaimed
	// the row, meaning the worker was slower than the heartbeat TTL.
	ErrClaimLost = errors.New("claim lost")

	// ErrUnknownHandler means the task names a handler this worker has not
	// registered. Almost always a deploy-ordering mistake.
	ErrUnknownHandler = errors.New("unknown handler")

	// ErrInvalidTimezone means the schedule names a zone the runtime cannot
	// load. Checked at write time so a bad zone is rejected on creation rather
	// than discovered by a materializer loop at three in the morning.
	ErrInvalidTimezone = errors.New("invalid timezone")

	// ErrInvalidCron means the schedule's cron expression does not parse.
	ErrInvalidCron = errors.New("invalid cron expression")

	// ErrQueuePaused means the queue is administratively paused and is not
	// handing out claims.
	ErrQueuePaused = errors.New("queue is paused")
)
