package domain

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestIdempotencyKeyForIsStableAcrossOffsets(t *testing.T) {
	instant := time.Date(2025, 11, 6, 21, 30, 0, 0, time.UTC)

	kolkata, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}

	// The same instant expressed in two zones must produce one key, or two
	// scheduler nodes in different regions would each materialize a task.
	utcKey := IdempotencyKeyFor(7, instant)
	istKey := IdempotencyKeyFor(7, instant.In(kolkata))

	if utcKey != istKey {
		t.Errorf("same instant produced two keys:\n  UTC: %s\n  IST: %s", utcKey, istKey)
	}
	if want := "7:2025-11-06T21:30:00Z"; utcKey != want {
		t.Errorf("key = %q, want %q", utcKey, want)
	}
}

func TestIdempotencyKeyForDistinguishesScheduleAndTime(t *testing.T) {
	base := time.Date(2025, 11, 6, 12, 0, 0, 0, time.UTC)

	if IdempotencyKeyFor(1, base) == IdempotencyKeyFor(2, base) {
		t.Error("different schedules at the same instant must not share a key")
	}
	if IdempotencyKeyFor(1, base) == IdempotencyKeyFor(1, base.Add(time.Minute)) {
		t.Error("the same schedule at different instants must not share a key")
	}
}

func TestScheduleLocation(t *testing.T) {
	// An unset timezone falls back to UTC rather than the host's local zone,
	// which would make behaviour depend on where the process happens to run.
	loc, err := (&Schedule{}).Location()
	if err != nil {
		t.Fatalf("empty timezone should default to UTC, got: %v", err)
	}
	if loc != time.UTC {
		t.Errorf("default location = %v, want UTC", loc)
	}

	if _, err := (&Schedule{Timezone: "Mars/Olympus_Mons"}).Location(); err == nil {
		t.Error("an unloadable zone must be rejected")
	} else if !errors.Is(err, ErrInvalidTimezone) {
		t.Errorf("error = %v, want it to wrap ErrInvalidTimezone", err)
	}
}

func TestTaskForCopiesScheduleFields(t *testing.T) {
	payload := json.RawMessage(`{"to":"ops@example.com"}`)
	s := &Schedule{
		ID:          42,
		Queue:       "emails",
		Handler:     "send_digest",
		Payload:     payload,
		Priority:    5,
		MaxAttempts: 3,
	}
	runAt := time.Date(2025, 11, 6, 2, 30, 0, 0, time.UTC)

	task := s.TaskFor(runAt)

	if task.Queue != "emails" || task.Handler != "send_digest" {
		t.Errorf("queue/handler not copied: %+v", task)
	}
	if task.Priority != 5 || task.MaxAttempts != 3 {
		t.Errorf("priority/max_attempts not copied: %+v", task)
	}
	if string(task.Payload) != string(payload) {
		t.Errorf("payload = %s, want %s", task.Payload, payload)
	}
	if task.Status != StatusPending {
		t.Errorf("status = %q, want pending", task.Status)
	}
	if !task.RunAt.Equal(runAt) {
		t.Errorf("run_at = %v, want %v", task.RunAt, runAt)
	}
	if task.ScheduleID == nil || *task.ScheduleID != 42 {
		t.Errorf("schedule_id not linked back: %+v", task.ScheduleID)
	}
	if task.IdempotencyKey == nil {
		t.Fatal("a materialized task must carry an idempotency key")
	}
	if want := IdempotencyKeyFor(42, runAt); *task.IdempotencyKey != want {
		t.Errorf("idempotency key = %q, want %q", *task.IdempotencyKey, want)
	}
	if !task.Recurring() {
		t.Error("a materialized task must report as recurring")
	}
}

// Two nodes computing the same fire time must produce identical tasks, since
// that identity is the only thing standing between one task and two.
func TestTaskForIsDeterministic(t *testing.T) {
	s := &Schedule{ID: 1, Queue: "default", Handler: "noop", MaxAttempts: 5}
	runAt := time.Date(2026, 3, 8, 7, 0, 0, 0, time.UTC)

	a, b := s.TaskFor(runAt), s.TaskFor(runAt)
	if *a.IdempotencyKey != *b.IdempotencyKey {
		t.Errorf("keys differ across calls: %q vs %q", *a.IdempotencyKey, *b.IdempotencyKey)
	}
}

// The API maps these to status codes, so each must stay distinguishable.
func TestSentinelErrorsAreDistinct(t *testing.T) {
	all := []error{
		ErrNotFound, ErrDuplicateIdempotencyKey, ErrNotCancellable,
		ErrClaimLost, ErrUnknownHandler, ErrInvalidTimezone,
		ErrInvalidCron, ErrQueuePaused,
	}
	for i, a := range all {
		for j, b := range all {
			if i != j && errors.Is(a, b) {
				t.Errorf("sentinel %d and %d are not distinguishable: %v / %v", i, j, a, b)
			}
		}
	}
}
