package postgres

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sanjayrohith/tick/internal/domain"
)

func TestRecoverOrphansReclaimsExpiredHeartbeatWithBackoff(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	seeded, err := s.Enqueue(ctx, &domain.Task{
		Queue: "default", Handler: "send_email", Payload: json.RawMessage(`{}`), MaxAttempts: 5,
	})
	if err != nil {
		t.Fatalf("Enqueue() = %v, want success", err)
	}
	if _, claimErr := s.Claim(ctx, "default", "worker-1", 1); claimErr != nil {
		t.Fatalf("Claim() = %v, want success", claimErr)
	}

	// Simulate a worker that died 120 seconds ago: well past the 90 second TTL.
	if _, backdateErr := testPool.Exec(ctx,
		"UPDATE tasks SET heartbeat_at = now() - interval '120 seconds' WHERE id = $1", seeded.ID); backdateErr != nil {
		t.Fatalf("backdating heartbeat_at: %v", backdateErr)
	}

	before := time.Now()
	recovered, err := s.RecoverOrphans(ctx)
	if err != nil {
		t.Fatalf("RecoverOrphans() = %v, want success", err)
	}
	if recovered != 1 {
		t.Fatalf("recovered = %d, want 1", recovered)
	}

	got, err := scanTask(testPool.QueryRow(ctx, "SELECT "+taskColumns+" FROM tasks WHERE id = $1", seeded.ID))
	if err != nil {
		t.Fatalf("reading back the recovered task: %v", err)
	}
	if got.Status != domain.StatusPending {
		t.Errorf("Status = %q, want %q", got.Status, domain.StatusPending)
	}
	if !got.RunAt.After(before) {
		t.Errorf("RunAt = %v, want it pushed into the future by backoff (after %v)", got.RunAt, before)
	}
	if got.ClaimedBy != nil {
		t.Errorf("ClaimedBy = %v, want nil", got.ClaimedBy)
	}
	if got.LastError == nil || *got.LastError != "orphaned: heartbeat expired" {
		t.Errorf("LastError = %v, want %q", got.LastError, "orphaned: heartbeat expired")
	}
}

func TestRecoverOrphansLeavesHealthyHeartbeatsUntouched(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	seeded, err := s.Enqueue(ctx, &domain.Task{
		Queue: "default", Handler: "send_email", Payload: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("Enqueue() = %v, want success", err)
	}
	if _, claimErr := s.Claim(ctx, "default", "worker-1", 1); claimErr != nil {
		t.Fatalf("Claim() = %v, want success", claimErr)
	}

	// A heartbeat only 5 seconds old is well within the 90 second TTL.
	if _, backdateErr := testPool.Exec(ctx,
		"UPDATE tasks SET heartbeat_at = now() - interval '5 seconds' WHERE id = $1", seeded.ID); backdateErr != nil {
		t.Fatalf("backdating heartbeat_at: %v", backdateErr)
	}

	recovered, err := s.RecoverOrphans(ctx)
	if err != nil {
		t.Fatalf("RecoverOrphans() = %v, want success", err)
	}
	if recovered != 0 {
		t.Errorf("recovered = %d, want 0: a healthy heartbeat must never be reclaimed", recovered)
	}

	got, err := scanTask(testPool.QueryRow(ctx, "SELECT "+taskColumns+" FROM tasks WHERE id = $1", seeded.ID))
	if err != nil {
		t.Fatalf("reading back the task: %v", err)
	}
	if got.Status != domain.StatusRunning {
		t.Errorf("Status = %q, want %q", got.Status, domain.StatusRunning)
	}
	if got.ClaimedBy == nil || *got.ClaimedBy != "worker-1" {
		t.Errorf("ClaimedBy = %v, want worker-1", got.ClaimedBy)
	}
}
