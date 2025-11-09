package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/sanjayrohith/tick/internal/domain"
)

func TestFailRecordsErrorAndReleasesClaim(t *testing.T) {
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

	if failErr := s.Fail(ctx, seeded.ID, "worker-1", errors.New("smtp timeout")); failErr != nil {
		t.Fatalf("Fail() = %v, want success", failErr)
	}

	got, err := scanTask(testPool.QueryRow(ctx, "SELECT "+taskColumns+" FROM tasks WHERE id = $1", seeded.ID))
	if err != nil {
		t.Fatalf("reading back the failed task: %v", err)
	}
	if got.Status != domain.StatusFailed {
		t.Errorf("Status = %q, want %q", got.Status, domain.StatusFailed)
	}
	if got.ClaimedBy != nil {
		t.Errorf("ClaimedBy = %v, want nil", got.ClaimedBy)
	}
	if got.LastError == nil || *got.LastError != "smtp timeout" {
		t.Errorf("LastError = %v, want %q", got.LastError, "smtp timeout")
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
