package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/sanjayrohith/tick/internal/domain"
)

func TestEnqueueDuplicateIdempotencyKeyReturnsOriginal(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	key := "job-42"

	first, err := s.Enqueue(ctx, &domain.Task{
		Queue: "default", Handler: "send_email", Payload: json.RawMessage(`{}`),
		IdempotencyKey: &key,
	})
	if err != nil {
		t.Fatalf("first Enqueue() = %v, want success", err)
	}

	second, err := s.Enqueue(ctx, &domain.Task{
		Queue: "default", Handler: "send_email", Payload: json.RawMessage(`{"different":true}`),
		IdempotencyKey: &key,
	})
	if !errors.Is(err, domain.ErrDuplicateIdempotencyKey) {
		t.Fatalf("second Enqueue() error = %v, want ErrDuplicateIdempotencyKey", err)
	}
	if second == nil || second.ID != first.ID {
		t.Fatalf("second Enqueue() returned task %+v, want the original task %+v", second, first)
	}
}

func TestEnqueueBulkResolvesDuplicatesWithoutFailingTheBatch(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	key := "job-99"

	first, err := s.Enqueue(ctx, &domain.Task{
		Queue: "default", Handler: "send_email", Payload: json.RawMessage(`{}`),
		IdempotencyKey: &key,
	})
	if err != nil {
		t.Fatalf("seeding Enqueue() = %v, want success", err)
	}

	fresh := "job-100"
	batch, err := s.EnqueueBulk(ctx, []*domain.Task{
		{Queue: "default", Handler: "send_email", Payload: json.RawMessage(`{}`), IdempotencyKey: &key},
		{Queue: "default", Handler: "send_email", Payload: json.RawMessage(`{}`), IdempotencyKey: &fresh},
	})
	if err != nil {
		t.Fatalf("EnqueueBulk() = %v, want success even with a duplicate", err)
	}
	if len(batch) != 2 {
		t.Fatalf("len(batch) = %d, want 2", len(batch))
	}
	if batch[0].ID != first.ID {
		t.Errorf("batch[0].ID = %d, want the original task's ID %d", batch[0].ID, first.ID)
	}
	if batch[1].ID == first.ID {
		t.Errorf("batch[1] should be a newly inserted task, got the original's ID")
	}
}
