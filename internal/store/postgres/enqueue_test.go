package postgres

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sanjayrohith/tick/internal/domain"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	truncateAll(t)
	return &Store{pool: testPool}
}

func TestEnqueueDefaultsRunAtToDatabaseNow(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	before := time.Now().Add(-time.Second)
	inserted, err := s.Enqueue(ctx, &domain.Task{
		Queue:   "default",
		Handler: "send_email",
		Payload: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("Enqueue() = %v, want success", err)
	}
	after := time.Now().Add(time.Second)

	if inserted.ID == 0 {
		t.Error("ID = 0, want a database-assigned id")
	}
	if inserted.Status != domain.StatusPending {
		t.Errorf("Status = %q, want %q", inserted.Status, domain.StatusPending)
	}
	if inserted.RunAt.Before(before) || inserted.RunAt.After(after) {
		t.Errorf("RunAt = %v, want between %v and %v (database clock, not client clock)",
			inserted.RunAt, before, after)
	}
	if inserted.MaxAttempts != 5 {
		t.Errorf("MaxAttempts = %d, want the default of 5", inserted.MaxAttempts)
	}
}

func TestEnqueueHonoursExplicitRunAt(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	runAt := time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)
	inserted, err := s.Enqueue(ctx, &domain.Task{
		Queue:   "default",
		Handler: "send_email",
		Payload: json.RawMessage(`{}`),
		RunAt:   runAt,
	})
	if err != nil {
		t.Fatalf("Enqueue() = %v, want success", err)
	}
	if !inserted.RunAt.Equal(runAt) {
		t.Errorf("RunAt = %v, want %v", inserted.RunAt, runAt)
	}
}
