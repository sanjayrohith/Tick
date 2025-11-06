package domain

import (
	"encoding/json"
	"fmt"
	"time"
)

// Schedule is a recurring rule that generates tasks.
//
// A schedule never executes anything itself. A materializer loop reads enabled
// schedules, computes upcoming fire times, and inserts one task per fire time.
// Any number of scheduler nodes may run that loop concurrently: the unique index
// on (queue, idempotency_key) turns a duplicate insert into a no-op.
type Schedule struct {
	// ID is the primary key, assigned by the database.
	ID int64 `json:"id"`

	// Cron is a five-field cron expression, or an @every shorthand.
	Cron string `json:"cron"`

	// Timezone is an IANA location name such as "Asia/Kolkata". Fire times are
	// computed in this zone's wall-clock terms and stored as UTC instants.
	//
	// A zone that observes DST makes this genuinely hard: twice a year a daily
	// wall-clock time either does not exist or exists twice. See the package
	// comment on internal/cron for the policy.
	Timezone string `json:"timezone"`

	// Queue is the queue materialized tasks are inserted into.
	Queue string `json:"queue"`

	// Handler names the registered function each generated task invokes.
	Handler string `json:"handler"`

	// Payload is copied verbatim onto every generated task.
	Payload json.RawMessage `json:"payload"`

	// Priority is copied onto every generated task.
	Priority int `json:"priority"`

	// MaxAttempts is copied onto every generated task.
	MaxAttempts int `json:"max_attempts"`

	// Enabled gates materialization. Disabling stops future tasks from being
	// generated; it does not touch tasks already materialized.
	Enabled bool `json:"enabled"`

	// LastMaterializedAt is the fire time of the most recent task generated from
	// this schedule. The materializer resumes from here, so a scheduler node
	// that was down for an hour catches up rather than skipping that hour.
	LastMaterializedAt *time.Time `json:"last_materialized_at,omitempty"`

	// CreatedAt is set by the database on insert.
	CreatedAt time.Time `json:"created_at"`
}

// DefaultTimezone is used when a schedule is created without one. UTC is the
// only defensible default: it is the one zone with no DST discontinuities.
const DefaultTimezone = "UTC"

// IdempotencyKeyFor builds the key that makes materialization idempotent across
// any number of scheduler nodes.
//
// The key is "<schedule_id>:<run_at in RFC3339 UTC>". Two nodes computing the
// same fire time for the same schedule produce byte-identical keys, so the
// second insert collides with the unique index and does nothing. Normalising to
// UTC matters: the same instant formatted in two offsets would produce two
// different keys and therefore two tasks.
func IdempotencyKeyFor(scheduleID int64, runAt time.Time) string {
	return fmt.Sprintf("%d:%s", scheduleID, runAt.UTC().Format(time.RFC3339))
}

// Location resolves the schedule's timezone, defaulting to UTC when unset.
func (s *Schedule) Location() (*time.Location, error) {
	name := s.Timezone
	if name == "" {
		name = DefaultTimezone
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("%w: %q: %w", ErrInvalidTimezone, name, err)
	}
	return loc, nil
}

// TaskFor builds the task this schedule generates for a given fire time.
func (s *Schedule) TaskFor(runAt time.Time) *Task {
	key := IdempotencyKeyFor(s.ID, runAt)
	id := s.ID
	return &Task{
		Queue:          s.Queue,
		Handler:        s.Handler,
		Payload:        s.Payload,
		Priority:       s.Priority,
		Status:         StatusPending,
		RunAt:          runAt.UTC(),
		MaxAttempts:    s.MaxAttempts,
		IdempotencyKey: &key,
		ScheduleID:     &id,
	}
}
