package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/sanjayrohith/tick/internal/domain"
)

func TestCompleteMarksTaskSucceededAndClearsClaim(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	seeded, err := s.Enqueue(ctx, &domain.Task{
		Queue: "default", Handler: "send_email", Payload: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("Enqueue() = %v, want success", err)
	}
	claimed, err := s.Claim(ctx, "default", "worker-1", 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("Claim() = (%v, %v), want one claimed task", claimed, err)
	}

	if completeErr := s.Complete(ctx, seeded.ID, "worker-1"); completeErr != nil {
		t.Fatalf("Complete() = %v, want success", completeErr)
	}

	got, err := scanTask(testPool.QueryRow(ctx, "SELECT "+taskColumns+" FROM tasks WHERE id = $1", seeded.ID))
	if err != nil {
		t.Fatalf("reading back the completed task: %v", err)
	}
	if got.Status != domain.StatusSucceeded {
		t.Errorf("Status = %q, want %q", got.Status, domain.StatusSucceeded)
	}
	if got.ClaimedBy != nil {
		t.Errorf("ClaimedBy = %v, want nil", got.ClaimedBy)
	}
	if got.HeartbeatAt != nil {
		t.Errorf("HeartbeatAt = %v, want nil", got.HeartbeatAt)
	}
}

func TestCompleteByWrongWorkerReturnsClaimLost(t *testing.T) {
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

	err = s.Complete(ctx, seeded.ID, "worker-2")
	if !errors.Is(err, domain.ErrClaimLost) {
		t.Fatalf("Complete() by the wrong worker = %v, want ErrClaimLost", err)
	}
}
