package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/sanjayrohith/tick/internal/domain"
)

// enqueueOnQueue inserts one task on queue with max_attempts 1, so a single
// failure is enough to exhaust it and move straight to dead.
func enqueueOnQueue(t *testing.T, s *Store, queue string) *domain.Task {
	t.Helper()
	task, err := s.Enqueue(context.Background(), &domain.Task{
		Queue: queue, Handler: "h", Payload: json.RawMessage(`{}`), MaxAttempts: 1,
	})
	if err != nil {
		t.Fatalf("Enqueue() = %v", err)
	}
	return task
}

func TestQueueStatsCountsPending(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	enqueueOnQueue(t, s, "stats-pending")

	stats, err := s.QueueStats(ctx, "stats-pending")
	if err != nil {
		t.Fatalf("QueueStats() = %v", err)
	}
	if stats.Depth[domain.StatusPending] != 1 {
		t.Errorf("Depth[pending] = %d, want 1", stats.Depth[domain.StatusPending])
	}
	if stats.OldestPendingAgeSeconds < 0 {
		t.Errorf("OldestPendingAgeSeconds = %f, want >= 0", stats.OldestPendingAgeSeconds)
	}
}

func TestQueueStatsCountsRunning(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	enqueueOnQueue(t, s, "stats-running")
	if _, err := s.Claim(ctx, "stats-running", "worker-1", 10); err != nil {
		t.Fatalf("Claim() = %v", err)
	}

	stats, err := s.QueueStats(ctx, "stats-running")
	if err != nil {
		t.Fatalf("QueueStats() = %v", err)
	}
	if stats.Depth[domain.StatusRunning] != 1 {
		t.Errorf("Depth[running] = %d, want 1", stats.Depth[domain.StatusRunning])
	}
}

func TestQueueStatsCountsSucceededAndThroughput(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	task := enqueueOnQueue(t, s, "stats-succeeded")
	if _, err := s.Claim(ctx, "stats-succeeded", "worker-1", 10); err != nil {
		t.Fatalf("Claim() = %v", err)
	}
	if err := s.Complete(ctx, task.ID, "worker-1"); err != nil {
		t.Fatalf("Complete() = %v", err)
	}

	stats, err := s.QueueStats(ctx, "stats-succeeded")
	if err != nil {
		t.Fatalf("QueueStats() = %v", err)
	}
	if stats.Depth[domain.StatusSucceeded] != 1 {
		t.Errorf("Depth[succeeded] = %d, want 1", stats.Depth[domain.StatusSucceeded])
	}
	if stats.CompletedLastMinute != 1 {
		t.Errorf("CompletedLastMinute = %d, want 1", stats.CompletedLastMinute)
	}
	if stats.CompletedLastHour != 1 {
		t.Errorf("CompletedLastHour = %d, want 1", stats.CompletedLastHour)
	}
	if stats.CompletedLastDay != 1 {
		t.Errorf("CompletedLastDay = %d, want 1", stats.CompletedLastDay)
	}
}

func TestQueueStatsCountsDead(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	task := enqueueOnQueue(t, s, "stats-dead")
	if _, err := s.Claim(ctx, "stats-dead", "worker-1", 10); err != nil {
		t.Fatalf("Claim() = %v", err)
	}
	if err := s.Fail(ctx, task.ID, "worker-1", errors.New("boom")); err != nil {
		t.Fatalf("Fail() = %v", err)
	}

	stats, err := s.QueueStats(ctx, "stats-dead")
	if err != nil {
		t.Fatalf("QueueStats() = %v", err)
	}
	if stats.Depth[domain.StatusDead] != 1 {
		t.Errorf("Depth[dead] = %d, want 1", stats.Depth[domain.StatusDead])
	}
}

func TestQueueStatsCountsCancelled(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	task := enqueueOnQueue(t, s, "stats-cancelled")
	if err := s.Cancel(ctx, task.ID); err != nil {
		t.Fatalf("Cancel() = %v", err)
	}

	stats, err := s.QueueStats(ctx, "stats-cancelled")
	if err != nil {
		t.Fatalf("QueueStats() = %v", err)
	}
	if stats.Depth[domain.StatusCancelled] != 1 {
		t.Errorf("Depth[cancelled] = %d, want 1", stats.Depth[domain.StatusCancelled])
	}
}

func TestQueueStatsEmptyQueue(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	stats, err := s.QueueStats(ctx, "never-used")
	if err != nil {
		t.Fatalf("QueueStats() = %v", err)
	}
	for _, status := range domain.AllStatuses() {
		if got := stats.Depth[status]; got != 0 {
			t.Errorf("Depth[%s] = %d, want 0", status, got)
		}
	}
	if stats.OldestPendingAgeSeconds != 0 {
		t.Errorf("OldestPendingAgeSeconds = %f, want 0", stats.OldestPendingAgeSeconds)
	}
}
