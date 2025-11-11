package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/sanjayrohith/tick/internal/domain"
)

func TestFailWithRetriesRemainingGoesBackToPendingWithBackoff(t *testing.T) {
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

	before := time.Now()
	if failErr := s.Fail(ctx, seeded.ID, "worker-1", errors.New("smtp timeout")); failErr != nil {
		t.Fatalf("Fail() = %v, want success", failErr)
	}

	got, err := scanTask(testPool.QueryRow(ctx, "SELECT "+taskColumns+" FROM tasks WHERE id = $1", seeded.ID))
	if err != nil {
		t.Fatalf("reading back the failed task: %v", err)
	}
	if got.Status != domain.StatusPending {
		t.Errorf("Status = %q, want %q (a retry, since attempts=1 < max_attempts=5)", got.Status, domain.StatusPending)
	}
	if !got.RunAt.After(before) {
		t.Errorf("RunAt = %v, want it pushed into the future by backoff (after %v)", got.RunAt, before)
	}
	if got.ClaimedBy != nil {
		t.Errorf("ClaimedBy = %v, want nil", got.ClaimedBy)
	}
	if got.LastError == nil || *got.LastError != "smtp timeout" {
		t.Errorf("LastError = %v, want %q", got.LastError, "smtp timeout")
	}
}

func TestFailWithExhaustedAttemptsMovesToDead(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	seeded, err := s.Enqueue(ctx, &domain.Task{
		Queue: "default", Handler: "send_email", Payload: json.RawMessage(`{}`), MaxAttempts: 1,
	})
	if err != nil {
		t.Fatalf("Enqueue() = %v, want success", err)
	}
	if _, claimErr := s.Claim(ctx, "default", "worker-1", 1); claimErr != nil {
		t.Fatalf("Claim() = %v, want success", claimErr)
	}

	if failErr := s.Fail(ctx, seeded.ID, "worker-1", errors.New("permanently broken")); failErr != nil {
		t.Fatalf("Fail() = %v, want success", failErr)
	}

	got, err := scanTask(testPool.QueryRow(ctx, "SELECT "+taskColumns+" FROM tasks WHERE id = $1", seeded.ID))
	if err != nil {
		t.Fatalf("reading back the dead task: %v", err)
	}
	if got.Status != domain.StatusDead {
		t.Errorf("Status = %q, want %q (attempts=1 has reached max_attempts=1)", got.Status, domain.StatusDead)
	}
	if got.Attempts != 1 {
		t.Errorf("Attempts = %d, want 1 (kept intact for inspection)", got.Attempts)
	}
	if got.RunAt != seeded.RunAt {
		t.Errorf("RunAt = %v, want unchanged from %v (a dead task is evidence, not touched further)",
			got.RunAt, seeded.RunAt)
	}
	if got.LastError == nil || *got.LastError != "permanently broken" {
		t.Errorf("LastError = %v, want %q", got.LastError, "permanently broken")
	}
}

func TestFailByWrongWorkerReturnsClaimLost(t *testing.T) {
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

	err = s.Fail(ctx, seeded.ID, "worker-2", errors.New("boom"))
	if !errors.Is(err, domain.ErrClaimLost) {
		t.Fatalf("Fail() by the wrong worker = %v, want ErrClaimLost", err)
	}
}
