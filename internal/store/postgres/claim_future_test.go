package postgres

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sanjayrohith/tick/internal/domain"
)

// TestClaimExcludesTasksNotYetDue seeds a task an hour in the future and
// asserts claim leaves it alone: run_at is compared against the database
// clock, and a task that is not due yet must not be handed to a worker.
func TestClaimExcludesTasksNotYetDue(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	if _, err := s.Enqueue(ctx, &domain.Task{
		Queue: "default", Handler: "send_email", Payload: json.RawMessage(`{}`),
		RunAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("Enqueue() = %v, want success", err)
	}

	claimed, err := s.Claim(ctx, "default", "worker-1", 10)
	if err != nil {
		t.Fatalf("Claim() = %v, want success", err)
	}
	if len(claimed) != 0 {
		t.Errorf("len(claimed) = %d, want 0: a future task must not be claimable", len(claimed))
	}
}
