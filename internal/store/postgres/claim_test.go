package postgres

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sanjayrohith/tick/internal/domain"
)

func TestClaimMarksTaskRunningAndStampsWorker(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	seeded, err := s.Enqueue(ctx, &domain.Task{
		Queue: "default", Handler: "send_email", Payload: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("Enqueue() = %v, want success", err)
	}

	claimed, err := s.Claim(ctx, "default", "worker-1", 10)
	if err != nil {
		t.Fatalf("Claim() = %v, want success", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("len(claimed) = %d, want 1", len(claimed))
	}

	got := claimed[0]
	if got.ID != seeded.ID {
		t.Errorf("ID = %d, want %d", got.ID, seeded.ID)
	}
	if got.Status != domain.StatusRunning {
		t.Errorf("Status = %q, want %q", got.Status, domain.StatusRunning)
	}
	if got.ClaimedBy == nil || *got.ClaimedBy != "worker-1" {
		t.Errorf("ClaimedBy = %v, want worker-1", got.ClaimedBy)
	}
	if got.HeartbeatAt == nil {
		t.Error("HeartbeatAt is nil, want it stamped on claim")
	}
	if got.Attempts != 1 {
		t.Errorf("Attempts = %d, want 1", got.Attempts)
	}
}

func TestClaimReturnsEmptyWhenNothingDue(t *testing.T) {
	s := newStore(t)
	claimed, err := s.Claim(context.Background(), "default", "worker-1", 10)
	if err != nil {
		t.Fatalf("Claim() = %v, want success", err)
	}
	if len(claimed) != 0 {
		t.Errorf("len(claimed) = %d, want 0", len(claimed))
	}
}

func TestClaimRespectsLimit(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	tasks := make([]*domain.Task, 0, 5)
	for i := 0; i < 5; i++ {
		tasks = append(tasks, &domain.Task{
			Queue: "default", Handler: "send_email", Payload: json.RawMessage(`{}`),
		})
	}
	if _, err := s.EnqueueBulk(ctx, tasks); err != nil {
		t.Fatalf("EnqueueBulk() = %v, want success", err)
	}

	claimed, err := s.Claim(ctx, "default", "worker-1", 3)
	if err != nil {
		t.Fatalf("Claim() = %v, want success", err)
	}
	if len(claimed) != 3 {
		t.Errorf("len(claimed) = %d, want 3", len(claimed))
	}
}
