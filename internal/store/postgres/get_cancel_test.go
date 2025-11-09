package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/sanjayrohith/tick/internal/domain"
)

func TestGetReturnsTaskByID(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	seeded, err := s.Enqueue(ctx, &domain.Task{
		Queue: "default", Handler: "send_email", Payload: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("Enqueue() = %v, want success", err)
	}

	got, err := s.Get(ctx, seeded.ID)
	if err != nil {
		t.Fatalf("Get() = %v, want success", err)
	}
	if got.ID != seeded.ID {
		t.Errorf("ID = %d, want %d", got.ID, seeded.ID)
	}
}

func TestGetUnknownIDReturnsNotFound(t *testing.T) {
	s := newStore(t)
	_, err := s.Get(context.Background(), 999999)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Get() = %v, want ErrNotFound", err)
	}
}

func TestCancelPendingTaskSucceeds(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	seeded, err := s.Enqueue(ctx, &domain.Task{
		Queue: "default", Handler: "send_email", Payload: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("Enqueue() = %v, want success", err)
	}

	if cancelErr := s.Cancel(ctx, seeded.ID); cancelErr != nil {
		t.Fatalf("Cancel() = %v, want success", cancelErr)
	}

	got, err := s.Get(ctx, seeded.ID)
	if err != nil {
		t.Fatalf("Get() = %v, want success", err)
	}
	if got.Status != domain.StatusCancelled {
		t.Errorf("Status = %q, want %q", got.Status, domain.StatusCancelled)
	}
}

func TestCancelRunningTaskReturnsNotCancellable(t *testing.T) {
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

	err = s.Cancel(ctx, seeded.ID)
	if !errors.Is(err, domain.ErrNotCancellable) {
		t.Fatalf("Cancel() on a running task = %v, want ErrNotCancellable", err)
	}
}

func TestCancelUnknownIDReturnsNotFound(t *testing.T) {
	s := newStore(t)
	err := s.Cancel(context.Background(), 999999)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Cancel() on an unknown id = %v, want ErrNotFound", err)
	}
}
