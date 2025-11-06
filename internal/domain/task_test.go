package domain

import (
	"strings"
	"testing"
	"time"
)

func TestStatusValid(t *testing.T) {
	for _, s := range AllStatuses() {
		if !s.Valid() {
			t.Errorf("AllStatuses() contains %q but Valid() rejects it", s)
		}
	}
	for _, s := range []Status{"", "PENDING", "queued", "done", "retrying"} {
		if s.Valid() {
			t.Errorf("Valid() accepted unknown status %q", s)
		}
	}
}

func TestStatusTerminal(t *testing.T) {
	terminal := map[Status]bool{
		StatusSucceeded: true, StatusDead: true, StatusCancelled: true,
		StatusPending: false, StatusRunning: false, StatusFailed: false,
	}
	for s, want := range terminal {
		if got := s.Terminal(); got != want {
			t.Errorf("%q.Terminal() = %v, want %v", s, got, want)
		}
	}
}

// Every status must be covered by the terminal map above, or a status added
// later silently defaults to non-terminal.
func TestAllStatusesCoveredByTerminal(t *testing.T) {
	if got, want := len(AllStatuses()), 6; got != want {
		t.Errorf("AllStatuses() has %d entries, want %d; update the terminal tests", got, want)
	}
}

func TestCancellableOnlyWhenPending(t *testing.T) {
	for _, s := range AllStatuses() {
		task := &Task{Status: s}
		want := s == StatusPending
		if got := task.Cancellable(); got != want {
			t.Errorf("status %q: Cancellable() = %v, want %v", s, got, want)
		}
	}
}

func TestRetriesRemaining(t *testing.T) {
	for _, tc := range []struct {
		attempts, maxAttempts, want int
	}{
		{0, 5, 5},
		{1, 5, 4},
		{4, 5, 1},
		{5, 5, 0},
		{9, 5, 0}, // never negative, even if attempts overshoot
	} {
		task := &Task{Attempts: tc.attempts, MaxAttempts: tc.maxAttempts}
		if got := task.RetriesRemaining(); got != tc.want {
			t.Errorf("attempts=%d max=%d: RetriesRemaining() = %d, want %d",
				tc.attempts, tc.maxAttempts, got, tc.want)
		}
	}
}

func TestExhausted(t *testing.T) {
	for _, tc := range []struct {
		attempts, maxAttempts int
		want                  bool
	}{
		{0, 5, false},
		{4, 5, false},
		{5, 5, true},
		{6, 5, true},
	} {
		task := &Task{Attempts: tc.attempts, MaxAttempts: tc.maxAttempts}
		if got := task.Exhausted(); got != tc.want {
			t.Errorf("attempts=%d max=%d: Exhausted() = %v, want %v",
				tc.attempts, tc.maxAttempts, got, tc.want)
		}
	}
}

func TestDue(t *testing.T) {
	now := time.Date(2025, 11, 6, 12, 0, 0, 0, time.UTC)

	for _, tc := range []struct {
		name   string
		status Status
		runAt  time.Time
		want   bool
	}{
		{"pending and past due", StatusPending, now.Add(-time.Minute), true},
		{"pending exactly now", StatusPending, now, true},
		{"pending in the future", StatusPending, now.Add(time.Minute), false},
		{"running and past due", StatusRunning, now.Add(-time.Minute), false},
		{"dead and past due", StatusDead, now.Add(-time.Minute), false},
		{"cancelled and past due", StatusCancelled, now.Add(-time.Minute), false},
	} {
		task := &Task{Status: tc.status, RunAt: tc.runAt}
		if got := task.Due(now); got != tc.want {
			t.Errorf("%s: Due() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestRecurring(t *testing.T) {
	id := int64(7)
	if (&Task{}).Recurring() {
		t.Error("a task with no schedule_id must not report as recurring")
	}
	if !(&Task{ScheduleID: &id}).Recurring() {
		t.Error("a task with a schedule_id must report as recurring")
	}
}

func TestTruncateError(t *testing.T) {
	short := "connection refused"
	if got := TruncateError(short); got != short {
		t.Errorf("a short message must pass through unchanged, got %q", got)
	}

	long := strings.Repeat("x", MaxLastErrorBytes*2)
	got := TruncateError(long)
	if len(got) > MaxLastErrorBytes {
		t.Errorf("truncated length = %d, want at most %d", len(got), MaxLastErrorBytes)
	}
	if !strings.HasSuffix(got, "[truncated]") {
		t.Errorf("a truncated message must say so, got tail %q", got[len(got)-20:])
	}

	// Exactly at the limit must not be truncated.
	exact := strings.Repeat("y", MaxLastErrorBytes)
	if got := TruncateError(exact); len(got) != MaxLastErrorBytes {
		t.Errorf("a message exactly at the limit was altered: len = %d", len(got))
	}
}
